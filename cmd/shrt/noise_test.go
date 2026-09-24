package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

const cachedRestart = "the first attempt carried a token read from the on-disk cache that no call in this run had used yet, and the backend refused it at authentication (a restart or a revoke)"

func TestTheSummaryPrintsAWarningSharedByStepsOnce(t *testing.T) {
	rec := &runner.Record{Chain: "access", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{ID: "admin_create", Status: runner.StatusPassed, Warning: cachedRestart},
		{ID: "clerk_create", Status: runner.StatusPassed, Warning: cachedRestart},
	}}
	out := summary(rec, false)
	if n := strings.Count(out, cachedRestart); n != 1 {
		t.Fatalf("one restart is one line, printed %d times:\n%s", n, out)
	}
	if !strings.Contains(out, "warning [admin_create, clerk_create]: ") {
		t.Fatalf("the line names every step that carried it:\n%s", out)
	}
}

func TestProgressPrintsARepeatedNotSentReasonOnce(t *testing.T) {
	why := `not sent: ${create.id} reads step "create", which was refused in-band (status.code = REJECTED): a refused call's response decodes to zero values, and the request would carry them as if they were real. A reference to its request (${steps.create.request...}) is still safe: that is what was sent, so a step reading only that is sent`
	later := strings.Replace(why, "${create.id}", "${create.name}", 1)
	c := runner.NewSkipCondenser()
	if got := c.Condense("fetch", why); got != why {
		t.Fatalf("the first time the reason is printed whole, got %q", got)
	}
	got := c.Condense("list", later)
	if strings.Contains(got, "zero values") || !strings.Contains(got, "${create.name}") || !strings.Contains(got, "fetch") {
		t.Fatalf("a repeat keeps its own reference and refers to the step that gave the reason, got %q", got)
	}
}
