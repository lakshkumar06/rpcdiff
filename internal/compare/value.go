package compare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Kind int

const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

func (k Kind) String() string {
	switch k {
	case KindNull:
		return "null"
	case KindBool:
		return "bool"
	case KindNumber:
		return "number"
	case KindString:
		return "string"
	case KindArray:
		return "array"
	case KindObject:
		return "object"
	default:
		return "unknown"
	}
}

// Value is a JSON value that preserves object key presence, including null.
type Value struct {
	Kind Kind
	Bool bool
	Num  json.Number
	Str  string
	Arr  []Value
	Obj  map[string]Value
}

func Parse(raw json.RawMessage) (Value, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return Value{}, fmt.Errorf("empty json")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return Value{}, err
	}
	return fromAny(v)
}

func fromAny(v any) (Value, error) {
	switch x := v.(type) {
	case nil:
		return Value{Kind: KindNull}, nil
	case bool:
		return Value{Kind: KindBool, Bool: x}, nil
	case json.Number:
		return Value{Kind: KindNumber, Num: x}, nil
	case string:
		return Value{Kind: KindString, Str: x}, nil
	case []any:
		arr := make([]Value, len(x))
		for i, item := range x {
			child, err := fromAny(item)
			if err != nil {
				return Value{}, err
			}
			arr[i] = child
		}
		return Value{Kind: KindArray, Arr: arr}, nil
	case map[string]any:
		obj := make(map[string]Value, len(x))
		for k, item := range x {
			child, err := fromAny(item)
			if err != nil {
				return Value{}, err
			}
			obj[k] = child
		}
		return Value{Kind: KindObject, Obj: obj}, nil
	default:
		return Value{}, fmt.Errorf("unsupported json type %T", v)
	}
}

func (v Value) String() string {
	switch v.Kind {
	case KindNull:
		return "null"
	case KindBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case KindNumber:
		return string(v.Num)
	case KindString:
		return strconv.Quote(v.Str)
	case KindArray:
		return fmt.Sprintf("array(len=%d)", len(v.Arr))
	case KindObject:
		keys := make([]string, 0, len(v.Obj))
		for k := range v.Obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return fmt.Sprintf("object(keys=%s)", strings.Join(keys, ","))
	default:
		return "unknown"
	}
}

func (v Value) Compact() string {
	s := v.String()
	if len(s) > 120 {
		return s[:117] + "..."
	}
	return s
}
