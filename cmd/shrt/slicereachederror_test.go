package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestAStepSentAndRefusedAsErrorIsReached(t *testing.T) {
	rec := &runner.Record{RunID: "r1", Status: runner.StatusError, Steps: []*runner.StepRecord{
		{ID: "write_3", Status: runner.StatusError, HTTPStatus: 401, Request: json.RawMessage(`{"email":"w3@example.test"}`),
			Response:  json.RawMessage(`{"code":"unauthenticated","message":"invalid or expired token"}`),
			Transport: &runner.TransportError{Code: "unauthenticated", Message: "invalid or expired token"}},
		{ID: "write_4", Status: runner.StatusSkipped},
		{ID: "unresolved", Status: runner.StatusError, Error: "unresolved reference ${x.y}"},
	}}
	if ok, why := reachedStep(rec, "write_3"); !ok {
		t.Fatalf("write_3 was sent and the backend answered it, so the run reached it: %s", why)
	}
	if ok, _ := reachedStep(rec, "write_4"); ok {
		t.Fatal("a skipped step was never sent")
	}
	if ok, _ := reachedStep(rec, "unresolved"); ok {
		t.Fatal("a step that errored before anything was sent was not reached")
	}
}

func TestASliceOfAStepRefusedForItsTokensAgeIsVerifiedAndSaysWhyItDiffers(t *testing.T) {
	b := &shortSessionBackend{uses: 1, short: true}
	shortSessionWorkspace(t, b, lifetimeWrites)
	ctx := context.Background()
	captureStdout(t, func() { _ = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(ctx, []string{"cli-thing-flow", "-step", "create_again", "-run", "latest", "-verify"})
	})
	if strings.Contains(out+fmt.Sprint(err), "reached step") || strings.Contains(out+fmt.Sprint(err), "did not reach") {
		t.Fatalf("create_again was sent and refused, so the run reached it: %v\n%s", err, out)
	}
	if exitCodeOf(err) != 1 || !strings.Contains(out, "NOT REPRODUCED") {
		t.Fatalf("the slice sends create_again with a fresh token, so the verdict differs: %v\n%s", err, out)
	}
	if !strings.Contains(out, "the slice's younger token was accepted") {
		t.Fatalf("the difference is the token's age, not a dropped write:\n%s", out)
	}
}
