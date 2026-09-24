package chain

import (
	"fmt"
	"sort"
)

func AuthBodyScope() *Scope {
	return NewScope(nil)
}

func AuthBodyReferenceProblems(body map[string]any) []string {
	out := []string{}
	for _, ref := range collectRefs(body) {
		r := ParseRef(ref)
		switch {
		case r.Err != nil:
			out = append(out, fmt.Sprintf("${%s} cannot resolve: %v", ref, r.Err))
		case r.Kind == RefEnv || r.Kind == RefUUID || r.Kind == RefClock:
		default:
			out = append(out, fmt.Sprintf("${%s} cannot resolve in an auth body, which is sent before any "+
				"step runs and sees no vars, exports or steps — only ${env.*}, ${uuid} and the clock forms", ref))
		}
	}
	sort.Strings(out)
	return out
}

func AuthBodyEnvRefs(body map[string]any) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, ref := range collectRefs(body) {
		if r := ParseRef(ref); r.Kind == RefEnv && !seen[ref] {
			seen[ref] = true
			out = append(out, "${"+ref+"}")
		}
	}
	sort.Strings(out)
	return out
}
