package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestVerifyCannotJudgeARunWhoseStepWasRefusedAuthentication(t *testing.T) {
	rec := &runner.Record{Steps: []*runner.StepRecord{
		{ID: "seed", Status: runner.StatusPassed, HTTPStatus: 200},
		{ID: "create", Status: runner.StatusError, HTTPStatus: 401, AuthRetry: runner.AuthRetryNotResent,
			Response: []byte(`{"code":"unauthenticated"}`), Error: "unauthenticated: invalid or expired token\nmore"},
		{ID: "list", Status: runner.StatusPassed, HTTPStatus: 200},
	}}
	after := &diff.Report{Changes: []diff.Change{
		{Step: "create", Path: "status", Kind: diff.KindStatus},
		{Step: "list", Path: "orders", Kind: diff.KindLength},
	}}
	step, why, ok := unansweredOnly(rec, after)
	if !ok || step != "create" || !strings.Contains(why, "refused authentication") {
		t.Fatalf("a step refused at authentication, and the changes it caused later, are no verdict: step=%q why=%q ok=%v", step, why, ok)
	}
	before := &diff.Report{Changes: append([]diff.Change{{Step: "seed", Path: "total", Kind: diff.KindChanged}}, after.Changes...)}
	if _, _, ok := unansweredOnly(rec, before); ok {
		t.Fatal("a change before the refused step is evidence and must still be judged")
	}
}
