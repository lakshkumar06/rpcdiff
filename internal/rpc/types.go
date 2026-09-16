package rpc

import (
	"encoding/json"
	"fmt"
)

// Request is a JSON-RPC 2.0 request loaded from the input file.
type Request struct {
	ID      json.RawMessage `json:"id"`
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Error is a JSON-RPC error object.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Response is a parsed JSON-RPC 2.0 response body.
type Response struct {
	ID      json.RawMessage `json:"id"`
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *Error          `json:"error"`
	// Raw is the original HTTP body.
	Raw json.RawMessage `json:"-"`
}

func (r Request) Validate() error {
	if r.Method == "" {
		return fmt.Errorf("json-rpc request is missing method")
	}
	if r.JSONRPC != "" && r.JSONRPC != "2.0" {
		return fmt.Errorf("unsupported jsonrpc version %q", r.JSONRPC)
	}
	return nil
}

// HasResult reports whether the result member was present, including JSON null.
func (r Response) HasResult() bool {
	return len(r.Result) > 0
}

func (r Response) HasError() bool {
	return r.Error != nil
}
