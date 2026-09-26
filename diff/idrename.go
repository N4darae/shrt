package diff

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/pathmask"
)

type idPair struct {
	step string
	path string
	want any
	got  any
}

func collectIDPairs(step string, want, got any, path string, mask *pathmask.Masker, out *[]idPair) {
	if path != "" && mask.Masks(path) {
		return
	}
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return
		}
		for _, k := range sortedKeys(w, g) {
			wv, wok := w[k]
			gv, gok := g[k]
			if wok && gok {
				collectIDPairs(step, wv, gv, pathmask.Join(path, k), mask, out)
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			return
		}
		for i := range min(len(w), len(g)) {
			collectIDPairs(step, w[i], g[i], pathmask.Join(path, pathmask.IndexKey(i)), mask, out)
		}
	default:
		if renameable(path, want, got) {
			*out = append(*out, idPair{step: step, path: path, want: want, got: got})
		}
	}
}

func renameable(path string, want, got any) bool {
	if !idValue(want) || !idValue(got) || jsonKind(want) != jsonKind(got) {
		return false
	}
	if idNamed(lastKey(path)) {
		return sameScalar(want, got) || sameShape(want, got)
	}
	return bothAre(want, got, uuidShape.MatchString)
}

func idValue(v any) bool {
	switch t := v.(type) {
	case string:
		return t != "" && t != pathmask.MaskRedacted && t != pathmask.MaskVolatile && !isTimestamp(t)
	case float64:
		return t != 0
	}
	return false
}

func idNamed(key string) bool {
	lower := strings.ToLower(key)
	switch {
	case lower == "id", lower == "ids", lower == "idempotency_key",
		strings.HasSuffix(lower, "_id"), strings.HasSuffix(lower, "_ids"), strings.HasPrefix(lower, "id_"),
		camelSuffix(key, "Id"), camelSuffix(key, "Ids"), camelIDPrefix(key):
		return true
	}
	return false
}

func IDNamedPath(path string) bool {
	return idNamed(lastKey(path))
}

func idKey(v any) string {
	return jsonKind(v) + ":" + fmt.Sprint(v)
}

func renamingViolations(pairs []idPair) []Change {
	forward := map[string]idPair{}
	reverse := map[string]idPair{}
	out := []Change{}
	for _, p := range pairs {
		w, g := idKey(p.want), idKey(p.got)
		if first, ok := forward[w]; ok && idKey(first.got) != g {
			out = append(out, Change{Step: p.step, Path: p.path, Kind: KindChanged, Want: first.got, Got: p.got,
				Detail: fmt.Sprintf("an id, but not renamed consistently: the safe spot's %v became %v at %s %s and %v here, "+
					"so this field now points at something else than it did", p.want, first.got, first.step, first.path, p.got)})
			continue
		}
		if first, ok := reverse[g]; ok && idKey(first.want) != w {
			out = append(out, Change{Step: p.step, Path: p.path, Kind: KindChanged, Want: p.want, Got: p.got,
				Detail: fmt.Sprintf("an id, but not renamed consistently: %v stands for the safe spot's %v at %s %s and for %v here, "+
					"so two different ids of the safe spot became one", p.got, first.want, first.step, first.path, p.want)})
			continue
		}
		if _, ok := forward[w]; !ok {
			forward[w] = p
		}
		if _, ok := reverse[g]; !ok {
			reverse[g] = p
		}
	}
	return out
}

func zeroID(s string) bool {
	rest := s
	if prefix := kindPrefix(s); prefix != "" {
		rest = s[len(prefix)+1:]
	}
	runs := alnumRun.FindAllString(rest, -1)
	if len(runs) == 0 {
		return false
	}
	for _, r := range runs {
		if strings.Trim(r, "0") != "" {
			return false
		}
	}
	return true
}
