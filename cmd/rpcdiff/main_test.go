package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rpcdiff/internal/fixtures"
	"rpcdiff/internal/report"
)

func TestCompareCLI(t *testing.T) {
	base := httptest.NewServer(fixtures.Handler(fixtures.Baseline))
	cand := httptest.NewServer(fixtures.Handler(fixtures.Candidate))
	t.Cleanup(base.Close)
	t.Cleanup(cand.Close)

	dir := t.TempDir()
	req := filepath.Join(dir, "requests.json")
	if err := os.WriteFile(req, []byte(`[
		{"id":1,"jsonrpc":"2.0","method":"eth_blockNumber","params":[]},
		{"id":2,"jsonrpc":"2.0","method":"eth_getStorageAt","params":[]}
	]`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "report.json")
	html := filepath.Join(dir, "report.html")

	err := runCompare([]string{
		"--baseline", base.URL,
		"--candidate", cand.URL,
		"--requests", req,
		"--output", out,
		"--html", html,
		"--timeout", "1s",
		"--workers", "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Results []struct {
			Method         string `json:"method"`
			Classification string `json:"classification"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Results) != 2 {
		t.Fatalf("results %d", len(parsed.Results))
	}
	if parsed.Results[0].Classification != "MATCH" {
		t.Fatalf("blockNumber %s", parsed.Results[0].Classification)
	}
	if parsed.Results[1].Classification != "VALUE_MISMATCH" {
		t.Fatalf("storage %s", parsed.Results[1].Classification)
	}
	if _, err := os.Stat(html); err != nil {
		t.Fatal(err)
	}
	_ = time.Second
}

func TestGateFailureSummaryNamesFailureTypes(t *testing.T) {
	if got := gateFailureSummary(report.Summary{TransportFailures: 3}); got != "3 transport failures" {
		t.Fatalf("transport-only summary = %q", got)
	}
	if got := gateFailureSummary(report.Summary{CompatibilityMismatches: 2, TransportFailures: 1}); got != "2 compatibility mismatches and 1 transport failures" {
		t.Fatalf("mixed summary = %q", got)
	}
}

func TestShadowCIFailsOnlyForMeaningfulResults(t *testing.T) {
	if shadowCIFails(report.Summary{Skipped: 2}) {
		t.Fatal("skipped writes should not fail shadow CI")
	}
	if !shadowCIFails(report.Summary{CompatibilityMismatches: 1}) {
		t.Fatal("compatibility mismatch should fail shadow CI")
	}
	if !shadowCIFails(report.Summary{TransportFailures: 1}) {
		t.Fatal("provider failure should fail shadow CI")
	}
}
