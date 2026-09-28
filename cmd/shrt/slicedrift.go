package main

import (
	"fmt"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func sliceReference(e *env, rec *runner.Record) (*runner.Record, bool) {
	id := rec.ReplayOf
	if id == "" {
		if spot, err := e.store.LoadSafeSpot(rec.Chain); err == nil {
			id = spot.RunID
		}
	}
	if id == "" || id == rec.RunID {
		return nil, true
	}
	ref, err := e.store.LoadRun(rec.Chain, id)
	return ref, err == nil
}

func targetChanges(e *env, c *chain.Chain, step string, ref, rec *runner.Record) map[string]diff.Change {
	rep := diff.CompareRunsSkipping(ref, rec, currentVolatile(e, ref.Chain), requestFixtures(c))
	rep.DropUnsentDefaults(ref, rec, unsentDefault(e))
	out := map[string]diff.Change{}
	for _, ch := range rep.Changes {
		if ch.Step == step {
			out[ch.Path+" ("+ch.Kind+")"] = ch
		}
	}
	return out
}

func sliceDrift(e *env, res *chain.SliceResult, rec, sliceRec *runner.Record, passed bool) (missing, drifted []string, compared bool) {
	ref, compared := sliceReference(e, rec)
	if ref == nil {
		return nil, nil, compared
	}
	want := targetChanges(e, res.Chain, res.Target, ref, rec)
	got := targetChanges(e, res.Chain, res.Target, ref, sliceRec)
	for _, k := range sortedKeys(want) {
		if g, ok := got[k]; !ok {
			missing = append(missing, fmt.Sprintf("source run %s changed %s against run %s, %s; the slice run did not", rec.RunID, k, ref.RunID, want[k].DescribeRuns()))
		} else if passed {
			drifted = append(drifted, fmt.Sprintf("drifted: %s source got=%s, slice got=%s", g.Path, quoted(want[k].Got), quoted(g.Got)))
		}
	}
	for _, k := range sortedKeys(got) {
		if _, ok := want[k]; !ok && passed {
			missing = append(missing, fmt.Sprintf("the slice run changed %s against run %s, %s; source run %s did not", k, ref.RunID, got[k].DescribeRuns(), rec.RunID))
		}
	}
	return missing, drifted, compared
}

func movedStatus(e *env, rec *runner.Record, step string) string {
	s := stepStatus(rec, step)
	if s != runner.StatusPassed {
		return s
	}
	c, err := chain.Resolve(e.chainsDir(), rec.Chain)
	if ref, _ := sliceReference(e, rec); ref != nil && err == nil && len(targetChanges(e, c, step, ref, rec)) > 0 {
		return "drifted"
	}
	return s
}
