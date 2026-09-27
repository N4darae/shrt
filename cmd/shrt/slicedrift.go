package main

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func sliceReference(e *env, rec *runner.Record) *runner.Record {
	id := rec.ReplayOf
	if id == "" {
		if spot, err := e.store.LoadSafeSpot(rec.Chain); err == nil {
			id = spot.RunID
		}
	}
	if id == "" || id == rec.RunID {
		return nil
	}
	ref, err := e.store.LoadRun(rec.Chain, id)
	if err != nil {
		return nil
	}
	return ref
}

func targetChanges(e *env, res *chain.SliceResult, ref, rec *runner.Record) map[string]string {
	rep := diff.CompareRunsSkipping(ref, rec, currentVolatile(e, ref.Chain), requestFixtures(res.Chain))
	rep.DropUnsentDefaults(ref, rec, unsentDefault(e))
	out := map[string]string{}
	for _, ch := range rep.Changes {
		if ch.Step == res.Target {
			out[ch.Path+" ("+ch.Kind+")"] = ch.DescribeRuns()
		}
	}
	return out
}

func sliceDrift(e *env, res *chain.SliceResult, rec, sliceRec *runner.Record) (missing []string, matched string) {
	ref := sliceReference(e, rec)
	if ref == nil {
		return nil, ""
	}
	want := targetChanges(e, res, ref, rec)
	if len(want) == 0 {
		return nil, ""
	}
	got := targetChanges(e, res, ref, sliceRec)
	keys := sortedKeys(want)
	for _, k := range keys {
		if _, ok := got[k]; !ok {
			missing = append(missing, fmt.Sprintf("source run %s changed %s against run %s, %s; the slice run did not", rec.RunID, k, ref.RunID, want[k]))
		}
	}
	if len(missing) > 0 {
		return missing, ""
	}
	return nil, fmt.Sprintf("the slice changed what source run %s changed against run %s: %s", rec.RunID, ref.RunID, strings.Join(keys, ", "))
}
