package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCallTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(50 * time.Millisecond)
	out := c.Call(context.Background(), srv.URL, Request{JSONRPC: "2.0", Method: "eth_blockNumber", Params: json.RawMessage("[]")})
	if !out.TimedOut {
		t.Fatalf("expected timeout, got %+v", out)
	}
}

func TestCallRetriesTemporaryStatus(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	t.Cleanup(srv.Close)
	out := NewClientWithRetries(time.Second, 3).Call(context.Background(), srv.URL, Request{JSONRPC: "2.0", Method: "eth_blockNumber", Params: json.RawMessage("[]")})
	if out.Transient || out.Parsed == nil || out.Attempts != 3 {
		t.Fatalf("expected recovery after retries, got %+v", out)
	}
}

func TestCallDoesNotRetryStateChangingMethod(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	out := NewClientWithRetries(time.Second, 3).Call(context.Background(), srv.URL, Request{
		JSONRPC: "2.0", Method: "eth_sendTransaction", Params: json.RawMessage(`[]`), ID: json.RawMessage(`1`),
	})
	if calls.Load() != 1 || out.Attempts != 1 {
		t.Fatalf("state-changing request retried: calls=%d outcome=%+v", calls.Load(), out)
	}
}

func TestCallPersistentTemporaryStatusIsMarked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	out := NewClientWithRetries(time.Second, 2).Call(context.Background(), srv.URL, Request{JSONRPC: "2.0", Method: "eth_blockNumber", Params: json.RawMessage("[]")})
	if !out.Transient || out.StatusCode != http.StatusTooManyRequests || out.Attempts != 3 {
		t.Fatalf("expected transient failure after retries, got %+v", out)
	}
}

func TestCallMalformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(time.Second)
	out := c.Call(context.Background(), srv.URL, Request{JSONRPC: "2.0", Method: "eth_blockNumber", Params: json.RawMessage("[]")})
	if out.ParseError == "" {
		t.Fatalf("expected parse error, got %+v", out)
	}
}

func TestCallLatencyIncludesBodyAndTracksAttemptAndTotal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(40 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)
	out := NewClientWithRetries(time.Second, 0).Call(context.Background(), srv.URL, Request{
		JSONRPC: "2.0", Method: "eth_blockNumber", Params: json.RawMessage(`[]`), ID: json.RawMessage(`1`),
	})
	if out.Latency < 35*time.Millisecond || out.TotalLatency < out.Latency {
		t.Fatalf("latency did not include body completion: attempt=%s total=%s", out.Latency, out.TotalLatency)
	}
}

func TestParseResponseRejectsInvalidEnvelopes(t *testing.T) {
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"result":123,"error":{}}`,
		`{"jsonrpc":"2.0","id":1,"error":{}}`,
		`{"jsonrpc":"2.0","id":1,"error":{"code":0}}`,
		`{"jsonrpc":"2.0","id":1,"error":{"code":"0","message":""}}`,
	} {
		if _, err := ParseResponse([]byte(body)); err == nil {
			t.Fatalf("accepted invalid response %s", body)
		}
	}
}

func TestParseResponseAcceptsZeroCodeAndEmptyMessage(t *testing.T) {
	if _, err := ParseResponse([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":0,"message":""}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestRedactURLsAndTransportErrors(t *testing.T) {
	endpoint := "https://user:password@example.test/v2/path-api-key?api_key=query-secret&network=mainnet"
	redacted := RedactURL(endpoint)
	for _, secret := range []string{"password", "path-api-key", "query-secret"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("secret %q remained in %q", secret, redacted)
		}
	}
	if got := RedactText(`http do: Get "` + endpoint + `": timeout`); strings.Contains(got, "query-secret") || strings.Contains(got, "password") {
		t.Fatalf("transport error leaked credentials: %s", got)
	}
}

func TestLoadRequests(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "r.json")
	if err := os.WriteFile(p, []byte(`[{"id":1,"jsonrpc":"2.0","method":"eth_blockNumber","params":[]}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	reqs, err := LoadRequests(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 || reqs[0].Method != "eth_blockNumber" {
		t.Fatalf("%+v", reqs)
	}
}

func TestMissingMethodRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "r.json")
	if err := os.WriteFile(p, []byte(`[{"id":1,"jsonrpc":"2.0","params":[]}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRequests(p); err == nil {
		t.Fatal("expected error")
	}
}
