package compare

import (
	"encoding/json"
	"time"

	"rpcdiff/internal/rpc"
)

type Classification string

const (
	Match            Classification = "MATCH"
	ValueMismatch    Classification = "VALUE_MISMATCH"
	ErrorMismatch    Classification = "ERROR_MISMATCH"
	ShapeMismatch    Classification = "SHAPE_MISMATCH"
	Timeout          Classification = "TIMEOUT"
	InvalidResponse  Classification = "INVALID_RESPONSE"
	Inconclusive     Classification = "INCONCLUSIVE"
	TransientFailure Classification = "TRANSIENT_FAILURE"
	Skipped          Classification = "SKIPPED"
)

// Result is the comparison of one request against both endpoints.
type Result struct {
	Method           string          `json:"method"`
	Params           json.RawMessage `json:"params"`
	Classification   Classification  `json:"classification"`
	DifferencePath   string          `json:"differencePath,omitempty"`
	Diffs            []Diff          `json:"diffs,omitempty"`
	Baseline         Side            `json:"baseline"`
	Candidate        Side            `json:"candidate"`
	Notes            []string        `json:"notes,omitempty"`
	NormalizedMethod bool            `json:"normalizedMethod"`
}

type Side struct {
	LatencyMS        float64         `json:"latencyMs"`
	AttemptLatencyMS float64         `json:"attemptLatencyMs"`
	TotalLatencyMS   float64         `json:"totalLatencyMs"`
	StatusCode       int             `json:"statusCode"`
	HTTPError        string          `json:"httpError,omitempty"`
	TimedOut         bool            `json:"timedOut"`
	Transient        bool            `json:"transient"`
	Attempts         int             `json:"attempts"`
	JSONRPCError     bool            `json:"jsonrpcError"`
	ParseError       string          `json:"parseError,omitempty"`
	InvalidRequest   bool            `json:"invalidRequest,omitempty"`
	Response         json.RawMessage `json:"response,omitempty"`
}

func classify(baseline, candidate rpc.CallOutcome, diffs []Diff) (Classification, []string) {
	var notes []string
	if baseline.InvalidRequest || candidate.InvalidRequest {
		return Inconclusive, []string{"request was not a valid JSON-RPC call"}
	}
	if baseline.Transient || candidate.Transient {
		return TransientFailure, []string{"temporary provider failure remained after retries"}
	}
	if baseline.TimedOut || candidate.TimedOut {
		return Timeout, notes
	}
	if invalid(baseline) || invalid(candidate) {
		return InvalidResponse, notes
	}

	bErr := baseline.JSONRPCError
	cErr := candidate.JSONRPCError
	if bErr != cErr {
		if len(diffs) == 0 {
			diffs = []Diff{{
				Path:    errorPresencePath(bErr),
				Kind:    DiffValue,
				Message: "one endpoint returned a JSON-RPC error and the other returned a result",
			}}
		}
		return ErrorMismatch, notes
	}
	if bErr && cErr {
		if hasDiffs(diffs) {
			return ErrorMismatch, notes
		}
		return Match, notes
	}

	if !hasDiffs(diffs) {
		return Match, notes
	}
	if shapeDiff(diffs) {
		return ShapeMismatch, notes
	}
	return ValueMismatch, notes
}

func errorPresencePath(baselineHasError bool) string {
	if baselineHasError {
		return "error"
	}
	return "result"
}

func invalid(o rpc.CallOutcome) bool {
	if o.ParseError != "" {
		return true
	}
	if o.HTTPError != "" && !o.TimedOut {
		return true
	}
	if o.Parsed == nil {
		return true
	}
	return false
}

func hasDiffs(diffs []Diff) bool {
	return len(diffs) > 0
}

func shapeDiff(diffs []Diff) bool {
	for _, d := range diffs {
		switch d.Kind {
		case DiffType, DiffArrayLength, DiffMissingField, DiffExtraField, DiffMissingNull, DiffShape:
			return true
		}
	}
	return false
}

func sideFrom(o rpc.CallOutcome) Side {
	s := Side{
		LatencyMS:        float64(o.TotalLatency) / float64(time.Millisecond),
		AttemptLatencyMS: float64(o.Latency) / float64(time.Millisecond),
		TotalLatencyMS:   float64(o.TotalLatency) / float64(time.Millisecond),
		StatusCode:       o.StatusCode,
		HTTPError:        rpc.RedactText(o.HTTPError),
		TimedOut:         o.TimedOut,
		Transient:        o.Transient,
		Attempts:         o.Attempts,
		JSONRPCError:     o.JSONRPCError,
		ParseError:       o.ParseError,
		InvalidRequest:   o.InvalidRequest,
	}
	if o.TotalLatency == 0 {
		s.LatencyMS = s.AttemptLatencyMS
		s.TotalLatencyMS = s.AttemptLatencyMS
	}
	if len(o.Body) > 0 {
		if json.Valid(o.Body) {
			s.Response = append(json.RawMessage(nil), o.Body...)
		} else {
			quoted, err := json.Marshal(string(o.Body))
			if err == nil {
				s.Response = quoted
			}
		}
	}
	return s
}
