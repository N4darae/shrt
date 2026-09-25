package diff

import (
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func (r *RunReport) DropUnsentDefaults(a, b *runner.Record, unsent func(procedure, path string, v any) bool) {
	if unsent == nil || a == nil || b == nil {
		return
	}
	forward := &Report{Changes: r.Changes}
	forward.DropUnsentDefaults(&store.SafeSpot{Chain: a.Chain, RunID: a.RunID, Steps: a.Steps}, b, unsent)
	flipped := make([]Change, 0, len(forward.Changes))
	for _, c := range forward.Changes {
		flipped = append(flipped, flipSides(c))
	}
	backward := &Report{Changes: flipped}
	backward.DropUnsentDefaults(&store.SafeSpot{Chain: b.Chain, RunID: b.RunID, Steps: b.Steps}, a, unsent)
	r.Changes = r.Changes[:0]
	for _, c := range backward.Changes {
		r.Changes = append(r.Changes, flipSides(c))
	}
	r.UnsentDefaults = append(r.UnsentDefaults, forward.UnsentDefaults...)
	r.UnsentDefaults = append(r.UnsentDefaults, backward.UnsentDefaults...)
	r.UndeclaredSame = append(r.UndeclaredSame, forward.UndeclaredSame...)
	r.UndeclaredSame = append(r.UndeclaredSame, backward.UndeclaredSame...)
	r.UndeclaredUnknown = append(r.UndeclaredUnknown, forward.UndeclaredUnknown...)
	r.UndeclaredUnknown = append(r.UndeclaredUnknown, backward.UndeclaredUnknown...)
}

func flipSides(c Change) Change {
	switch c.Kind {
	case KindUnexpected:
		c.Kind = KindMissing
	case KindMissing:
		c.Kind = KindUnexpected
	}
	c.Want, c.Got = c.Got, c.Want
	return c
}
