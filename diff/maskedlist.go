package diff

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/pathmask"
)

const (
	shapeMask   = "the id or timestamp shape of both values"
	renamedMask = "an id renamed consistently across the run"
	fixtureMask = "an echo of the fixture name the run sent"
)

func maskOf(m *pathmask.Masker, c Change) string {
	if p := hidingPattern(m, c.Path, c.Kind != KindMissing && c.Kind != KindUnexpected); p != "" {
		return p
	}
	return hidingPattern(m, c.Path, true)
}

func maskSuffix(c Change) string {
	if c.Mask == "" {
		return ""
	}
	return " hidden by " + c.Mask
}

func withMask(cs []Change, mask string) []Change {
	out := make([]Change, 0, len(cs))
	for _, c := range cs {
		c.Mask = mask
		out = append(out, c)
	}
	return out
}

func (r *RunReport) varChanges(fixture bool) []string {
	out := []string{}
	for _, v := range r.VarChanges {
		if v.Fixture == fixture {
			out = append(out, fmt.Sprintf("%s a=%v b=%v", v.Name, orAbsent(v.A), orAbsent(v.B)))
		}
	}
	return out
}

func (r *RunReport) MaskedList() string {
	var b strings.Builder
	if vars := r.varChanges(true); len(vars) > 0 {
		fmt.Fprintf(&b, "fixture vars differ, echoes masked: %s\n", strings.Join(vars, "; "))
	}
	if r.FixtureRequests > 0 {
		fmt.Fprintf(&b, "%d request value(s) differ only in a fixture name, a ${uuid} or the clock\n", r.FixtureRequests)
	}
	if len(r.UnsentDefaults)+len(r.UndeclaredSame) > 0 {
		fmt.Fprintf(&b, "response fields declared in one record only, unsent or sent undeclared alike: %s\n",
			strings.Join(append(append([]string{}, r.UnsentDefaults...), r.UndeclaredSame...), ", "))
	}
	fmt.Fprintf(&b, "%d masked difference(s), run A -> run B:\n", len(r.MaskedChanges))
	for _, c := range r.MaskedChanges {
		fmt.Fprintf(&b, "  [%s] %s (%s) hidden by %s\n", c.Step, c.Path, c.Transition(), c.Mask)
	}
	return strings.TrimRight(b.String(), "\n")
}
