package chain

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

func AuthBodyScope() *Scope {
	return NewScope(nil)
}

func ResolveAuthBody(body map[string]any) (any, error) {
	scope := AuthBodyScope()
	unset := slices.DeleteFunc(uniqueRefs(body, func(r Ref) (string, bool) { return r.Rest, r.Kind == RefEnv }), func(name string) bool {
		_, set := scope.env(name)
		return set
	})
	if len(unset) > 1 {
		return nil, fmt.Errorf("unresolved references ${env.%s}: env %s are not set", strings.Join(unset, "}, ${env."), strings.Join(unset, ", "))
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
	return uniqueRefs(body, func(r Ref) (string, bool) { return "${" + r.Expr + "}", r.Kind == RefEnv })
}

func AuthBodyEnvNames(body map[string]any) []string {
	return uniqueRefs(body, func(r Ref) (string, bool) { return r.Rest, r.Kind == RefEnv && r.Rest != "" })
}

func VarRefs(v any) []string {
	return uniqueRefs(v, func(r Ref) (string, bool) {
		return "${" + r.Expr + "}", r.Kind == RefVars && r.Err == nil && r.Rest != ""
	})
}

func uniqueRefs(v any, pick func(Ref) (string, bool)) []string {
	out := []string{}
	for _, ref := range collectRefs(v) {
		if text, ok := pick(ParseRef(ref)); ok {
			out = append(out, text)
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}
