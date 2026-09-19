package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"rpcdiff/internal/compare"
	"rpcdiff/internal/normalize"
	"rpcdiff/internal/report"
	"rpcdiff/internal/rpc"
)

const maxRequestBodyBytes = 8 << 20

// Config controls a shadow proxy. Baseline requests are deliberately made
// without retries because the proxy must never replay a write. Candidate
// requests are made only for methods in ReadOnlyMethods.
type Config struct {
	Baseline            string
	Candidate           string
	Timeout             time.Duration
	Retries             int
	Workers             int
	Queue               int
	IgnoreErrorMessages bool
}

// ReadOnlyMethods is a conservative allowlist. Unknown methods are sent only
// to the baseline until they are explicitly reviewed and added here.
var ReadOnlyMethods = rpc.ReadOnlyMethods

type candidateJob struct {
	req      rpc.Request
	body     []byte
	baseline rpc.CallOutcome
}

// Proxy forwards application traffic to baseline and records asynchronous
// candidate comparisons for safe read methods.
type Proxy struct {
	config          Config
	baselineClient  *rpc.Client
	candidateClient *rpc.Client
	jobs            chan candidateJob
	workersWG       sync.WaitGroup
	store           *resultStore

	waitOnce    sync.Once
	waitResults []compare.Result
	waitErr     error
}

func NewProxy(cfg Config) (*Proxy, error) {
	if cfg.Baseline == "" || cfg.Candidate == "" {
		return nil, fmt.Errorf("baseline and candidate URLs are required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.Retries < 0 {
		cfg.Retries = 0
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.Queue <= 0 {
		cfg.Queue = 256
	}
	store, err := newResultStore()
	if err != nil {
		return nil, err
	}
	p := &Proxy{
		config:          cfg,
		baselineClient:  rpc.NewClientWithRetries(cfg.Timeout, 0),
		candidateClient: rpc.NewClientWithRetries(cfg.Timeout, cfg.Retries),
		jobs:            make(chan candidateJob, cfg.Queue),
		store:           store,
	}
	for i := 0; i < cfg.Workers; i++ {
		p.workersWG.Add(1)
		go p.worker()
	}
	return p, nil
}

func (p *Proxy) Handler() http.Handler {
	return http.HandlerFunc(p.handle)
}

func (p *Proxy) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes+1))
	if err != nil {
		http.Error(w, "unable to read request body", http.StatusBadRequest)
		return
	}
	if len(body) > maxRequestBodyBytes {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	req, parseErr := parseRequest(body)
	baseline := p.baselineClient.CallRaw(r.Context(), p.config.Baseline, body)
	writeOutcome(w, baseline, req)

	if parseErr != nil {
		p.recordUncompared(req, baseline, "request was not a valid single JSON-RPC read request: "+parseErr.Error())
		return
	}
	if !ReadOnlyMethods[req.Method] {
		p.record(compare.SkippedResult(req.Method, req.Params, baseline, "candidate duplication skipped: method is not on the read-only allowlist"))
		return
	}
	if len(req.ID) == 0 {
		p.record(compare.SkippedResult(req.Method, req.Params, baseline, "candidate comparison skipped: notifications have no response to compare"))
		return
	}

	// Candidate work is bounded and starts after the baseline response has been
	// written. A full queue records a transport failure without delaying the
	// application's response or growing memory without limit.
	job := candidateJob{req: req, body: append([]byte(nil), body...), baseline: baseline}
	select {
	case p.jobs <- job:
	default:
		p.record(compare.UncomparedResult(req.Method, req.Params, compare.TransientFailure, baseline, "candidate duplication skipped: worker queue is full"))
	}
}

func parseRequest(body []byte) (rpc.Request, error) {
	var req rpc.Request
	if err := json.Unmarshal(body, &req); err != nil {
		return req, err
	}
	if err := req.Validate(); err != nil {
		return req, err
	}
	if len(req.JSONRPC) == 0 {
		req.JSONRPC = "2.0"
	}
	if len(req.Params) == 0 {
		req.Params = json.RawMessage("[]")
	}
	return req, nil
}

func writeOutcome(w http.ResponseWriter, outcome rpc.CallOutcome, req rpc.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := outcome.StatusCode
	body := outcome.Body
	if status == 0 {
		status = http.StatusBadGateway
		id := req.ID
		if len(id) == 0 {
			id = json.RawMessage("null")
		}
		body = []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32001,"message":"baseline provider unavailable"}}`, id))
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (p *Proxy) record(result compare.Result) {
	_ = p.store.append(result)
}

func (p *Proxy) recordUncompared(req rpc.Request, baseline rpc.CallOutcome, note string) {
	params := req.Params
	if len(params) == 0 {
		params = json.RawMessage("null")
	}
	p.record(compare.UncomparedResult("<invalid>", params, compare.Inconclusive, baseline, note))
}

func (p *Proxy) worker() {
	defer p.workersWG.Done()
	for job := range p.jobs {
		candidate := p.candidateClient.CallRaw(context.Background(), p.config.Candidate, job.body)
		baseForCompare, baseNormalization := normalize.Outcome(job.req.Method, job.baseline)
		candidateForCompare, candidateNormalization := normalize.Outcome(job.req.Method, candidate)
		p.record(compare.PairWithOptions(
			job.req.Method,
			job.req.Params,
			baseForCompare,
			candidateForCompare,
			baseNormalization.Applied || candidateNormalization.Applied || normalize.Supported[job.req.Method],
			compare.Options{IgnoreErrorMessages: p.config.IgnoreErrorMessages},
		))
	}
}

// Wait blocks until all candidate requests have finished and returns a stable
// copy of the streamed results.
func (p *Proxy) Wait() []compare.Result {
	p.waitOnce.Do(func() {
		close(p.jobs)
		p.workersWG.Wait()
		p.waitResults, p.waitErr = p.store.readAll()
	})
	results := make([]compare.Result, len(p.waitResults))
	copy(results, p.waitResults)
	return results
}

func (p *Proxy) Report(proxyURL string) (report.Run, error) {
	run := report.BuildMode("shadow", p.config.Baseline, p.config.Candidate, proxyURL, p.config.Timeout.String(), p.Wait())
	return run, p.waitErr
}
