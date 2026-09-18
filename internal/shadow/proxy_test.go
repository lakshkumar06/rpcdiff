package shadow

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"rpcdiff/internal/compare"
)

func TestProxyRecordsMatchingRead(t *testing.T) {
	var candidateCalls atomic.Int32
	var baselineBody string
	results, body, _ := exerciseProxy(t,
		func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			baselineBody = string(raw)
			writeFixtureResponse(w, `{"jsonrpc":"2.0","id":7,"result":{"ok":true}}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			candidateCalls.Add(1)
			writeFixtureResponse(w, `{"jsonrpc":"2.0","id":7,"result":{"ok":true}}`)
		},
		`{"jsonrpc":"2.0","id":7,"method":"eth_call","params":[{"to":"0x1"},"latest"]}`,
	)

	if string(body) != `{"jsonrpc":"2.0","id":7,"result":{"ok":true}}` {
		t.Fatalf("proxy body = %s", body)
	}
	if candidateCalls.Load() != 1 {
		t.Fatalf("candidate calls = %d, want 1", candidateCalls.Load())
	}
	if baselineBody != `{"jsonrpc":"2.0","id":7,"method":"eth_call","params":[{"to":"0x1"},"latest"]}` {
		t.Fatalf("baseline body = %s", baselineBody)
	}
	if len(results) != 1 || results[0].Classification != compare.Match {
		t.Fatalf("results = %+v", results)
	}
	if got := string(results[0].Params); got != `[{"to":"0x1"},"latest"]` {
		t.Fatalf("params = %s", got)
	}
}

func TestProxyRecordsMismatchAndProviderError(t *testing.T) {
	results, _, _ := exerciseProxy(t,
		func(w http.ResponseWriter, r *http.Request) {
			writeFixtureResponse(w, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			writeFixtureResponse(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"candidate unavailable"}}`)
		},
		`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`,
	)
	if len(results) != 1 || results[0].Classification != compare.ErrorMismatch {
		t.Fatalf("results = %+v", results)
	}
	if !results[0].Candidate.JSONRPCError || results[0].Candidate.Response == nil {
		t.Fatalf("candidate provider error was not recorded: %+v", results[0].Candidate)
	}
}

func TestProxyCandidateTimeoutDoesNotDelayBaseline(t *testing.T) {
	started := make(chan struct{})
	results, _, elapsed := exerciseProxyWithConfig(t, Config{
		Timeout: 40 * time.Millisecond,
	},
		func(w http.ResponseWriter, r *http.Request) {
			writeFixtureResponse(w, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			close(started)
			time.Sleep(200 * time.Millisecond)
			writeFixtureResponse(w, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
		},
		`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`,
	)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("candidate request did not start")
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("baseline response took %s while candidate was slow", elapsed)
	}
	if len(results) != 1 || !results[0].Candidate.TimedOut {
		t.Fatalf("timeout was not recorded: %+v", results)
	}
}

func TestProxySkipsWriteMethod(t *testing.T) {
	var candidateCalls atomic.Int32
	results, _, _ := exerciseProxy(t,
		func(w http.ResponseWriter, r *http.Request) {
			writeFixtureResponse(w, `{"jsonrpc":"2.0","id":3,"result":"0xhash"}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			candidateCalls.Add(1)
			writeFixtureResponse(w, `{"jsonrpc":"2.0","id":3,"result":"should not happen"}`)
		},
		`{"jsonrpc":"2.0","id":3,"method":"eth_sendRawTransaction","params":["0xdeadbeef"]}`,
	)
	if candidateCalls.Load() != 0 {
		t.Fatalf("write method was duplicated %d times", candidateCalls.Load())
	}
	if len(results) != 1 || results[0].Classification != compare.Skipped {
		t.Fatalf("results = %+v", results)
	}
	if len(results[0].Notes) == 0 {
		t.Fatal("skipped result has no reason")
	}
}

func exerciseProxy(t *testing.T, baseline, candidate func(http.ResponseWriter, *http.Request), payload string) ([]compare.Result, []byte, time.Duration) {
	return exerciseProxyWithConfig(t, Config{Timeout: time.Second}, baseline, candidate, payload)
}

func exerciseProxyWithConfig(t *testing.T, cfg Config, baseline, candidate func(http.ResponseWriter, *http.Request), payload string) ([]compare.Result, []byte, time.Duration) {
	t.Helper()
	baseServer := httptest.NewServer(http.HandlerFunc(baseline))
	candidateServer := httptest.NewServer(http.HandlerFunc(candidate))
	proxy, err := NewProxy(Config{
		Baseline: baseServer.URL, Candidate: candidateServer.URL, Timeout: cfg.Timeout,
		Retries: cfg.Retries, IgnoreErrorMessages: cfg.IgnoreErrorMessages,
	})
	if err != nil {
		t.Fatal(err)
	}
	proxyServer := httptest.NewServer(proxy.Handler())
	t.Cleanup(func() {
		proxyServer.Close()
		baseServer.Close()
		candidateServer.Close()
	})

	start := time.Now()
	resp, err := http.Post(proxyServer.URL, "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d, body = %s", resp.StatusCode, body)
	}
	responseElapsed := time.Since(start)
	results := proxy.Wait()
	return results, body, responseElapsed
}

func writeFixtureResponse(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func TestReadOnlyAllowlistDoesNotContainKnownWrites(t *testing.T) {
	for _, method := range []string{"eth_sendRawTransaction", "eth_sendTransaction", "personal_sendTransaction", "evm_mine"} {
		if ReadOnlyMethods[method] {
			t.Errorf("write method %s is allowlisted", method)
		}
	}
}
