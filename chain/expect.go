package chain

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type Expectation struct {
	Path     string  `yaml:"path" json:"path"`
	Equals   any     `yaml:"equals,omitempty" json:"equals,omitempty"`
	Includes any     `yaml:"includes,omitempty" json:"includes,omitempty"`
	NotEqual any     `yaml:"not_equal,omitempty" json:"not_equal,omitempty"`
	Contains string  `yaml:"contains,omitempty" json:"contains,omitempty"`
	Exists   *bool   `yaml:"exists,omitempty" json:"exists,omitempty"`
	NotEmpty bool    `yaml:"not_empty,omitempty" json:"not_empty,omitempty"`
	Gt       any     `yaml:"gt,omitempty" json:"gt,omitempty"`
	Gte      any     `yaml:"gte,omitempty" json:"gte,omitempty"`
	Lt       any     `yaml:"lt,omitempty" json:"lt,omitempty"`
	Lte      any     `yaml:"lte,omitempty" json:"lte,omitempty"`
	Between  []any   `yaml:"between,omitempty" json:"between,omitempty"`
	Within   *Within `yaml:"within,omitempty" json:"within,omitempty"`

	vacuous string
}

func (e Expectation) vacuousWhy() string {
	switch e.vacuous {
	case `contains: ""`:
		return `contains: "", which every value contains, so it can never fail; write the text the value must contain`
	case "not_empty: false":
		return "not_empty: false, which asks for nothing (only not_empty: true is a check), so it can never fail; " +
			"write not_empty: true, or exists: false for a field that must be absent"
	}
	return ""
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
	var err error
	out := e.mapNamed(func(name string, v any) any {
		if v == nil || err != nil {
			return v
		}
		r, rerr := scope.ResolveValue(v)
		if rerr != nil {
			err = fmt.Errorf("expect on %q: %s: %w", e.Path, name, rerr)
			return v
		}
		return r
	})
	if err != nil {
		return e, err
	}
	return out, nil
}

func (e Expectation) Evaluate(response any) ExpectResult {
	return e.EvaluateIn(response, response)
}

func (e Expectation) EvaluateIn(response, presence any) ExpectResult {
	return e.EvaluateTyped(response, presence, "")
}

func (e Expectation) EvaluateTyped(response, presence any, kind string) ExpectResult {
	got, found := Get(response, e.Path)
	switch {
	case e.Exists != nil:
		_, sent := Get(presence, e.Path)
		return result(e.Path, "exists", *e.Exists, sent, sent == *e.Exists, "")
	case e.NotEmpty:
		ok := found && !IsZeroOf(kind, got)
		return result(e.Path, "not_empty", true, got, ok, "")
	case e.Contains != "":
		ok := found && strings.Contains(stringify(got), e.Contains)
		return result(e.Path, "contains", e.Contains, got, ok, "")
	case e.NotEqual != nil:
		if !found {
			return result(e.Path, "not_equal", e.NotEqual, nil, false, "path not present in response")
		}
		return result(e.Path, "not_equal", e.NotEqual, got, !equalOf(kind, got, e.NotEqual), "")
	case e.Equals != nil:
		if !found {
			return result(e.Path, "equals", e.Equals, nil, false, "path not present in response")
		}
		return result(e.Path, "equals", e.Equals, got, equalOf(kind, got, e.Equals), "")
	case e.Includes != nil:
		return evaluateIncludes(e.Path, e.Includes, got, found)
	default:
		if r, ok := e.evaluateComparison(got, found, presence); ok {
			return r
		}
		if why := e.vacuousWhy(); why != "" {
			return result(e.Path, "invalid", nil, nil, false, "not evaluated, the expectation is "+why)
		}
		return result(e.Path, "invalid", nil, nil, false, "expectation has no rule")
	}
}

func evaluateIncludes(path string, want, got any, found bool) ExpectResult {
	if !found {
		return result(path, "includes", want, nil, false, "path not present in response")
	}
	list, ok := got.([]any)
	if !ok {
		return result(path, "includes", want, got, false, "the value is not a list")
	}
	for _, item := range list {
		if itemMatches(item, want) {
			return result(path, "includes", want, len(list), true, "")
		}
	}
	return result(path, "includes", want, len(list), false, fmt.Sprintf("none of the %d item(s) matches", len(list)))
}

func itemMatches(item, want any) bool {
	fields, isMap := want.(map[string]any)
	if !isMap {
		return equal(item, want)
	}
	for k, v := range fields {
		got, ok := Get(item, k)
		if !ok || !equal(got, v) {
			return false
		}
	}
	return true
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

func equalOf(kind string, got, want any) bool {
	if text, ok := want.(string); ok && text == "" && IsNumericKind(kind) {
		return IsZeroOf(kind, got)
	}
	return equal(got, want)
}

func IsNumericKind(kind string) bool {
	switch kind {
	case "int32", "int64", "uint32", "uint64", "sint32", "sint64", "fixed32", "fixed64", "sfixed32", "sfixed64",
		"float", "double":
		return true
	}
	return false
}

func IsZeroOf(kind string, v any) bool {
	if text, ok := v.(string); ok && IsNumericKind(kind) {
		if text == "" {
			return true
		}
		n, err := strconv.ParseFloat(text, 64)
		return err == nil && n == 0
	}
	return isEmpty(v)
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
	out := fmt.Sprintf("%s %s %s", status, r.Path, WantGot(r.Rule, fmt.Sprint(r.Want), fmt.Sprint(r.Got)))
	if !ruleShown(r.Rule) {
		out = fmt.Sprintf("%s %s %s want=%v", status, r.Path, r.Rule, r.Want)
		if !unchecked(r) {
			out += fmt.Sprintf(" got=%v", r.Got)
		}
	}
	return withNote(out, r.Detail)
}

func unchecked(r ExpectResult) bool {
	return r.Rule == "unevaluated" && (r.Got == nil || scalarText(r.Got) == scalarText(r.Want))
}

func ruleShown(rule string) bool {
	switch rule {
	case "", "equals", "item_envelope", "not_equal", "gt", "gte", "lt", "lte", "contains", "includes", "between", "within", "not_empty", "exists":
		return true
	}
	return false
}

var wantSigns = map[string]string{"not_equal": "≠", "gt": ">", "gte": "≥", "lt": "<", "lte": "≤", "between": " in ", "": "=", "equals": "=", "item_envelope": "="}

func WantText(rule, want string) string {
	switch {
	case rule == "not_empty":
		return "want non-empty"
	case rule == "exists" && want == "false":
		return "want absent"
	case rule == "exists":
		return "want present"
	}
	if sign, ok := wantSigns[rule]; ok {
		return "want" + sign + want
	}
	return "want " + rule + " " + want
}

func GotText(rule, got string) string {
	if rule == "exists" && (got == "true" || got == "false") {
		if got == "true" {
			return "got present"
		}
		return "got absent"
	}
	if rule == "includes" {
		return "got " + got + " item(s)"
	}
	return "got=" + got
}

func WantGot(rule, want, got string) string {
	return WantText(rule, want) + " " + GotText(rule, got)
}

func TautologyReason(e Expectation) string {
	switch {
	case e.Exists != nil && *e.Exists && IsEnvelopePath(e.Path):
		return "only asserts that the envelope is present, which every well-formed response carries"
	case (e.NotEmpty || (e.Exists != nil && *e.Exists)) && alwaysPresentTransportPath(e.Path):
		return "asserts that the call has a transport outcome, which every answered call has — success " +
			"or refusal"
	case e.NotEmpty && (e.Path == EnvelopeField() || e.Path == EnvelopePath()):
		return "asserts that the envelope is non-empty, which it always is — every response carries a " +
			"code, success or refusal"
	}
	return ""
}

func TautologyRemedy(e Expectation) string {
	switch {
	case alwaysPresentTransportPath(e.Path):
		return fmt.Sprintf("Assert the outcome's VALUE instead: %s.code equals: %s for a call that must "+
			"succeed, or the refusal it must get, e.g. %s.code equals: unauthenticated", TransportPrefix, TransportOK, TransportPrefix)
	case IsEnvelopePath(e.Path) || e.Path == EnvelopePath():
		return fmt.Sprintf("Assert the code's VALUE instead: %s equals: %s for a call that must succeed, or "+
			"the refusal code it must get", EnvelopePath(), EnvelopeOK())
	}
	return "Assert the value this step should have produced"
}

func EnumTautologyReason(e Expectation, enumValues []string) string {
	if e.NotEqual == nil || len(enumValues) == 0 || slices.Contains(enumValues, stringify(e.NotEqual)) {
		return ""
	}
	want := stringify(e.NotEqual)
	return fmt.Sprintf("says the value is not %q, which is not one of the values this field can hold (%s)", want, strings.Join(enumValues, ", "))
}
