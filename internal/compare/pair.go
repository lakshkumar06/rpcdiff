package compare

import (
	"encoding/json"
	"fmt"

	"rpcdiff/internal/rpc"
)

// Pair compares two JSON-RPC call outcomes after optional normalization of the JSON bodies.
func Pair(method string, params json.RawMessage, baseline, candidate rpc.CallOutcome, normalized bool) Result {
	return PairWithOptions(method, params, baseline, candidate, normalized, Options{})
}

// Options controls comparison policy. The default is deliberately strict.
type Options struct {
	// IgnoreErrorMessages treats provider-specific JSON-RPC error wording as
	// non-semantic, while continuing to compare error code and data.
	IgnoreErrorMessages bool
}

// PairWithOptions compares two outcomes using an explicit migration policy.
func PairWithOptions(method string, params json.RawMessage, baseline, candidate rpc.CallOutcome, normalized bool, options Options) Result {
	diffs := diffOutcomes(baseline, candidate)
	ignoredErrorMessage := false
	if options.IgnoreErrorMessages {
		kept := diffs[:0]
		for _, diff := range diffs {
			if diff.Path == "error.message" {
				ignoredErrorMessage = true
				continue
			}
			kept = append(kept, diff)
		}
		diffs = kept
	}
	result := Classify(method, params, baseline, candidate, diffs, normalized)
	if ignoredErrorMessage {
		result.Notes = append(result.Notes, "ignored provider-specific JSON-RPC error.message difference")
	}
	return result
}

func Classify(method string, params json.RawMessage, baseline, candidate rpc.CallOutcome, diffs []Diff, normalized bool) Result {
	res := Result{
		Method:           method,
		Params:           params,
		Diffs:            diffs,
		NormalizedMethod: normalized,
		Baseline:         sideFrom(baseline),
		Candidate:        sideFrom(candidate),
	}
	res.Classification, res.Notes = classify(baseline, candidate, diffs)
	if res.DifferencePath == "" && len(diffs) > 0 {
		res.DifferencePath = diffs[0].Path
	}
	if res.Classification == ErrorMismatch && res.DifferencePath == "" {
		if baseline.JSONRPCError {
			res.DifferencePath = "error"
		} else {
			res.DifferencePath = "result"
		}
	}
	return res
}

func diffOutcomes(baseline, candidate rpc.CallOutcome) []Diff {
	if baseline.TimedOut || candidate.TimedOut || invalid(baseline) || invalid(candidate) || baseline.InvalidRequest || candidate.InvalidRequest {
		return nil
	}
	b := baseline.Parsed
	c := candidate.Parsed
	if b.HasError() && c.HasError() {
		return prefixDiffs("error", compareRaw(errorBody(b.Error), errorBody(c.Error)))
	}
	if b.HasError() != c.HasError() {
		path := "result"
		if b.HasError() {
			path = "error"
		}
		return []Diff{{
			Path:    path,
			Kind:    DiffValue,
			Left:    presence(b),
			Right:   presence(c),
			Message: "JSON-RPC error presence differs",
		}}
	}
	return prefixDiffs("result", compareRaw(b.Result, c.Result))
}

func compareRaw(left, right json.RawMessage) []Diff {
	lv, errL := Parse(left)
	rv, errR := Parse(right)
	if errL != nil || errR != nil {
		msg := "unable to parse JSON value"
		if errL != nil {
			msg = errL.Error()
		} else if errR != nil {
			msg = errR.Error()
		}
		return []Diff{{
			Path:    "",
			Kind:    DiffShape,
			Left:    string(left),
			Right:   string(right),
			Message: msg,
		}}
	}
	return Equal(lv, rv)
}

func prefixDiffs(prefix string, diffs []Diff) []Diff {
	out := make([]Diff, len(diffs))
	for i, d := range diffs {
		d.Path = joinPath(prefix, d.Path)
		out[i] = d
	}
	return out
}

func joinPath(prefix, path string) string {
	if path == "" {
		return prefix
	}
	if path[0] == '[' {
		return prefix + path
	}
	return prefix + "." + path
}

func errorBody(e *rpc.Error) json.RawMessage {
	if e == nil {
		return json.RawMessage("null")
	}
	b, err := json.Marshal(e)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

func presence(r *rpc.Response) string {
	if r.HasError() {
		return fmt.Sprintf("error(code=%d)", r.Error.Code)
	}
	return "result"
}
