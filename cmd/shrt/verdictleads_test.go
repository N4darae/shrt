package main

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestTheHeadlineLeadsWithTheStepsFlippedVerdict(t *testing.T) {
	env := chain.EnvelopePath()
	report := &diff.Report{Changes: []diff.Change{
		{Step: "get", Path: "status", Kind: diff.KindStatus, Want: runner.StatusPassed, Got: runner.StatusFailed},
		{Step: "get", Path: "customer", Kind: diff.KindType, Want: nil, Got: map[string]any{}},
		{Step: "get", Path: env, Kind: diff.KindChanged, Want: "REJECTED", Got: "SUCCESS"},
		{Step: "list", Path: "total", Kind: diff.KindChanged, Want: 1, Got: 2},
	}}
	first, steps := firstChange(report)
	if first == nil || first.Path != env || steps != 2 {
		t.Fatalf("the envelope verdict flip leads, got %+v over %d step(s)", first, steps)
	}
	report.Changes = append([]diff.Change{{Step: "get", Path: "status", Kind: diff.KindStatus, Want: runner.StatusPassed, Got: runner.StatusError}}, report.Changes[1:]...)
	if first, _ = firstChange(report); first.Kind != diff.KindStatus {
		t.Fatalf("a transport error leads, got %+v", first)
	}
}
