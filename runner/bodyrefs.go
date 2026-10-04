package runner

import (
	"regexp"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
)

var bodyRef = regexp.MustCompile(`\$\{([^}]+)\}`)

func BodyRefs(body map[string]any) map[string]string {
	out := map[string]string{}
	eachLeaf(body, "", func(v any, path string) {
		if t, ok := v.(string); ok && ReadsAnotherStep(t) {
			out[path] = t
		}
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

func eachLeaf(v any, path string, leaf func(v any, path string)) {
	switch t := v.(type) {
	case map[string]any:
		for k, item := range t {
			eachLeaf(item, pathmask.Join(path, k), leaf)
		}
	case []any:
		for i, item := range t {
			eachLeaf(item, pathmask.Join(path, pathmask.IndexKey(i)), leaf)
		}
	default:
		leaf(v, path)
	}
}

func ReadsAnotherStep(text string) bool {
	for _, m := range bodyRef.FindAllStringSubmatch(text, -1) {
		switch chain.ParseRef(strings.TrimSpace(m[1])).Kind {
		case chain.RefStep, chain.RefBare, chain.RefExports:
			return true
		}
	}
	return false
}
