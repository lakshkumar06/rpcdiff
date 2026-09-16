package compare

import (
	"encoding/json"
	"fmt"
	"sort"
)

type DiffKind string

const (
	DiffValue        DiffKind = "value"
	DiffShape        DiffKind = "shape"
	DiffMissingNull  DiffKind = "missing_vs_null"
	DiffType         DiffKind = "type"
	DiffArrayLength  DiffKind = "array_length"
	DiffMissingField DiffKind = "missing_field"
	DiffExtraField   DiffKind = "extra_field"
)

// Diff is one deterministic difference at a JSON path.
type Diff struct {
	Path    string   `json:"path"`
	Kind    DiffKind `json:"kind"`
	Left    string   `json:"baseline"`
	Right   string   `json:"candidate"`
	Message string   `json:"message"`
}

func Equal(left, right Value) []Diff {
	var diffs []Diff
	walk("", left, right, true, true, &diffs)
	return diffs
}

func walk(path string, left, right Value, leftPresent, rightPresent bool, diffs *[]Diff) {
	if !leftPresent && !rightPresent {
		return
	}
	if leftPresent && !rightPresent {
		*diffs = append(*diffs, Diff{
			Path:    path,
			Kind:    DiffMissingField,
			Left:    left.Compact(),
			Right:   "<missing>",
			Message: "field present on baseline, missing on candidate",
		})
		return
	}
	if !leftPresent && rightPresent {
		*diffs = append(*diffs, Diff{
			Path:    path,
			Kind:    DiffExtraField,
			Left:    "<missing>",
			Right:   right.Compact(),
			Message: "field missing on baseline, present on candidate",
		})
		return
	}
	if left.Kind != right.Kind {
		if left.Kind == KindNull || right.Kind == KindNull {
			*diffs = append(*diffs, Diff{
				Path:    path,
				Kind:    DiffValue,
				Left:    left.Compact(),
				Right:   right.Compact(),
				Message: fmt.Sprintf("null versus %s", otherKind(left, right)),
			})
			return
		}
		*diffs = append(*diffs, Diff{
			Path:    path,
			Kind:    DiffType,
			Left:    left.Kind.String() + " " + left.Compact(),
			Right:   right.Kind.String() + " " + right.Compact(),
			Message: fmt.Sprintf("type mismatch: %s vs %s", left.Kind, right.Kind),
		})
		return
	}
	switch left.Kind {
	case KindNull:
		return
	case KindBool:
		if left.Bool != right.Bool {
			*diffs = append(*diffs, valueDiff(path, left, right, "boolean values differ"))
		}
	case KindNumber:
		if !numbersEqual(left.Num, right.Num) {
			*diffs = append(*diffs, valueDiff(path, left, right, "numeric values differ"))
		}
	case KindString:
		if left.Str != right.Str {
			*diffs = append(*diffs, valueDiff(path, left, right, "string values differ"))
		}
	case KindArray:
		if len(left.Arr) != len(right.Arr) {
			*diffs = append(*diffs, Diff{
				Path:    path,
				Kind:    DiffArrayLength,
				Left:    left.Compact(),
				Right:   right.Compact(),
				Message: fmt.Sprintf("array length %d vs %d", len(left.Arr), len(right.Arr)),
			})
		}
		n := min(len(left.Arr), len(right.Arr))
		for i := 0; i < n; i++ {
			walk(indexPath(path, i), left.Arr[i], right.Arr[i], true, true, diffs)
		}
	case KindObject:
		keys := map[string]struct{}{}
		for k := range left.Obj {
			keys[k] = struct{}{}
		}
		for k := range right.Obj {
			keys[k] = struct{}{}
		}
		ordered := make([]string, 0, len(keys))
		for k := range keys {
			ordered = append(ordered, k)
		}
		sort.Strings(ordered)
		for _, k := range ordered {
			lv, lOK := left.Obj[k]
			rv, rOK := right.Obj[k]
			child := fieldPath(path, k)
			switch {
			case lOK && rOK:
				walk(child, lv, rv, true, true, diffs)
			case lOK && !rOK:
				if lv.Kind == KindNull {
					*diffs = append(*diffs, Diff{
						Path:    child,
						Kind:    DiffMissingNull,
						Left:    "null",
						Right:   "<missing>",
						Message: "baseline field is null; candidate field is missing",
					})
					continue
				}
				walk(child, lv, Value{}, true, false, diffs)
			case !lOK && rOK:
				if rv.Kind == KindNull {
					*diffs = append(*diffs, Diff{
						Path:    child,
						Kind:    DiffMissingNull,
						Left:    "<missing>",
						Right:   "null",
						Message: "baseline field is missing; candidate field is null",
					})
					continue
				}
				walk(child, Value{}, rv, false, true, diffs)
			}
		}
	}
}

func valueDiff(path string, left, right Value, msg string) Diff {
	return Diff{
		Path:    path,
		Kind:    DiffValue,
		Left:    left.Compact(),
		Right:   right.Compact(),
		Message: msg,
	}
}

func otherKind(left, right Value) string {
	if left.Kind == KindNull {
		return right.Kind.String()
	}
	return left.Kind.String()
}

func numbersEqual(a, b json.Number) bool {
	return string(a) == string(b)
}

func fieldPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func indexPath(parent string, i int) string {
	if parent == "" {
		return fmt.Sprintf("[%d]", i)
	}
	return fmt.Sprintf("%s[%d]", parent, i)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
