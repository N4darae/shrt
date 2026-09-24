package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestRunSummaryCapsTheDidNotPassList(t *testing.T) {
	failed := []string{}
	for i := range 313 {
		failed = append(failed, "step_"+strconv.Itoa(i))
	}
	rec := &runner.Record{Chain: "big", Status: runner.StatusError, KeepGoing: true, FailedSteps: failed}
	out := summary(rec, false)
	if strings.Contains(out, "step_312") {
		t.Fatalf("313 step ids on one line bury the reason below it:\n%s", out)
	}
	if !strings.Contains(out, "did not pass: step_0, step_1") || !strings.Contains(out, "and 303 more") {
		t.Fatalf("want the first few ids and how many more:\n%s", out)
	}
}
