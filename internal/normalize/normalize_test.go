package normalize

import (
	"encoding/json"
	"testing"

	"rpcdiff/internal/rpc"
)

func TestCanonicalQuantity(t *testing.T) {
	got, ok := CanonicalQuantity("0x00")
	if !ok || got != "0x0" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	got, ok = CanonicalQuantity("0X0A")
	if !ok || got != "0xa" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestDoesNotPadData(t *testing.T) {
	got, ok := CanonicalData("0xAb")
	if !ok || got != "0xab" {
		t.Fatalf("got %q", got)
	}
	if got == "0x00ab" {
		t.Fatal("DATA must not be padded")
	}
}

func TestApplyBlockNumber(t *testing.T) {
	out := rpc.CallOutcome{
		Body: []byte(`{"jsonrpc":"2.0","id":1,"result":"0x01"}`),
		Parsed: &rpc.Response{
			Result: json.RawMessage(`"0x01"`),
		},
	}
	got := Apply("eth_blockNumber", out)
	if !got.Applied {
		t.Fatal("expected apply")
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(got.Body, &env); err != nil {
		t.Fatal(err)
	}
	if string(env["result"]) != `"0x1"` {
		t.Fatalf("result %s", env["result"])
	}
}

func TestUnknownMethodUnchanged(t *testing.T) {
	out := rpc.CallOutcome{Body: []byte(`{"jsonrpc":"2.0","id":1,"result":"0x01"}`)}
	got := Apply("eth_chainId", out)
	if got.Applied || !got.Unsupported {
		t.Fatalf("unexpected %+v", got)
	}
}
