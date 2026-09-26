package chain

import (
	"fmt"
	"sort"
	"strings"
)

func AuthBodyScope() *Scope {
	return NewScope(nil)
}

func ResolveAuthBody(body map[string]any) (any, error) {
	scope := AuthBodyScope()
	seen := map[string]bool{}
	unset := []string{}
	for _, ref := range collectRefs(body) {
		r := ParseRef(ref)
		if r.Kind != RefEnv || seen[r.Rest] {
			continue
		}
		seen[r.Rest] = true
		if _, ok := scope.env(r.Rest); !ok {
			unset = append(unset, r.Rest)
		}
	}
	if len(unset) > 1 {
		sort.Strings(unset)
		refs := make([]string, 0, len(unset))
		for _, name := range unset {
			refs = append(refs, "${env."+name+"}")
		}
		return nil, fmt.Errorf("unresolved references %s: env %s are not set", strings.Join(refs, ", "), strings.Join(unset, ", "))
	}
	return scope.ResolveValue(body)
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

func AuthBodyEnvNames(body map[string]any) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, ref := range collectRefs(body) {
		if r := ParseRef(ref); r.Kind == RefEnv && r.Rest != "" && !seen[r.Rest] {
			seen[r.Rest] = true
			out = append(out, r.Rest)
		}
	}
	sort.Strings(out)
	return out
}

func VarRefs(v any) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, ref := range collectRefs(v) {
		if r := ParseRef(ref); r.Kind == RefVars && r.Err == nil && r.Rest != "" && !seen[r.Expr] {
			seen[r.Expr] = true
			out = append(out, "${"+r.Expr+"}")
		}
	}
	sort.Strings(out)
	return out
}
