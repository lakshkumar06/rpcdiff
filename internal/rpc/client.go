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
	JSONRPCError   bool
	InvalidRequest bool
}

// Client posts JSON-RPC requests over HTTP.
type Client struct {
	http    *http.Client
	timeout time.Duration
}

func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		timeout: timeout,
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				MaxIdleConns:        32,
				IdleConnTimeout:     30 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
}

// Call sends req to endpointURL. Authorization headers are never set or logged.
func (c *Client) Call(ctx context.Context, endpointURL string, req Request) CallOutcome {
	out := CallOutcome{URL: endpointURL}
	if err := req.Validate(); err != nil {
		out.InvalidRequest = true
		out.HTTPError = err.Error()
		return out
	}

	payload, err := json.Marshal(req)
	if err != nil {
		out.InvalidRequest = true
		out.HTTPError = fmt.Sprintf("marshal request: %v", err)
		return out
	}

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

	parsed, parseErr := parseResponse(body)
	if parseErr != nil {
		out.ParseError = parseErr.Error()
		return out
	}
	out.Parsed = parsed
	out.JSONRPCError = parsed.HasError()
	return out
}

func parseResponse(body []byte) (*Response, error) {
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
