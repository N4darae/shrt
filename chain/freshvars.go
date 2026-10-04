package chain

import (
	"regexp"
)

var (
	interpolatedVarRef = regexp.MustCompile(`\$\{vars\.([A-Za-z0-9_]+)\}`)
	freshPerRun        = regexp.MustCompile(`\$\{\s*(uuid|now|nowunix)\b`)
)

func interpolatingStrings(steps []*Step) map[string]bool {
	out := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			if freshPerRun.MatchString(t) {
				return
			}
			for _, m := range interpolatedVarRef.FindAllStringIndex(t, -1) {
				if m[0] != 0 || m[1] != len(t) {
					out[t] = true
					return
				}
			}
		case map[string]any:
			for _, item := range t {
				walk(item)
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	for _, st := range steps {
		if st != nil {
			walk(st.Body)
		}
	}
	return out
}

func FreshVars(steps []*Step, isLogin func(*Step) bool, existing []*Step) []string {
	writes := []*Step{}
	for _, st := range steps {
		if st == nil || !isWriteCall(st.Call) {
			continue
		}
		if isLogin != nil && isLogin(st) {
			continue
		}
		writes = append(writes, st)
	}
	named := interpolatingStrings(existing)
	seen := map[string]bool{}
	for text := range interpolatingStrings(writes) {
		if named[text] {
			continue
		}
		for _, m := range interpolatedVarRef.FindAllStringSubmatch(text, -1) {
			seen[m[1]] = true
		}
	}
	return sortedKeys(seen)
}
