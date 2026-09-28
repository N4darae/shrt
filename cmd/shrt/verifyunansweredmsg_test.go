package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestCouldNotVerifySaysWhetherTheStepsAfterTheUnansweredOneWereCompared(t *testing.T) {
	refused := &runner.Record{Steps: []*runner.StepRecord{
		{ID: "p", Status: runner.StatusError, HTTPStatus: 401, AuthRetry: runner.AuthRetryResent},
		{ID: "cust", Status: runner.StatusPassed, HTTPStatus: 200},
		{ID: "cust2", Status: runner.StatusPassed, HTTPStatus: 200},
	}}
	err := couldNotVerifyAfter("dropz", "p", "the backend refused authentication", refused, nil)
	if exitCodeOf(err) != 3 {
		t.Fatalf("could not verify exits 3, got %d %v", exitCodeOf(err), err)
	}
	msg := err.Error()
	if strings.Contains(msg, "nothing past it was compared") {
		t.Fatalf("two later steps were answered and compared; the message must not say nothing was: %s", msg)
	}
	if !strings.Contains(msg, "2 step(s) after it") || !strings.Contains(msg, "not judged") {
		t.Fatalf("the message must say the later steps were compared but not judged: %s", msg)
	}
	unreachable := &runner.Record{Steps: []*runner.StepRecord{
		{ID: "p", Status: runner.StatusError},
		{ID: "cust", Status: runner.StatusSkipped},
	}}
	msg = couldNotVerifyAfter("dropz", "p", "connection refused", unreachable, nil).Error()
	if !strings.Contains(msg, "nothing after it got an answer") {
		t.Fatalf("no later step was answered, so the message must say so: %s", msg)
	}
}
