package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"rpcdiff/internal/compare"
	"rpcdiff/internal/rpc"
)

type Run struct {
	Tool       string           `json:"tool"`
	Version    string           `json:"version"`
	Mode       string           `json:"mode,omitempty"`
	Timestamp  time.Time        `json:"timestamp"`
	Baseline   string           `json:"baseline"`
	Candidate  string           `json:"candidate"`
	Proxy      string           `json:"proxy,omitempty"`
	Timeout    string           `json:"timeout"`
	Requests   int              `json:"requestCount"`
	Summary    Summary          `json:"summary"`
	Results    []compare.Result `json:"results"`
	Disclaimer string           `json:"disclaimer"`
}

type Summary struct {
	Total                   int            `json:"total"`
	Matches                 int            `json:"matches"`
	CompatibilityMismatches int            `json:"compatibilityMismatches"`
	TransportFailures       int            `json:"transportFailures"`
	BaselineFailures        int            `json:"baselineFailures"`
	Skipped                 int            `json:"skipped"`
	ByCategory              map[string]int `json:"byCategory"`
	Slowest                 []SlowRequest  `json:"slowest"`
}

type SlowRequest struct {
	Method      string  `json:"method"`
	Index       int     `json:"index"`
	BaselineMS  float64 `json:"baselineMs"`
	CandidateMS float64 `json:"candidateMs"`
	MaxMS       float64 `json:"maxMs"`
}

const Version = "0.4.0"

func Build(baseline, candidate, timeout string, results []compare.Result) Run {
	return BuildMode("compare", baseline, candidate, "", timeout, results)
}

func BuildMode(mode, baseline, candidate, proxy, timeout string, results []compare.Result) Run {
	sum := Summary{
		Total:      len(results),
		ByCategory: map[string]int{},
	}
	slow := make([]SlowRequest, 0, len(results))
	for i, r := range results {
		sum.ByCategory[string(r.Classification)]++
		if baselineFailure(r.Baseline) {
			sum.BaselineFailures++
		}
		switch r.Classification {
		case compare.Match:
			sum.Matches++
		case compare.Timeout, compare.TransientFailure, compare.InvalidResponse, compare.Inconclusive:
			sum.TransportFailures++
		case compare.Skipped:
			sum.Skipped++
			if baselineFailure(r.Baseline) {
				sum.TransportFailures++
			}
		default:
			sum.CompatibilityMismatches++
		}
		max := r.Baseline.LatencyMS
		if r.Candidate.LatencyMS > max {
			max = r.Candidate.LatencyMS
		}
		slow = append(slow, SlowRequest{
			Method:      r.Method,
			Index:       i,
			BaselineMS:  r.Baseline.LatencyMS,
			CandidateMS: r.Candidate.LatencyMS,
			MaxMS:       max,
		})
	}
	sort.Slice(slow, func(i, j int) bool { return slow[i].MaxMS > slow[j].MaxMS })
	if len(slow) > 5 {
		slow = slow[:5]
	}
	sum.Slowest = slow
	return Run{
		Tool:       "rpcdiff",
		Version:    Version,
		Mode:       mode,
		Timestamp:  time.Now().UTC(),
		Baseline:   redactEndpoint(baseline),
		Candidate:  redactEndpoint(candidate),
		Proxy:      redactEndpoint(proxy),
		Timeout:    timeout,
		Requests:   len(results),
		Summary:    sum,
		Results:    results,
		Disclaimer: "This report compares JSON-RPC responses using deterministic rules. It does not prove semantic equivalence of Ethereum implementations or pin chain state.",
	}
}

func redactEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	return rpc.RedactURL(endpoint)
}

func baselineFailure(side compare.Side) bool {
	return side.HTTPError != "" || side.ParseError != "" || side.TimedOut || side.Transient || side.StatusCode >= 400
}

func WriteJSON(path string, run Run) error {
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func PrintSummary(w io.Writer, run Run) {
	fmt.Fprintf(w, "rpcdiff compare  %s vs %s\n", run.Baseline, run.Candidate)
	fmt.Fprintf(w, "timestamp        %s\n", run.Timestamp.Format(time.RFC3339))
	fmt.Fprintf(w, "total requests   %d\n", run.Summary.Total)
	fmt.Fprintf(w, "matches                 %d\n", run.Summary.Matches)
	fmt.Fprintf(w, "compatibility mismatches %d\n", run.Summary.CompatibilityMismatches)
	fmt.Fprintf(w, "transport failures       %d  (transient / timeout / invalid / inconclusive)\n", run.Summary.TransportFailures)
	fmt.Fprintf(w, "baseline failures        %d\n", run.Summary.BaselineFailures)
	fmt.Fprintf(w, "skipped                  %d  (write or unknown methods)\n", run.Summary.Skipped)
	fmt.Fprintln(w, "by category")
	cats := make([]string, 0, len(run.Summary.ByCategory))
	for k := range run.Summary.ByCategory {
		cats = append(cats, k)
	}
	sort.Strings(cats)
	for _, k := range cats {
		fmt.Fprintf(w, "  %-18s %d\n", k, run.Summary.ByCategory[k])
	}
	fmt.Fprintln(w, "slowest requests")
	if len(run.Summary.Slowest) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, s := range run.Summary.Slowest {
		fmt.Fprintf(w, "  [%d] %-28s max=%.2fms  baseline=%.2fms  candidate=%.2fms\n",
			s.Index, s.Method, s.MaxMS, s.BaselineMS, s.CandidateMS)
	}
	fmt.Fprintln(w, "mismatches")
	any := false
	for i, r := range run.Results {
		if r.Classification == compare.Match || r.Classification == compare.Skipped {
			continue
		}
		any = true
		path := r.DifferencePath
		if path == "" {
			path = "-"
		}
		fmt.Fprintf(w, "  [%d] %s  %s  %s\n", i, r.Classification, r.Method, path)
	}
	if !any {
		fmt.Fprintln(w, "  (none)")
	}
	fmt.Fprintln(w, strings.TrimSpace(run.Disclaimer))
}
