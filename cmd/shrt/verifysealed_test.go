package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/store"
)

func resealSafeSpot(t *testing.T, path string) {
	t.Helper()
	spot := &store.SafeSpot{}
	if err := json.Unmarshal(mustRead(t, path), spot); err != nil {
		t.Fatal(err)
	}
	spot.Digest = spot.ComputeDigest()
	raw, err := json.MarshalIndent(spot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(raw))
}

func TestCLIVerifyRefusesAHandEditedSafeSpot(t *testing.T) {
	approvedThingFlow(t)
	path := ".shrt/safespots/cli-thing-flow.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), `"name": "widget"`, `"name": "gadget"`, 1)
	if edited == string(raw) {
		t.Fatal("fixture edit did not apply")
	}
	writeFile(t, path, edited)
	var verr error
	captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet", "-save=false"})
	})
	if verr == nil || !strings.Contains(verr.Error(), "does not match") || !strings.Contains(verr.Error(), "-supersede") {
		t.Fatalf("a safe spot edited after approval must be refused with how to re-approve, got %v", verr)
	}
}

func TestCLIVerifyRefusesAHandEditedApprover(t *testing.T) {
	approvedThingFlow(t)
	path := ".shrt/safespots/cli-thing-flow.json"
	raw := string(mustRead(t, path))
	edited := strings.Replace(raw, `"confirmed_by": "alice@example.test"`, `"confirmed_by": "mallory@example.test"`, 1)
	if edited == raw {
		t.Fatal("fixture edit did not apply")
	}
	writeFile(t, path, edited)
	var verr error
	captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet", "-save=false"})
	})
	if verr == nil || !strings.Contains(verr.Error(), "does not match") {
		t.Fatalf("a safe spot whose approver was edited after approval must be refused, got %v", verr)
	}
}

func TestCLIVerifySaysWhenASafeSpotPredatesTheApprovalDigest(t *testing.T) {
	approvedThingFlow(t)
	path := ".shrt/safespots/cli-thing-flow.json"
	spot := &store.SafeSpot{}
	if err := json.Unmarshal(mustRead(t, path), spot); err != nil {
		t.Fatal(err)
	}
	spot.Digest = spot.ContentDigest()
	raw, err := json.MarshalIndent(spot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(raw))
	var verr error
	out := captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow", "-save=false"})
	})
	if verr != nil {
		t.Fatalf("an older safe spot must still verify, got %v:\n%s", verr, out)
	}
	if !strings.Contains(out, "older kind") || !strings.Contains(out, "confirmed_by") {
		t.Fatalf("verify must say the safe spot's approval is not covered by its digest:\n%s", out)
	}
}
