package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rpcdiff/internal/compare"
	"rpcdiff/internal/fixtures"
)

func TestRunAgainstFixtureServers(t *testing.T) {
	base := httptest.NewServer(fixtures.Handler(fixtures.Baseline))
	cand := httptest.NewServer(fixtures.Handler(fixtures.Candidate))
	t.Cleanup(base.Close)
	t.Cleanup(cand.Close)

	dir := t.TempDir()
	reqPath := filepath.Join(dir, "requests.json")
	if err := os.WriteFile(reqPath, fixtures.DefaultRequests(), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "report.json")
	html := filepath.Join(dir, "report.html")

	run, err := Run(context.Background(), Config{
		Baseline:  base.URL,
		Candidate: cand.URL,
		Requests:  reqPath,
		Output:    out,
		HTML:      html,
		Timeout:   400 * time.Millisecond,
		Workers:   2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Total != 10 {
		t.Fatalf("total %d", run.Summary.Total)
	}

	got := map[string]compare.Classification{}
	for _, r := range run.Results {
		got[r.Method] = r.Classification
	}
	want := map[string]compare.Classification{
		"eth_blockNumber":           compare.Match,
		"eth_getBalance":            compare.Match,
		"eth_getCode":               compare.Match,
		"eth_getStorageAt":          compare.ValueMismatch,
		"eth_getBlockByNumber":      compare.ShapeMismatch,
		"eth_getTransactionReceipt": compare.ValueMismatch,
		"eth_gasPrice":              compare.ErrorMismatch,
		"eth_chainId":               compare.TransientFailure,
		"eth_syncing":               compare.InvalidResponse,
		"eth_call":                  compare.Match,
	}
	for method, class := range want {
		if got[method] != class {
			t.Errorf("%s: got %s want %s", method, got[method], class)
		}
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["baseline"] != base.URL {
		t.Fatalf("report baseline %v", parsed["baseline"])
	}
}
