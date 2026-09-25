package chain

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type Within struct {
	Of any `yaml:"of" json:"of"`
	By any `yaml:"by" json:"by"`
}

func (e Expectation) Operands() []any {
	out := []any{e.Equals, e.NotEqual, e.Contains, e.Includes, e.Gt, e.Gte, e.Lt, e.Lte}
	out = append(out, e.Between...)
	if e.Within != nil {
		out = append(out, e.Within.Of, e.Within.By)
	}
	return out
}

func (e Expectation) HasComparison() bool {
	return e.Gt != nil || e.Gte != nil || e.Lt != nil || e.Lte != nil || e.Between != nil || e.Within != nil
}

func (e Expectation) MapOperands(f func(any) any) Expectation {
	out := e
	out.Equals = f(e.Equals)
	out.NotEqual = f(e.NotEqual)
	out.Includes = f(e.Includes)
	if text, ok := f(e.Contains).(string); ok {
		out.Contains = text
	}
	out.Gt, out.Gte, out.Lt, out.Lte = f(e.Gt), f(e.Gte), f(e.Lt), f(e.Lte)
	if e.Between != nil {
		out.Between = make([]any, len(e.Between))
		for i, v := range e.Between {
			out.Between[i] = f(v)
		}
	}
	if e.Within != nil {
		out.Within = &Within{Of: f(e.Within.Of), By: f(e.Within.By)}
	}
	return out
}

func (e Expectation) resolveComparisons(scope *Scope) (Expectation, error) {
	var err error
	resolve := func(name string, v any) any {
		if v == nil || err != nil {
			return v
		}
		r, rerr := scope.ResolveValue(v)
		if rerr != nil {
			err = fmt.Errorf("expect on %q: %s: %w", e.Path, name, rerr)
			return v
		}
		return r
	}
	out := e
	out.Gt, out.Gte, out.Lt, out.Lte = resolve("gt", e.Gt), resolve("gte", e.Gte), resolve("lt", e.Lt), resolve("lte", e.Lte)
	if e.Between != nil {
		out.Between = make([]any, len(e.Between))
		for i, v := range e.Between {
			out.Between[i] = resolve("between", v)
		}
	}
	if e.Within != nil {
		out.Within = &Within{Of: resolve("within.of", e.Within.Of), By: resolve("within.by", e.Within.By)}
	}
	if err != nil {
		return e, err
	}
	return out, nil
}

func numberOf(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		s := strings.TrimSpace(t)
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return n, true
		}
		if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return float64(ts.UnixNano()) / 1e9, true
		}
	}
	return 0, false
}

func numberText(n float64) string {
	if n == math.Trunc(n) && math.Abs(n) < 1e15 {
		return strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

func OperandText(v any) (string, bool) {
	n, ok := numberOf(v)
	if !ok {
		return "", false
	}
	return numberText(n), true
}

func (e Expectation) evaluateComparison(got any, found bool, presence any) (ExpectResult, bool) {
	type bound struct {
		rule string
		want any
		ok   func(g, w float64) bool
	}
	var one *bound
	switch {
	case e.Gt != nil:
		one = &bound{"gt", e.Gt, func(g, w float64) bool { return g > w }}
	case e.Gte != nil:
		one = &bound{"gte", e.Gte, func(g, w float64) bool { return g >= w }}
	case e.Lt != nil:
		one = &bound{"lt", e.Lt, func(g, w float64) bool { return g < w }}
	case e.Lte != nil:
		one = &bound{"lte", e.Lte, func(g, w float64) bool { return g <= w }}
	}
	rule, want := "", any(nil)
	switch {
	case one != nil:
		rule, want = one.rule, one.want
	case e.Between != nil:
		rule, want = "between", e.Between
	case e.Within != nil:
		rule, want = "within", map[string]any{"of": e.Within.Of, "by": e.Within.By}
	default:
		return ExpectResult{}, false
	}
	if !found {
		return result(e.Path, rule, want, nil, false, "path not present in response"), true
	}
	g, ok := numberOf(got)
	if _, sent := Get(presence, e.Path); !ok && !sent && isEmpty(got) {
		return result(e.Path, rule, want, got, false, "path not present in response"), true
	}
	if !ok && got == nil {
		return result(e.Path, rule, want, got, false, "the value is null, not a number or an RFC3339 time, so it cannot be compared"), true
	}
	if !ok {
		return result(e.Path, rule, want, got, false, "the value is not a number or an RFC3339 time, so it cannot be compared"), true
	}
	operand := func(v any, what string) (float64, string) {
		n, ok := numberOf(v)
		if !ok {
			return 0, fmt.Sprintf("%s %v is not a number or an RFC3339 time", what, v)
		}
		return n, ""
	}
	switch {
	case one != nil:
		w, bad := operand(one.want, rule)
		if bad != "" {
			return result(e.Path, rule, want, got, false, bad), true
		}
		return result(e.Path, rule, numberText(w), got, one.ok(g, w), ""), true
	case e.Between != nil:
		if len(e.Between) != 2 {
			return result(e.Path, rule, want, got, false, "between takes exactly two bounds, [low, high]"), true
		}
		lo, bad := operand(e.Between[0], "the low bound")
		if bad == "" {
			var hi float64
			hi, bad = operand(e.Between[1], "the high bound")
			if bad == "" {
				return result(e.Path, rule, "["+numberText(lo)+", "+numberText(hi)+"]", got, g >= lo && g <= hi, ""), true
			}
		}
		return result(e.Path, rule, want, got, false, bad), true
	default:
		of, bad := operand(e.Within.Of, "of")
		if bad == "" {
			var by float64
			by, bad = operand(e.Within.By, "by")
			if bad == "" {
				text := numberText(of) + " ± " + numberText(by)
				return result(e.Path, rule, text, got, math.Abs(g-of) <= by, ""), true
			}
		}
		return result(e.Path, rule, want, got, false, bad), true
	}
}
