package diff

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

const ExpectValuePath = "expect_value"

func ExpectValueChanges(spot *store.SafeSpot, rec *runner.Record, c *chain.Chain, fixture func(template string) bool) []Change {
	out := []Change{}
	if c == nil || rec == nil {
		return out
	}
	for _, was := range spot.Steps {
		got, ok := rec.Step(was.ID)
		now, inChain := c.Step(was.ID)
		if !ok || !inChain || now == nil {
			continue
		}
		declared := make([]chain.ExpectResult, len(now.Expect))
		for j, e := range now.Expect {
			declared[j] = e.Evaluate(nil)
		}
		for i, j := range pairExpectations(was.Expect, declared) {
			if j < 0 || j >= len(got.Expect) {
				continue
			}
			w, g := was.Expect[i], got.Expect[j]
			template, _ := declared[j].Want.(string)
			if w.Path != g.Path || w.Rule != g.Rule || !inputOnly(template) || (fixture != nil && fixture(template)) {
				continue
			}
			if redactedValue(w.Want) || redactedValue(g.Want) || fmt.Sprint(w.Want) == fmt.Sprint(g.Want) {
				continue
			}
			out = append(out, Change{Step: was.ID, Path: ExpectValuePath, Kind: KindChanged,
				Want: expectText(w.Path, w.Rule, w.Want), Got: g.Want, Detail: template})
		}
	}
	return out
}

func inputOnly(template string) bool {
	refs := refPattern.FindAllStringSubmatch(template, -1)
	if len(refs) == 0 {
		return false
	}
	for _, m := range refs {
		switch chain.ParseRef(m[1]).Kind {
		case chain.RefVars, chain.RefEnv:
		default:
			return false
		}
	}
	return !strings.Contains(template, pathmask.MaskRedacted)
}

func expectationChange(c Change) bool {
	return c.Path == ExpectPath || c.Path == ExpectValuePath
}
