package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
