package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestVerifySaysTheBackendLostItsSessionMidRun(t *testing.T) {
	srv := newEchoNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	spot, err := e.store.LoadSafeSpot("cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	base, err := e.store.LoadRun("cli-thing-flow", spot.RunID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(base)
	var rec runner.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	rec.RunID = "20990101T000000Z-restart1"
	rec.Seal = ""
	rec.Status = runner.StatusFailed
	for _, st := range rec.Steps {
		st.AuthProfile = "default"
	}
	fetch := rec.Steps[1]
	fetch.AuthRetry = "resent"
	fetch.Status = runner.StatusFailed
	fetch.Response = json.RawMessage(`{"error":{"code":"NOT_FOUND","message":"no thing"},"id":"","name":"","created_at":"","total":0}`)
	if _, err := e.store.SaveRun(&rec); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-run", rec.RunID}) })
	if err == nil {
		t.Fatal("a run the backend lost its session in is not a clean verify")
	}
	if strings.Contains(err.Error(), "regression") {
		t.Fatalf("nothing drifted before the refused token, so this is not a regression: %v\n%s", err, out)
	}
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatalf("want could not verify, exit 3, got %v", err)
	}
	want := "the backend refused a token it had accepted earlier in this run at step 2"
	if !strings.Contains(out, want) || !strings.Contains(err.Error(), "restarted mid-run") {
		t.Fatalf("verify must say the backend lost its session state mid-run, want %q:\n%s\n%v", want, out, err)
	}
}
