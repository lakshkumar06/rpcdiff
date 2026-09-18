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
	IgnoreErrorMessages bool
}

// ReadOnlyMethods is a conservative allowlist. Unknown methods are sent only
// to the baseline until they are explicitly reviewed and added here.
var ReadOnlyMethods = map[string]bool{
	"web3_clientVersion": true,
	"web3_sha3":          true,
	"net_version":        true,
	"net_listening":      true,
	"net_peerCount":      true,

	"eth_protocolVersion":                  true,
	"eth_syncing":                          true,
	"eth_chainId":                          true,
	"eth_blockNumber":                      true,
	"eth_coinbase":                         true,
	"eth_mining":                           true,
	"eth_hashrate":                         true,
	"eth_gasPrice":                         true,
	"eth_blobBaseFee":                      true,
	"eth_maxPriorityFeePerGas":             true,
	"eth_feeHistory":                       true,
	"eth_getBalance":                       true,
	"eth_getStorageAt":                     true,
	"eth_getTransactionCount":              true,
	"eth_getBlockTransactionCountByHash":   true,
	"eth_getBlockTransactionCountByNumber": true,
	"eth_getUncleCountByBlockHash":         true,
	"eth_getUncleCountByBlockNumber":       true,
	"eth_getCode":                          true,
	"eth_call":                             true,
	"eth_estimateGas":                      true,
	"eth_getBlockByHash":                   true,
	"eth_getBlockByNumber":                 true,
	"eth_getTransactionByHash":             true,
	"eth_getTransactionReceipt":            true,
	"eth_getUncleByBlockHashAndIndex":      true,
	"eth_getUncleByBlockNumberAndIndex":    true,
	"eth_getLogs":                          true,
	"eth_getProof":                         true,
	"eth_getBlockReceipts":                 true,
	"eth_getRawTransactionByHash":          true,
	"eth_getRawTransactionFromBlock":       true,
}

// Proxy forwards application traffic to baseline and records asynchronous
// candidate comparisons for safe read methods.
type Proxy struct {
	config          Config
	baselineClient  *rpc.Client
	candidateClient *rpc.Client

	mu      sync.Mutex
	results []compare.Result
	wg      sync.WaitGroup
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
	return &Proxy{
		config:          cfg,
		baselineClient:  rpc.NewClientWithRetries(cfg.Timeout, 0),
		candidateClient: rpc.NewClientWithRetries(cfg.Timeout, cfg.Retries),
	}, nil
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

	// Candidate work starts after the baseline response has been written. Its
	// latency and failures therefore cannot delay the application's response.
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		candidate := p.candidateClient.CallRaw(context.Background(), p.config.Candidate, body)
		baseForCompare, baseNormalization := normalize.Outcome(req.Method, baseline)
		candidateForCompare, candidateNormalization := normalize.Outcome(req.Method, candidate)
		p.record(compare.PairWithOptions(
			req.Method,
			req.Params,
			baseForCompare,
			candidateForCompare,
			baseNormalization.Applied || candidateNormalization.Applied || normalize.Supported[req.Method],
			compare.Options{IgnoreErrorMessages: p.config.IgnoreErrorMessages},
		))
	}()
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
	if status == 0 || len(body) == 0 {
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
	p.mu.Lock()
	p.results = append(p.results, result)
	p.mu.Unlock()
}

func (p *Proxy) recordUncompared(req rpc.Request, baseline rpc.CallOutcome, note string) {
	params := req.Params
	if len(params) == 0 {
		params = json.RawMessage("null")
	}
	p.record(compare.Result{
		Method:         "<invalid>",
		Params:         params,
		Classification: compare.Inconclusive,
		Baseline:       sideFromOutcome(baseline),
		Notes:          []string{note},
	})
}

// Wait blocks until all candidate requests have finished and returns a stable
// copy of the recorded results.
func (p *Proxy) Wait() []compare.Result {
	p.wg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	results := make([]compare.Result, len(p.results))
	copy(results, p.results)
	return results
}

func (p *Proxy) Report(proxyURL string) report.Run {
	return report.BuildMode("shadow", p.config.Baseline, p.config.Candidate, proxyURL, p.config.Timeout.String(), p.Wait())
}

// sideFromOutcome keeps invalid incoming requests visible in a shadow report
// without making the compare package's internal side conversion public.
func sideFromOutcome(outcome rpc.CallOutcome) compare.Side {
	var response json.RawMessage
	if len(outcome.Body) > 0 {
		if json.Valid(outcome.Body) {
			response = append(json.RawMessage(nil), outcome.Body...)
		}
	}
	return compare.Side{
		LatencyMS:      float64(outcome.Latency) / float64(time.Millisecond),
		StatusCode:     outcome.StatusCode,
		HTTPError:      outcome.HTTPError,
		TimedOut:       outcome.TimedOut,
		Transient:      outcome.Transient,
		Attempts:       outcome.Attempts,
		JSONRPCError:   outcome.JSONRPCError,
		ParseError:     outcome.ParseError,
		InvalidRequest: outcome.InvalidRequest,
		Response:       response,
	}
}
