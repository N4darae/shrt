package contract

import (
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
)

type Strictness int

const (
	AuthoredBody Strictness = iota
	ScaffoldedBody
)

func HasUsableValue(body map[string]any, path string, how Strictness) bool {
	v, ok := bodyValue(body, path)
	return ok && !IsPlaceholder(v, how)
}

func IsPlaceholder(v any, how Strictness) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		if t == "" || strings.HasSuffix(t, "_UNSPECIFIED") {
			return true
		}
		return how == ScaffoldedBody && t == "0"
	case float64:
		return how == ScaffoldedBody && t == 0
	case int:
		return how == ScaffoldedBody && t == 0
	case bool:
		return false
	case []any:
		if len(t) == 0 {
			return true
		}
		for _, item := range t {
			if !IsPlaceholder(item, how) {
				return false
			}
		}
		return true
	case map[string]any:
		if len(t) == 0 {
			return true
		}
		for _, item := range t {
			if !IsPlaceholder(item, how) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func bodyValue(body map[string]any, path string) (any, bool) {
	segs := chain.SplitPath(path)
	var cur any = body
	for _, seg := range segs {
		if list, ok := cur.([]any); ok {
			idx, err := strconv.Atoi(seg)
			if err == nil {
				if idx < 0 || idx >= len(list) {
					return nil, false
				}
				cur = list[idx]
				continue
			}
			if len(list) == 0 {
				return nil, false
			}
			cur = list[0]
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := m[seg]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}
