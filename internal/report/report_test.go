package report

import (
	"testing"

	"rpcdiff/internal/compare"
	"rpcdiff/internal/rpc"
)

func TestBuildSummarySeparatesCompatibilityAndTransport(t *testing.T) {
	run := Build("baseline", "candidate", "1s", []compare.Result{
		{Classification: compare.Match},
		{Classification: compare.ValueMismatch},
		{Classification: compare.ErrorMismatch},
		{Classification: compare.InvalidResponse},
		{Classification: compare.Timeout},
	})

	if run.Summary.Matches != 1 {
		t.Fatalf("matches = %d, want 1", run.Summary.Matches)
	}
	if run.Summary.CompatibilityMismatches != 2 {
		t.Fatalf("compatibility mismatches = %d, want 2", run.Summary.CompatibilityMismatches)
	}
	if run.Summary.TransportFailures != 2 {
		t.Fatalf("transport failures = %d, want 2", run.Summary.TransportFailures)
	}
}

func TestBuildSummaryTracksSkippedShadowRequests(t *testing.T) {
	run := BuildMode("shadow", "https://baseline.test", "https://candidate.test", "http://proxy.test", "1s", []compare.Result{
		{Classification: compare.Match},
		{Classification: compare.Skipped},
	})
	if run.Mode != "shadow" || run.Proxy != "http://proxy.test" {
		t.Fatalf("shadow metadata = mode %q proxy %q", run.Mode, run.Proxy)
	}
	if run.Summary.Skipped != 1 || run.Summary.CompatibilityMismatches != 0 || run.Summary.TransportFailures != 0 {
		t.Fatalf("summary = %+v", run.Summary)
	}
}

func TestSkippedBaselineFailureIsTransportFailure(t *testing.T) {
	run := BuildMode("shadow", "https://user:secret@example.test/v2/path-key?token=query-secret", "https://candidate.test", "http://127.0.0.1:1", "1s", []compare.Result{
		compare.SkippedResult("eth_sendRawTransaction", nil, rpc.CallOutcome{
			StatusCode: 503,
			HTTPError:  "http status 503",
		}, "write skipped"),
	})
	if run.Summary.Skipped != 1 || run.Summary.TransportFailures != 1 || run.Summary.BaselineFailures != 1 {
		t.Fatalf("summary = %+v", run.Summary)
	}
	for _, value := range []string{run.Baseline, run.Candidate} {
		if value == "" {
			t.Fatal("expected endpoint metadata")
		}
	}
	if run.Baseline != "https://redacted@example.test/v2/%3Credacted%3E?token=%3Credacted%3E" {
		t.Fatalf("baseline was not redacted: %s", run.Baseline)
	}
}
