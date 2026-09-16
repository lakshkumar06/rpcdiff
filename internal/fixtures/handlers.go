package fixtures

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type Flavor string

const (
	Baseline  Flavor = "baseline"
	Candidate Flavor = "candidate"
)

func Handler(flavor Flavor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		id := req.ID
		if len(id) == 0 {
			id = json.RawMessage("1")
		}
		write := func(status int, payload string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, payload)
		}
		rpcOK := func(result string) {
			write(http.StatusOK, `{"jsonrpc":"2.0","id":`+string(id)+`,"result":`+result+`}`)
		}
		rpcErr := func(code int, msg string) {
			write(http.StatusOK, `{"jsonrpc":"2.0","id":`+string(id)+`,"error":{"code":`+itoa(code)+`,"message":`+quote(msg)+`}}`)
		}

		switch req.Method {
		case "eth_blockNumber":
			if flavor == Baseline {
				rpcOK(`"0x10"`)
			} else {
				// Same quantity, different hex padding and object-level envelope key order is not relevant.
				rpcOK(`"0x010"`)
			}
		case "eth_getBalance":
			if flavor == Baseline {
				write(http.StatusOK, `{"jsonrpc":"2.0","id":`+string(id)+`,"result":"0x0"}`)
			} else {
				write(http.StatusOK, `{"result":"0x00","jsonrpc":"2.0","id":`+string(id)+`}`)
			}
		case "eth_getCode":
			if flavor == Baseline {
				rpcOK(`"0x606060"`)
			} else {
				rpcOK(`"0X606060"`)
			}
		case "eth_getStorageAt":
			if flavor == Baseline {
				rpcOK(`"0x0000000000000000000000000000000000000000000000000000000000000001"`)
			} else {
				rpcOK(`"0x0000000000000000000000000000000000000000000000000000000000000002"`)
			}
		case "eth_getBlockByNumber":
			if flavor == Baseline {
				rpcOK(`{"number":"0x10","hash":"0xabc","miner":"0xAAA","extra":null,"transactions":["0x1","0x2"]}`)
			} else {
				rpcOK(`{"hash":"0xABC","number":"0x10","miner":"0xaaa","transactions":["0x1","0x2"]}`)
			}
		case "eth_getTransactionReceipt":
			if flavor == Baseline {
				rpcOK(`{"status":"0x1","logs":[{"address":"0x1","topics":["0xaa"]},{"address":"0x2","topics":["0xbb"]}]}`)
			} else {
				rpcOK(`{"status":"0x1","logs":[{"address":"0x2","topics":["0xbb"]},{"address":"0x1","topics":["0xaa"]}]}`)
			}
		case "eth_chainId":
			if flavor == Baseline {
				rpcOK(`"0x1"`)
			} else {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(2500 * time.Millisecond):
					rpcOK(`"0x1"`)
				}
			}
		case "eth_syncing":
			if flavor == Baseline {
				rpcOK(`false`)
			} else {
				write(http.StatusOK, `{not json`)
			}
		case "eth_gasPrice":
			if flavor == Baseline {
				rpcErr(-32000, "unavailable")
			} else {
				rpcOK(`"0x1"`)
			}
		case "eth_call":
			rpcOK(`"0x"`)
		default:
			rpcErr(-32601, "method not found: "+req.Method)
		}
	})
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n == 0 {
		return "0"
	}
	var d [12]byte
	i := len(d)
	for n > 0 {
		i--
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d[i:])
}

func DefaultRequests() []byte {
	return []byte(strings.TrimSpace(`[
  {"id": 1, "jsonrpc": "2.0", "method": "eth_blockNumber", "params": []},
  {"id": 2, "jsonrpc": "2.0", "method": "eth_getBalance", "params": ["0x0000000000000000000000000000000000000000", "latest"]},
  {"id": 3, "jsonrpc": "2.0", "method": "eth_getCode", "params": ["0x0000000000000000000000000000000000000001", "latest"]},
  {"id": 4, "jsonrpc": "2.0", "method": "eth_getStorageAt", "params": ["0x0000000000000000000000000000000000000001", "0x0", "latest"]},
  {"id": 5, "jsonrpc": "2.0", "method": "eth_getBlockByNumber", "params": ["0x10", false]},
  {"id": 6, "jsonrpc": "2.0", "method": "eth_getTransactionReceipt", "params": ["0x1"]},
  {"id": 7, "jsonrpc": "2.0", "method": "eth_gasPrice", "params": []},
  {"id": 8, "jsonrpc": "2.0", "method": "eth_chainId", "params": []},
  {"id": 9, "jsonrpc": "2.0", "method": "eth_syncing", "params": []},
  {"id": 10, "jsonrpc": "2.0", "method": "eth_call", "params": [{}, "latest"]}
]
`) + "\n")
}
