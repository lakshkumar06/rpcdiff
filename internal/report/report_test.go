package report

import (
	"testing"

	"rpcdiff/internal/compare"
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
	run := BuildMode("shadow", "baseline", "candidate", "proxy", "1s", []compare.Result{
		{Classification: compare.Match},
		{Classification: compare.Skipped},
	})
	if run.Mode != "shadow" || run.Proxy != "proxy" {
		t.Fatalf("shadow metadata = mode %q proxy %q", run.Mode, run.Proxy)
	}
	if run.Summary.Skipped != 1 || run.Summary.CompatibilityMismatches != 0 || run.Summary.TransportFailures != 0 {
		t.Fatalf("summary = %+v", run.Summary)
	}
}
