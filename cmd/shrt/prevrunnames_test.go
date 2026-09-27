package main

import (
	"testing"
	"time"

	"github.com/N4darae/shrt/runner"
)

func TestTheTokenLifetimeNamesAnEarlierRunsStepAsThatRunRecordedIt(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	read := func(id string, i int) *runner.StepRecord {
		return &runner.StepRecord{Index: i, ID: id, Call: "ThingService/Fetch", Status: runner.StatusPassed}
	}
	start := time.Date(2026, 9, 27, 17, 0, 0, 0, time.UTC)
	was := &runner.Record{Chain: "cli-thing-flow", RunID: "20260927T170000Z-aaaaaaaa", StartedAt: start, Status: runner.StatusPassed,
		Steps: []*runner.StepRecord{read("read_10s", 1), read("read_25s", 2)}}
	if _, err := e.store.SaveRun(was); err != nil {
		t.Fatal(err)
	}
	now := &runner.Record{Chain: "cli-thing-flow", RunID: "20260927T171000Z-bbbbbbbb", StartedAt: start.Add(10 * time.Minute),
		Steps: []*runner.StepRecord{read("read_0", 1), read("read_1", 2), read("read_2", 3)}}
	prev := previousRecord(e, now)
	if prev == nil || prev.Steps[1].ID != "read_25s" {
		t.Fatalf("the earlier run's step keeps its recorded name, got %+v", prev)
	}
}
