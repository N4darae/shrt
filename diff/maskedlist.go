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

func (r *RunReport) MaskedList() string {
	if len(r.MaskedChanges) == 0 {
		return "masked differences: none"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d masked difference(s), not counted as differences, run A -> run B:\n", len(r.MaskedChanges))
	for _, c := range r.MaskedChanges {
		fmt.Fprintf(&b, "  [%s] %s (%s) hidden by %s\n", c.Step, c.Path, c.Transition(), c.Mask)
	}
	return strings.TrimRight(b.String(), "\n")
}
