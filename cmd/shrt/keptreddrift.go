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
		if prev.KeptRed != runner.KeptRedAsPinned || len(prev.KeptRedSlow) > 0 || prev.ChainDigest != rec.ChainDigest || !config.SameTarget(prev.Target, rec.Target) {
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
	rec.KeptRedNote = fmt.Sprintf("kept_red pins %s and every pin failed as pinned, but the pinned steps now return something else "+
		"than in run %s, the last run that failed as pinned (a = that run, b = this one):\n%s\n"+
		"compare: shrt diff %s %s %s; if intended, re-pin: shrt chain slice <source chain> -step <pinned step> -kept-red -write %s -force",
		runner.PinCount(len(c.KeptRed)), ref.RunID, strings.Join(shown, "\n"), rec.Chain, ref.RunID, rec.RunID, rec.Chain)
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

func keptRedLatencyFailure(name string, flags []diff.LatencyFlag, p diff.LatencyPolicy) error {
	slow := confirmedLatency(flags)
	if !p.Fail || len(slow) == 0 {
		return nil
	}
	steps := make([]string, len(slow))
	for i, f := range slow {
		steps[i] = fmt.Sprintf("%s %dms -> %dms", f.Step, f.BeforeMS, f.AfterMS)
	}
	return fmt.Errorf("latency regression in %s: it failed as pinned, but %d step(s) took at least +%dms and %gx the last run "+
		"that failed as pinned, confirmed by re-measurement or the previous run (%s); latency.fail is set in .shrt/config.yaml, "+
		"so this fails the run of a kept-red chain, which has no safe spot for verify to compare with",
		name, len(slow), p.FloorMS, p.Ratio, strings.Join(steps, ", "))
}
