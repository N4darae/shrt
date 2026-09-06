package chain

import (
	"fmt"
	"strings"
)

type Expectation struct {
	Path     string `yaml:"path" json:"path"`
	Equals   any    `yaml:"equals,omitempty" json:"equals,omitempty"`
	NotEqual any    `yaml:"not_equal,omitempty" json:"not_equal,omitempty"`
	Contains string `yaml:"contains,omitempty" json:"contains,omitempty"`
	Exists   *bool  `yaml:"exists,omitempty" json:"exists,omitempty"`
	NotEmpty bool   `yaml:"not_empty,omitempty" json:"not_empty,omitempty"`
}

type ExpectResult struct {
	Path   string `json:"path"`
	Rule   string `json:"rule"`
	Want   any    `json:"want,omitempty"`
	Got    any    `json:"got,omitempty"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// ResolveWith resolves ${...} inside the COMPARISON VALUES (equals / not_equal / contains) against
// scope, leaving Path literal. Path is a JSON path into this step's own response, so a reference
// there would name a location rather than a value and there is no sensible meaning for it.
//
// This is what lets a chain state a money invariant. Before it, expect.go offered five rules that
// each compared one path against one LITERAL, so no chain could say "after == before", "side A ==
// side B", or "give + fees == get + margin" — every conservation check in the repo was a
// hand-computed number encoding its author's belief, and 48.7% of steps asserted only the error
// envelope because that was the only thing an assertion could reach.
func (e Expectation) ResolveWith(scope *Scope) (Expectation, error) {
	out := e
	for _, f := range []struct {
		name string
		get  func() any
		set  func(any)
	}{
		{"equals", func() any { return e.Equals }, func(v any) { out.Equals = v }},
		{"not_equal", func() any { return e.NotEqual }, func(v any) { out.NotEqual = v }},
		{"contains", func() any {
			if e.Contains == "" {
				return nil
			}
			return e.Contains
		}, func(v any) { out.Contains = stringify(v) }},
	} {
		raw := f.get()
		if raw == nil {
			continue
		}
		resolved, err := scope.ResolveValue(raw)
		if err != nil {
			return e, fmt.Errorf("expect on %q: %s: %w", e.Path, f.name, err)
		}
		f.set(resolved)
	}
	return out, nil
}

func (e Expectation) Evaluate(response any) ExpectResult {
	return e.EvaluateIn(response, response)
}

func (e Expectation) EvaluateIn(response, presence any) ExpectResult {
	got, found := Get(response, e.Path)
	switch {
	case e.Exists != nil:
		_, sent := Get(presence, e.Path)
		return result(e.Path, "exists", *e.Exists, sent, sent == *e.Exists, "")
	case e.NotEmpty:
		ok := found && !isEmpty(got)
		return result(e.Path, "not_empty", true, got, ok, "")
	case e.Contains != "":
		ok := found && strings.Contains(stringify(got), e.Contains)
		return result(e.Path, "contains", e.Contains, got, ok, "")
	case e.NotEqual != nil:
		if !found {
			return result(e.Path, "not_equal", e.NotEqual, nil, false, "path not present in response")
		}
		return result(e.Path, "not_equal", e.NotEqual, got, !equal(got, e.NotEqual), "")
	case e.Equals != nil:
		if !found {
			return result(e.Path, "equals", e.Equals, nil, false, "path not present in response")
		}
		return result(e.Path, "equals", e.Equals, got, equal(got, e.Equals), "")
	default:
		return result(e.Path, "invalid", nil, nil, false, "expectation has no rule")
	}
}

func result(path, rule string, want, got any, passed bool, detail string) ExpectResult {
	return ExpectResult{Path: path, Rule: rule, Want: want, Got: got, Passed: passed, Detail: detail}
}

func equal(got, want any) bool {
	if fmt.Sprintf("%v", got) == fmt.Sprintf("%v", want) {
		return true
	}
	return stringify(got) == stringify(want)
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	case float64:
		return t == 0
	case bool:
		return !t
	default:
		return false
	}
}

func (r ExpectResult) String() string {
	status := "FAIL"
	if r.Passed {
		status = "ok"
	}
	if r.Detail != "" {
		return fmt.Sprintf("%s %s %s want=%v got=%v (%s)", status, r.Path, r.Rule, r.Want, r.Got, r.Detail)
	}
	return fmt.Sprintf("%s %s %s want=%v got=%v", status, r.Path, r.Rule, r.Want, r.Got)
}

func TautologyReason(e Expectation) string {
	switch {
	case e.Exists != nil && *e.Exists && IsEnvelopePath(e.Path):
		return "only asserts that the envelope is present, which every well-formed response carries"
	case (e.NotEmpty || (e.Exists != nil && *e.Exists)) && alwaysPresentTransportPath(e.Path):
		return "asserts that the call has a transport outcome, which every answered call has — success " +
			"or refusal. Assert the outcome's VALUE, e.g. equals: unauthenticated"
	case e.NotEmpty && (e.Path == EnvelopeField() || e.Path == EnvelopePath()):
		return "asserts that the envelope is non-empty, which it always is — every response carries a " +
			"code, success or refusal. Assert the code's VALUE, not its presence"
	}
	return ""
}

func EnumTautologyReason(e Expectation, enumValues []string) string {
	if e.NotEqual == nil || len(enumValues) == 0 {
		return ""
	}
	want := stringify(e.NotEqual)
	for _, v := range enumValues {
		if v == want {
			return ""
		}
	}
	return fmt.Sprintf("says the value is not %q, which is not one of the values this field can hold (%s)", want, strings.Join(enumValues, ", "))
}
