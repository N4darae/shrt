package main

import (
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestVerifyCannotJudgeTheStepsAfterADroppedConnection(t *testing.T) {
	rec := &runner.Record{Steps: []*runner.StepRecord{
		{ID: "seed", Status: runner.StatusPassed, HTTPStatus: 200},
		{ID: "create", Status: runner.StatusError,
			Error: "the backend closed the connection before a response arrived (EOF)"},
		{ID: "list", Status: runner.StatusPassed, HTTPStatus: 200},
	}}
	after := &diff.Report{Changes: []diff.Change{
		{Step: "create", Path: "status", Kind: diff.KindStatus},
		{Step: "list", Path: "orders", Kind: diff.KindLength},
	}}
	step, _, ok := unansweredOnly(rec, after)
	if !ok || step != "create" {
		t.Fatalf("a step that never got an answer, and the changes after it, are no verdict: step=%q ok=%v", step, ok)
	}
	before := &diff.Report{Changes: append([]diff.Change{{Step: "seed", Path: "total", Kind: diff.KindChanged}}, after.Changes...)}
	if _, _, ok := unansweredOnly(rec, before); ok {
		t.Fatal("a change before the unanswered step is evidence and must still be judged")
	}
}
