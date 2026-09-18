package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxBodyBytes = 8 << 20 // 8 MiB
const defaultRetries = 3
const maxRetries = 8

// CallOutcome is the recorded result of one HTTP JSON-RPC POST.
type CallOutcome struct {
	URL            string
	StatusCode     int
	Latency        time.Duration
	HTTPError      string
	Body           []byte
	Parsed         *Response
	ParseError     string
	TimedOut       bool
	Transient      bool
	Attempts       int
	JSONRPCError   bool
	InvalidRequest bool
}

// Client posts JSON-RPC requests over HTTP.
type Client struct {
	http    *http.Client
	timeout time.Duration
	retries int
}

func NewClient(timeout time.Duration) *Client {
	return NewClientWithRetries(timeout, defaultRetries)
}

// NewClientWithRetries creates a client with retries after the initial attempt.
func NewClientWithRetries(timeout time.Duration, retries int) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if retries < 0 {
		retries = 0
	}
	if retries > maxRetries {
		retries = maxRetries
	}
	return &Client{
		timeout: timeout,
		retries: retries,
		http: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				MaxIdleConns:        32,
				IdleConnTimeout:     30 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
}

// Call sends req to endpointURL, retrying temporary failures. Authorization
// headers are never set or logged.
func (c *Client) Call(ctx context.Context, endpointURL string, req Request) CallOutcome {
	if err := req.Validate(); err != nil {
		return CallOutcome{URL: endpointURL, HTTPError: err.Error(), InvalidRequest: true}
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return CallOutcome{URL: endpointURL, HTTPError: fmt.Sprintf("marshal request: %v", err), InvalidRequest: true}
	}
	return c.callPayload(ctx, endpointURL, payload)
}

// CallRaw sends an already encoded JSON-RPC request. It is used by the shadow
// proxy so the baseline sees the same request body that the application sent.
func (c *Client) CallRaw(ctx context.Context, endpointURL string, payload []byte) CallOutcome {
	if len(bytes.TrimSpace(payload)) == 0 {
		return CallOutcome{URL: endpointURL, HTTPError: "empty request body", InvalidRequest: true}
	}
	return c.callPayload(ctx, endpointURL, payload)
}

func (c *Client) callPayload(ctx context.Context, endpointURL string, payload []byte) CallOutcome {
	var last CallOutcome
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(100*(1<<(attempt-1))) * time.Millisecond
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				last = CallOutcome{URL: endpointURL, HTTPError: ctx.Err().Error(), Transient: true}
				last.Attempts = attempt
				return last
			case <-timer.C:
			}
		}
		last = c.callOnce(ctx, endpointURL, payload)
		last.Attempts = attempt + 1
		if !last.Transient || attempt == c.retries {
			return last
		}
	}
	return last
}

func (c *Client) callOnce(ctx context.Context, endpointURL string, payload []byte) CallOutcome {
	out := CallOutcome{URL: endpointURL}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(payload))
	if err != nil {
		out.HTTPError = fmt.Sprintf("build request: %v", err)
		return out
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.http.Do(httpReq)
	out.Latency = time.Since(start)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded || isTimeout(err) {
			out.TimedOut = true
			out.Transient = true
			out.HTTPError = "request timed out"
			return out
		}
		out.HTTPError = fmt.Sprintf("http do: %v", err)
		return out
	}
	defer resp.Body.Close()

	out.StatusCode = resp.StatusCode
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded || isTimeout(err) {
			out.TimedOut = true
			out.Transient = true
			out.HTTPError = "request timed out while reading body"
			return out
		}
		out.HTTPError = fmt.Sprintf("read body: %v", err)
		return out
	}
	if len(body) > maxBodyBytes {
		out.HTTPError = fmt.Sprintf("response body exceeds %d bytes", maxBodyBytes)
		return out
	}
	out.Body = body

	if resp.StatusCode >= 400 {
		out.HTTPError = fmt.Sprintf("http status %d", resp.StatusCode)
	}
	if isTransientStatus(resp.StatusCode) {
		out.Transient = true
	}

	parsed, parseErr := ParseResponse(body)
	if parseErr != nil {
		out.ParseError = parseErr.Error()
		return out
	}
	out.Parsed = parsed
	out.JSONRPCError = parsed.HasError()
	return out
}

func isTransientStatus(status int) bool {
	return status == http.StatusMovedPermanently || status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable
}

func ParseResponse(body []byte) (*Response, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty response body")
	}
	var raw Response
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("malformed json: %w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("malformed json: trailing data after first value")
	}
	raw.Raw = append(json.RawMessage(nil), trimmed...)
	if !raw.HasResult() && !raw.HasError() {
		return nil, fmt.Errorf("json-rpc response missing both result and error")
	}
	return &raw, nil
}

func isTimeout(err error) bool {
	type timeout interface {
		Timeout() bool
	}
	if t, ok := err.(timeout); ok && t.Timeout() {
		return true
	}
	return false
}
