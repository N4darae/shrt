package chain

import (
	"strconv"
	"strings"

	"github.com/N4darae/shrt/namecase"
)

func SplitPath(path string) []string {
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "[", ".")
	path = strings.ReplaceAll(path, "]", "")
	parts := strings.Split(path, ".")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func Get(root any, path string) (any, bool) {
	return get(root, path, false)
}

func GetSynthetic(root any, path string) (any, bool) {
	return get(root, path, true)
}

func get(root any, path string, synthetic bool) (any, bool) {
	cur := root
	for _, seg := range SplitPath(path) {
		next, ok := step(cur, seg)
		if !ok && synthetic {
			next, ok = syntheticStep(cur, seg)
		}
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func syntheticStep(cur any, seg string) (any, bool) {
	v, isList := cur.([]any)
	if !isList || len(v) == 0 {
		return nil, false
	}
	if i, err := strconv.Atoi(seg); err != nil || i < 0 {
		return nil, false
	}
	return v[len(v)-1], true
}

func step(cur any, seg string) (any, bool) {
	switch v := cur.(type) {
	case map[string]any:
		key, ok := namecase.LookupKey(v, seg)
		if !ok {
			return nil, false
		}
		return v[key], true
	case []any:
		i, err := strconv.Atoi(seg)
		if err != nil || i < 0 || i >= len(v) {
			return nil, false
		}
		return v[i], true
	default:
		return nil, false
	}
}
