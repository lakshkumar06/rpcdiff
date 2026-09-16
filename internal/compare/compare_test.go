package compare

import (
	"encoding/json"
	"strings"
	"testing"

	"rpcdiff/internal/rpc"
)

func mustParse(t *testing.T, s string) Value {
	t.Helper()
	v, err := Parse(json.RawMessage(s))
	if err != nil {
		t.Fatalf("parse %s: %v", s, err)
	}
	return v
}

func TestEqualDifferentKeyOrder(t *testing.T) {
	left := mustParse(t, `{"b":1,"a":2}`)
	right := mustParse(t, `{"a":2,"b":1}`)
	if diffs := Equal(left, right); len(diffs) != 0 {
		t.Fatalf("expected match, got %#v", diffs)
	}
}

func TestPrimitiveMismatch(t *testing.T) {
	left := mustParse(t, `"0x1"`)
	right := mustParse(t, `"0x2"`)
	diffs := Equal(left, right)
	if len(diffs) != 1 || diffs[0].Kind != DiffValue {
		t.Fatalf("expected value mismatch, got %#v", diffs)
	}
}

func TestMissingVersusNull(t *testing.T) {
	left := mustParse(t, `{"balance":null}`)
	right := mustParse(t, `{}`)
	diffs := Equal(left, right)
	if len(diffs) != 1 || diffs[0].Kind != DiffMissingNull {
		t.Fatalf("expected missing vs null, got %#v", diffs)
	}
	if diffs[0].Path != "balance" {
		t.Fatalf("path = %q", diffs[0].Path)
	}
}

func TestArrayOrderMismatch(t *testing.T) {
	left := mustParse(t, `["a","b"]`)
	right := mustParse(t, `["b","a"]`)
	diffs := Equal(left, right)
	if len(diffs) != 2 {
		t.Fatalf("expected two index diffs, got %#v", diffs)
	}
	if diffs[0].Path != "[0]" || diffs[0].Kind != DiffValue {
		t.Fatalf("unexpected first diff %#v", diffs[0])
	}
}

func mustOutcome(t *testing.T, body string, timedOut bool, status int) rpc.CallOutcome {
	t.Helper()
	out := rpc.CallOutcome{StatusCode: status, Body: []byte(body), TimedOut: timedOut}
	if status >= 400 {
		out.HTTPError = "http status"
	}
	if timedOut {
		out.HTTPError = "request timed out"
		return out
	}
	if body == "" && status >= 400 {
		return out
	}
	var parsed rpc.Response
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		out.ParseError = err.Error()
		return out
	}
	out.Parsed = &parsed
	out.JSONRPCError = parsed.HasError()
	if !parsed.HasResult() && !parsed.HasError() {
		out.ParseError = "json-rpc response missing both result and error"
		out.Parsed = nil
	}
	return out
}

func TestJSONRPCErrorMismatch(t *testing.T) {
	base := mustOutcome(t, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"nope"}}`, false, 200)
	cand := mustOutcome(t, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`, false, 200)
	res := Pair("eth_blockNumber", json.RawMessage("[]"), base, cand, false)
	if res.Classification != ErrorMismatch {
		t.Fatalf("got %s", res.Classification)
	}
	if res.DifferencePath != "error" {
		t.Fatalf("path %q", res.DifferencePath)
	}
}

func TestTimeoutClassification(t *testing.T) {
	base := mustOutcome(t, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`, false, 200)
	cand := rpc.CallOutcome{TimedOut: true, HTTPError: "request timed out"}
	res := Pair("eth_blockNumber", json.RawMessage("[]"), base, cand, false)
	if res.Classification != Timeout {
		t.Fatalf("got %s", res.Classification)
	}
}

func TestMalformedJSONClassification(t *testing.T) {
	base := mustOutcome(t, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`, false, 200)
	cand := rpc.CallOutcome{StatusCode: 200, Body: []byte(`not-json`), ParseError: "malformed json"}
	res := Pair("eth_blockNumber", json.RawMessage("[]"), base, cand, false)
	if res.Classification != InvalidResponse {
		t.Fatalf("got %s", res.Classification)
	}
}

func TestEachClassificationCategory(t *testing.T) {
	ok := `{"jsonrpc":"2.0","id":1,"result":{"n":"0x1","xs":[1,2]}}`
	okReordered := `{"jsonrpc":"2.0","id":1,"result":{"xs":[1,2],"n":"0x1"}}`
	value := `{"jsonrpc":"2.0","id":1,"result":{"n":"0x2","xs":[1,2]}}`
	shape := `{"jsonrpc":"2.0","id":1,"result":{"n":"0x1"}}`
	rpcErr := `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"missing"}}`
	rpcErr2 := `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"missing"}}`

	cases := []struct {
		name string
		base rpc.CallOutcome
		cand rpc.CallOutcome
		want Classification
		path string
	}{
		{"match", mustOutcome(t, ok, false, 200), mustOutcome(t, okReordered, false, 200), Match, ""},
		{"value", mustOutcome(t, ok, false, 200), mustOutcome(t, value, false, 200), ValueMismatch, "result.n"},
		{"shape", mustOutcome(t, ok, false, 200), mustOutcome(t, shape, false, 200), ShapeMismatch, "result.xs"},
		{"error", mustOutcome(t, rpcErr, false, 200), mustOutcome(t, ok, false, 200), ErrorMismatch, "error"},
		{"error-code", mustOutcome(t, rpcErr, false, 200), mustOutcome(t, rpcErr2, false, 200), ErrorMismatch, "error.code"},
		{"timeout", mustOutcome(t, ok, false, 200), rpc.CallOutcome{TimedOut: true, HTTPError: "request timed out"}, Timeout, ""},
		{"invalid", mustOutcome(t, ok, false, 200), rpc.CallOutcome{StatusCode: 500, HTTPError: "http status 500", Body: []byte("oops")}, InvalidResponse, ""},
		{"inconclusive", rpc.CallOutcome{InvalidRequest: true, HTTPError: "missing method"}, mustOutcome(t, ok, false, 200), Inconclusive, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Pair("eth_getBalance", json.RawMessage("[]"), tc.base, tc.cand, false)
			if res.Classification != tc.want {
				t.Fatalf("got %s want %s diffs=%#v", res.Classification, tc.want, res.Diffs)
			}
			if tc.path != "" && res.DifferencePath != tc.path {
				t.Fatalf("path got %q want %q", res.DifferencePath, tc.path)
			}
		})
	}
}

func TestNumericLiteralMismatchIsValue(t *testing.T) {
	left := mustParse(t, `1`)
	right := mustParse(t, `1.0`)
	diffs := Equal(left, right)
	if len(diffs) != 1 || diffs[0].Kind != DiffValue {
		t.Fatalf("JSON number literals must compare as written, got %#v", diffs)
	}
}

func TestNestedPath(t *testing.T) {
	left := mustParse(t, `{"logs":[{"address":"0x1"}]}`)
	right := mustParse(t, `{"logs":[{"address":"0x2"}]}`)
	diffs := Equal(left, right)
	if len(diffs) != 1 || diffs[0].Path != "logs[0].address" {
		t.Fatalf("got %#v", diffs)
	}
}

func TestShapeArrayLength(t *testing.T) {
	ok := mustOutcome(t, `{"jsonrpc":"2.0","id":1,"result":[1,2]}`, false, 200)
	short := mustOutcome(t, `{"jsonrpc":"2.0","id":1,"result":[1]}`, false, 200)
	res := Pair("custom", json.RawMessage("[]"), ok, short, false)
	if res.Classification != ShapeMismatch {
		t.Fatalf("got %s", res.Classification)
	}
	if !strings.Contains(res.DifferencePath, "result") {
		t.Fatalf("path %q", res.DifferencePath)
	}
}
