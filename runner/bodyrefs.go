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
	var visit func(v any, path string)
	visit = func(v any, path string) {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				visit(x, pathmask.Join(path, k))
			}
		case []any:
			for i, x := range t {
				visit(x, pathmask.Join(path, pathmask.IndexKey(i)))
			}
		case string:
			if ReadsAnotherStep(t) {
				out[path] = t
			}
		}
	}
	visit(body, "")
	if len(out) == 0 {
		return nil
	}
	return out
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
