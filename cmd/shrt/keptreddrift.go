package main

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

const pinnedReferenceScan = 50

func pinnedReference(e *env, c *chain.Chain, rec *runner.Record) *runner.Record {
	if c == nil || rec == nil || len(c.KeptRed) == 0 || rec.DryRun {
		return nil
	}
	ids, _ := e.store.ListRuns(rec.Chain)
	scanned := 0
	for i := len(ids) - 1; i >= 0 && scanned < pinnedReferenceScan; i-- {
		if ids[i] == rec.RunID {
			continue
		}
		scanned++
		prev, err := loadRunNamedAs(e, rec, ids[i])
		if err != nil || prev.DryRun || prev.ReplayOf != "" || !ranBefore(prev, rec) {
			continue
		}
		if prev.KeptRed != runner.KeptRedAsPinned || prev.ChainDigest != rec.ChainDigest || !config.SameTarget(prev.Target, rec.Target) {
			continue
		}
		return prev
	}
	return nil
}

func pinnedStepDrift(e *env, c *chain.Chain, ref, rec *runner.Record) []string {
	pinned := map[string]bool{}
	for _, k := range c.KeptRed {
		pinned[k.Step] = true
	}
	rep := diff.CompareRunsSkipping(ref, rec, currentVolatile(e, rec.Chain), requestFixtures(c))
	rep.DropUnsentDefaults(ref, rec, unsentDefault(e))
	out := []string{}
	for _, ch := range rep.Changes {
		if pinned[ch.Step] {
			out = append(out, fmt.Sprintf("%s %s %s %s", ch.Step, ch.Kind, ch.Path, ch.DescribeRuns()))
		}
	}
	for _, id := range rep.NoLongerReached {
		if pinned[id] {
			out = append(out, id+" was answered in run "+ref.RunID+" and not in this run")
		}
	}
	return out
}

func judgePinnedDrift(e *env, c *chain.Chain, rec *runner.Record, ref *runner.Record) {
	if rec.KeptRed != runner.KeptRedAsPinned {
		return
	}
	if ref == nil {
		rec.KeptRedNote += "; no earlier run of this chain file failed as pinned against this target, so what the pinned steps " +
			"return beyond the pinned paths was not compared: this run is the reference for the next one"
		return
	}
	drift := pinnedStepDrift(e, c, ref, rec)
	if len(drift) == 0 {
		rec.KeptRedNote += "; the pinned steps return what they returned in run " + ref.RunID + ", the last run that failed as pinned"
		return
	}
	shown := drift
	if len(shown) > 5 {
		shown = append(append([]string{}, drift[:5]...), fmt.Sprintf("and %d more", len(drift)-5))
	}
	rec.KeptRed = runner.KeptRedNotAsPinned
	rec.KeptRedNote = fmt.Sprintf("kept_red pins %s, and every pinned expectation failed as pinned, but the pinned steps return something "+
		"else than in run %s, the last run that failed as pinned (a = that run, b = this one): %s. A new defect may hide behind the pinned one: "+
		"compare with shrt diff %s %s %s. If the change is intended, re-pin: shrt chain slice <source chain> -step <pinned step> -kept-red -write %s -force, "+
		"or edit the chain file, and the next run that fails as pinned becomes the reference",
		pinPathsOf(c.KeptRed), ref.RunID, strings.Join(shown, "; "), rec.Chain, ref.RunID, rec.RunID, rec.Chain)
	rec.KeptRedNew = runner.NewFailurePrefix + "a pinned step returns something else than in run " + ref.RunID + ": " + strings.Join(shown, "; ")
}

func pinPathsOf(pins []chain.Pin) string {
	parts := make([]string, 0, len(pins))
	for _, k := range pins {
		s := k.Step + " " + k.Path
		if k.Got != nil {
			s += " got=" + *k.Got
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}
