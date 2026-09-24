package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestTheRunSummaryCarriesEveryStepWarning(t *testing.T) {
	rec := &runner.Record{
		Chain:  "green-with-warnings",
		Status: runner.StatusPassed,
		Steps: []*runner.StepRecord{
			{Index: 1, ID: "login", Status: runner.StatusPassed},
			{Index: 2, ID: "probe", Status: runner.StatusPassed,
				Warning: "refused in-band (status.code = REJECTED, not SUCCESS)\n       and a second line"},
		},
	}
	out := summary(rec, false)
	if !strings.Contains(out, "probe") || !strings.Contains(out, "refused in-band") || !strings.Contains(out, "a second line") {
		t.Fatalf("-quiet prints no progress lines, so a green run's step warnings must reach the summary, got:\n%s", out)
	}
	if strings.Contains(out, "login") {
		t.Fatalf("a step without a warning is not listed:\n%s", out)
	}
}
