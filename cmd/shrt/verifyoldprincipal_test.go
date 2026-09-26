package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCLIVerifyDoesNotCallDriftARegressionWhenTheSafeSpotHasNoPrincipal(t *testing.T) {
	approvedThingFlow(t)
	e, err := loadEnv(true)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := e.store.LatestRun("cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	path := ".shrt/safespots/cli-thing-flow.json"
	spot := map[string]any{}
	if err := json.Unmarshal(mustRead(t, path), &spot); err != nil {
		t.Fatal(err)
	}
	for _, s := range spot["steps"].([]any) {
		step := s.(map[string]any)
		step["auth_profile"] = "default"
		delete(step, "auth_principal")
	}
	raw, err := json.MarshalIndent(spot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(raw))
	resealSafeSpot(t, path)
	rec.RunID = "20990101T000000Z-0badc0de"
	for _, s := range rec.Steps {
		s.AuthProfile, s.AuthPrincipal = "default", "cccc3333dddd4444"
	}
	fetch := rec.Steps[len(rec.Steps)-1]
	fetch.Response = json.RawMessage(strings.Replace(string(fetch.Response), `"widget"`, `"gadget"`, 1))
	if _, err := e.store.SaveRun(rec); err != nil {
		t.Fatal(err)
	}
	var verr error
	out := captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow", "-run", rec.RunID})
	})
	if !strings.Contains(out, "principal checking is off") || !strings.Contains(out, "-supersede") {
		t.Fatalf("verify must say principal checking is off for this safe spot and how to turn it on:\n%s", out)
	}
	if verr == nil || strings.HasPrefix(verr.Error(), "regression") {
		t.Fatalf("the principal cannot be compared, so the drift is not called a regression, got %v\n%s", verr, out)
	}
}
