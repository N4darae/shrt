package main

import (
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func loadRunNamedAs(e *env, rec *runner.Record, id string) (*runner.Record, error) {
	prev, err := e.store.LoadRun(rec.Chain, id)
	if err != nil {
		return nil, err
	}
	return namedAs(prev, rec), nil
}

func namedAs(prev, rec *runner.Record) *runner.Record {
	renames := diff.StepRenames(prev.Steps, rec.Steps)
	if len(renames) == 0 {
		return prev
	}
	cp := *prev
	cp.Steps = diff.RenameSteps(prev.Steps, renames)
	return &cp
}

func previousRun(e *env, rec *runner.Record, named bool, keep func(*runner.Record) bool) *runner.Record {
	ids, _ := e.store.ListRuns(rec.Chain)
	var best *runner.Record
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] == rec.RunID {
			continue
		}
		if best != nil && runStamp(ids[i]) < runStamp(best.RunID) {
			break
		}
		prev, err := e.store.LoadRun(rec.Chain, ids[i])
		if err == nil && named {
			prev = namedAs(prev, rec)
		}
		if err != nil || prev.DryRun || !ranBefore(prev, rec) || keep != nil && !keep(prev) {
			continue
		}
		if best == nil || ranBefore(best, prev) {
			best = prev
		}
	}
	return best
}
