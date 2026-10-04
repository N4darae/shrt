package chain

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const clockOffsetTolerance = 2

var firstNumberPattern = regexp.MustCompile(`-?[0-9]+(\.[0-9]+)?`)

func ClockRelativeExpectation(e Expectation) bool {
	for _, op := range e.Operands() {
		if text, ok := op.(string); ok && strings.Contains(text, "${now") {
			return true
		}
	}
	return false
}

func ClockRelative(step *Step, v Verdict) Verdict {
	if step == nil {
		return v
	}
	out := v
	out.Expect = append([]ExpectResult(nil), v.Expect...)
	for i := range min(len(out.Expect), len(step.Expect)) {
		e := step.Expect[i]
		r := out.Expect[i]
		if e.Path != r.Path || !ClockRelativeExpectation(e) {
			continue
		}
		anchor, anchored := boundAnchor(r.Want)
		r.Want = templateText(e)
		if g, ok := numberOf(r.Got); ok && anchored {
			r.Got = clockOffset(g - anchor)
		}
		out.Expect[i] = r
	}
	return out
}

func SameClockOffset(a, b any) bool {
	x, okA := parseClockOffset(a)
	y, okB := parseClockOffset(b)
	return okA && okB && math.Abs(x-y) <= clockOffsetTolerance
}

func boundAnchor(want any) (float64, bool) {
	switch t := want.(type) {
	case map[string]any:
		return numberOf(t["of"])
	case []any:
		if len(t) > 0 {
			return numberOf(t[0])
		}
		return 0, false
	}
	if n, ok := numberOf(want); ok {
		return n, true
	}
	text, _ := want.(string)
	m := firstNumberPattern.FindString(text)
	if m == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(m, 64)
	return n, err == nil
}

func templateText(e Expectation) string {
	parts := []string{}
	for _, op := range e.Operands() {
		if op != nil && fmt.Sprint(op) != "" {
			parts = append(parts, fmt.Sprint(op))
		}
	}
	return strings.Join(parts, ", ")
}

func clockOffset(d float64) string {
	return fmt.Sprintf("bound%+gs", math.Round(d*1000)/1000)
}

func parseClockOffset(v any) (float64, bool) {
	text, ok := v.(string)
	if !ok || !strings.HasPrefix(text, "bound") || !strings.HasSuffix(text, "s") {
		return 0, false
	}
	n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimPrefix(text, "bound"), "s"), 64)
	return n, err == nil
}
