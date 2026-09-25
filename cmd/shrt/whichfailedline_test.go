package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestWhichNamesTheFailingExpectationOnTheObservedLine(t *testing.T) {
	line := whichSeenCell(&chain.WhichEvidence{
		Run: "r1", Status: runner.StatusFailed, Code: "SUCCESS", Path: "status.code", Asserted: "status.code",
		Failures: []chain.ExpectResult{{Path: "customer.name", Rule: "equals", Want: "idem i4", Got: "changed"}},
	})
	if !strings.Contains(line, "got SUCCESS") || !strings.Contains(line, "FAILED") || !strings.Contains(line, "customer.name") {
		t.Fatalf("a step that got the asserted code yet FAILED must say on that line which expectation failed: %q", line)
	}
	if passed := whichSeenCell(&chain.WhichEvidence{Run: "r1", Status: runner.StatusPassed, Code: "SUCCESS", Path: "status.code", Asserted: "status.code"}); strings.Contains(passed, "on ") {
		t.Fatalf("a passing step names no failure: %q", passed)
	}
}
