package normalize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"rpcdiff/internal/compare"
	"rpcdiff/internal/rpc"
)

// Methods that receive conservative, documented normalization.
var Supported = map[string]bool{
	"eth_blockNumber":           true,
	"eth_getBalance":            true,
	"eth_getCode":               true,
	"eth_getStorageAt":          true,
	"eth_getBlockByNumber":      true,
	"eth_getTransactionReceipt": true,
}

type Result struct {
	Body        json.RawMessage
	Applied     bool
	Notes       []string
	Unsupported bool
}

// Apply normalizes a successful JSON-RPC result for a known method.
// Differences are never dropped unless a rule below says so.
//
// Rules (MVP):
//   - QUANTITY hex strings: lowercase and strip leading zeros (0x00 -> 0x0, 0x01 -> 0x1).
//   - DATA / hash / address hex strings: lowercase only. Bytes are not padded or trimmed.
//   - Extra object fields are kept.
//   - JSON-RPC errors are not rewritten.
//
// Outcome returns a copy of outcome with a normalized body and parsed envelope when rules apply.
func Outcome(method string, outcome rpc.CallOutcome) (rpc.CallOutcome, Result) {
	res := Apply(method, outcome)
	if !res.Applied {
		return outcome, res
	}
	outcome.Body = res.Body
	parsed, err := rpc.ParseResponse(res.Body)
	if err != nil {
		res.Notes = append(res.Notes, "reparse after normalize: "+err.Error())
		return outcome, res
	}
	outcome.Parsed = parsed
	outcome.JSONRPCError = parsed.HasError()
	outcome.ParseError = ""
	return outcome, res
}

func Apply(method string, outcome rpc.CallOutcome) Result {
	if !Supported[method] {
		return Result{Body: outcome.Body, Unsupported: true}
	}
	if outcome.Parsed == nil || outcome.Parsed.HasError() || !outcome.Parsed.HasResult() {
		return Result{Body: outcome.Body, Applied: false}
	}
	v, err := compare.Parse(outcome.Parsed.Result)
	if err != nil {
		return Result{Body: outcome.Body, Notes: []string{"skip normalize: " + err.Error()}}
	}
	notes := []string{}
	nv := normalizeValue(method, v, &notes)
	raw, err := encode(nv)
	if err != nil {
		return Result{Body: outcome.Body, Notes: []string{"skip normalize encode: " + err.Error()}}
	}
	rewritten := rewriteResult(outcome.Body, outcome.Parsed.Result, raw)
	return Result{Body: rewritten, Applied: true, Notes: notes}
}

func normalizeValue(method string, v compare.Value, notes *[]string) compare.Value {
	switch method {
	case "eth_blockNumber", "eth_getBalance":
		return normalizeQuantityValue(v, notes)
	case "eth_getCode", "eth_getStorageAt":
		return normalizeDataValue(v, notes)
	case "eth_getBlockByNumber":
		return normalizeBlock(v, notes)
	case "eth_getTransactionReceipt":
		return normalizeReceipt(v, notes)
	default:
		return v
	}
}

func normalizeQuantityValue(v compare.Value, notes *[]string) compare.Value {
	if v.Kind != compare.KindString {
		return v
	}
	canon, ok := CanonicalQuantity(v.Str)
	if ok && canon != v.Str {
		*notes = append(*notes, fmt.Sprintf("quantity %s -> %s", v.Str, canon))
		v.Str = canon
	}
	return v
}

func normalizeDataValue(v compare.Value, notes *[]string) compare.Value {
	if v.Kind != compare.KindString {
		return v
	}
	canon, ok := CanonicalData(v.Str)
	if ok && canon != v.Str {
		*notes = append(*notes, fmt.Sprintf("data %s -> %s", v.Str, canon))
		v.Str = canon
	}
	return v
}

var quantityBlockFields = map[string]bool{
	"number": true, "difficulty": true, "gasLimit": true, "gasUsed": true,
	"timestamp": true, "size": true, "totalDifficulty": true,
	"baseFeePerGas": true, "blobGasUsed": true, "excessBlobGas": true,
	"nonce": false, // nonce is DATA in blocks
}

var dataBlockFields = map[string]bool{
	"hash": true, "parentHash": true, "sha3Uncles": true, "miner": true,
	"stateRoot": true, "transactionsRoot": true, "receiptsRoot": true,
	"logsBloom": true, "extraData": true, "mixHash": true, "nonce": true,
	"parentBeaconBlockRoot": true, "transactions": false,
}

func normalizeBlock(v compare.Value, notes *[]string) compare.Value {
	if v.Kind == compare.KindNull {
		return v
	}
	if v.Kind != compare.KindObject {
		return v
	}
	out := v
	out.Obj = copyObj(v.Obj)
	for k, child := range out.Obj {
		switch {
		case quantityBlockFields[k]:
			out.Obj[k] = normalizeQuantityValue(child, notes)
		case k == "uncles" && child.Kind == compare.KindArray:
			out.Obj[k] = mapArray(child, func(item compare.Value) compare.Value {
				return normalizeDataValue(item, notes)
			})
		case k == "transactions" && child.Kind == compare.KindArray:
			out.Obj[k] = mapArray(child, func(item compare.Value) compare.Value {
				if item.Kind == compare.KindString {
					return normalizeDataValue(item, notes)
				}
				return item
			})
		case dataBlockFields[k]:
			out.Obj[k] = normalizeDataValue(child, notes)
		}
	}
	return out
}

func normalizeReceipt(v compare.Value, notes *[]string) compare.Value {
	if v.Kind == compare.KindNull {
		return v
	}
	if v.Kind != compare.KindObject {
		return v
	}
	qty := map[string]bool{
		"transactionIndex": true, "blockNumber": true, "gasUsed": true,
		"cumulativeGasUsed": true, "status": true, "type": true,
		"effectiveGasPrice": true, "blobGasUsed": true, "logIndex": true,
	}
	data := map[string]bool{
		"transactionHash": true, "blockHash": true, "from": true, "to": true,
		"contractAddress": true, "logsBloom": true, "root": true,
	}
	out := v
	out.Obj = copyObj(v.Obj)
	for k, child := range out.Obj {
		switch {
		case qty[k]:
			out.Obj[k] = normalizeQuantityValue(child, notes)
		case data[k]:
			out.Obj[k] = normalizeDataValue(child, notes)
		case k == "logs" && child.Kind == compare.KindArray:
			out.Obj[k] = mapArray(child, func(item compare.Value) compare.Value {
				return normalizeLog(item, notes)
			})
		}
	}
	return out
}

func normalizeLog(v compare.Value, notes *[]string) compare.Value {
	if v.Kind != compare.KindObject {
		return v
	}
	out := v
	out.Obj = copyObj(v.Obj)
	for k, child := range out.Obj {
		switch k {
		case "address", "blockHash", "transactionHash", "data":
			out.Obj[k] = normalizeDataValue(child, notes)
		case "blockNumber", "logIndex", "transactionIndex":
			out.Obj[k] = normalizeQuantityValue(child, notes)
		case "topics":
			if child.Kind == compare.KindArray {
				out.Obj[k] = mapArray(child, func(item compare.Value) compare.Value {
					return normalizeDataValue(item, notes)
				})
			}
		}
	}
	return out
}

func CanonicalQuantity(s string) (string, bool) {
	if len(s) <= 2 || !isHexString(s) {
		return s, false
	}
	hexdigits := s[2:]
	hexdigits = strings.TrimLeft(strings.ToLower(hexdigits), "0")
	if hexdigits == "" {
		hexdigits = "0"
	}
	return "0x" + hexdigits, true
}

func CanonicalData(s string) (string, bool) {
	if !isHexString(s) {
		return s, false
	}
	return "0x" + strings.ToLower(s[2:]), true
}

func isHexString(s string) bool {
	if len(s) < 2 || !(s[0] == '0' && (s[1] == 'x' || s[1] == 'X')) {
		return false
	}
	for i := 2; i < len(s); i++ {
		c := s[i]
		ok := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !ok {
			return false
		}
	}
	return true
}

func copyObj(in map[string]compare.Value) map[string]compare.Value {
	out := make(map[string]compare.Value, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mapArray(v compare.Value, fn func(compare.Value) compare.Value) compare.Value {
	arr := make([]compare.Value, len(v.Arr))
	for i, item := range v.Arr {
		arr[i] = fn(item)
	}
	v.Arr = arr
	return v
}

func encode(v compare.Value) (json.RawMessage, error) {
	b, err := json.Marshal(toAny(v))
	return b, err
}

func toAny(v compare.Value) any {
	switch v.Kind {
	case compare.KindNull:
		return nil
	case compare.KindBool:
		return v.Bool
	case compare.KindNumber:
		return v.Num
	case compare.KindString:
		return v.Str
	case compare.KindArray:
		out := make([]any, len(v.Arr))
		for i, item := range v.Arr {
			out[i] = toAny(item)
		}
		return out
	case compare.KindObject:
		out := make(map[string]any, len(v.Obj))
		for k, item := range v.Obj {
			out[k] = toAny(item)
		}
		return out
	default:
		return nil
	}
}

func rewriteResult(full, oldResult, newResult json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(full)) == 0 {
		return newResult
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(full, &envelope); err != nil {
		return full
	}
	envelope["result"] = newResult
	b, err := json.Marshal(envelope)
	if err != nil {
		return full
	}
	_ = oldResult
	return b
}
