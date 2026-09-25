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
	renames := diff.StepRenames(prev.Steps, rec.Steps)
	if len(renames) == 0 {
		return prev, nil
	}
	cp := *prev
	cp.Steps = diff.RenameSteps(prev.Steps, renames)
	return &cp, nil
}
