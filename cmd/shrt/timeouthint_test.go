package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestCouldNotVerifyNamesTargetTimeoutWhenTheCallTimedOut(t *testing.T) {
	rec := &runner.Record{Steps: []*runner.StepRecord{{ID: "create", Status: runner.StatusError}}}
	why := "POST http://127.0.0.1:1/x: sent, no answer before target.timeout (700ms): context deadline exceeded"
	err := couldNotVerify("thing-flow", "create", why, rec)
	if !strings.Contains(err.Error(), "raise target.timeout") {
		t.Fatalf("a timeout is fixed by the timeout, not by starting the target: %v", err)
	}
	if strings.Contains(err.Error(), "start or reach the target") {
		t.Fatalf("the target answered nothing in time, it was reached: %v", err)
	}
}
