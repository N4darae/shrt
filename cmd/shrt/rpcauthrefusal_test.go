package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func saveRefusedAtFetch(t *testing.T, e *env, base *runner.Record, id string) {
	t.Helper()
	raw, _ := json.Marshal(base)
	var rec runner.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	rec.RunID = id
	rec.Seal = ""
	rec.Status = runner.StatusError
	for _, st := range rec.Steps {
		st.AuthProfile = "default"
	}
	fetch := rec.Steps[1]
	fetch.AuthRetry = runner.AuthRetryNotResent
	fetch.Status = runner.StatusError
	fetch.HTTPStatus = 401
	fetch.Transport = &runner.TransportError{Code: "unauthenticated", Message: "token rejected"}
	fetch.Response = json.RawMessage(`{"code":"unauthenticated","message":"token rejected"}`)
	if _, err := e.store.SaveRun(&rec); err != nil {
		t.Fatal(err)
	}
}

func TestARefusalRepeatedAtTheSameRPCIsAFindingNotARestart(t *testing.T) {
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
	saveRefusedAtFetch(t, e, base, "20990101T000000Z-refused1")
	saveRefusedAtFetch(t, e, base, "20990101T000001Z-refused2")
	var coded *exitError
	out := captureStdout(t, func() {
		err = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-run", "20990101T000000Z-refused1"})
	})
	if !errors.As(err, &coded) || coded.code != 3 || !strings.Contains(err.Error(), "restarted mid-run") {
		t.Fatalf("a first refusal with a token accepted earlier reads as a likely restart, exit 3: %v\n%s", err, out)
	}
	out = captureStdout(t, func() {
		err = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-run", "20990101T000001Z-refused2"})
	})
	if err == nil || errors.As(err, &coded) {
		t.Fatalf("a refusal repeated at the same rpc is a finding, exit 1, not could-not-verify: %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "auth refused at") || !strings.Contains(err.Error(), "20990101T000000Z-refused1") || strings.Contains(err.Error(), "restarted mid-run") {
		t.Fatalf("the finding must name the rpc and the earlier run refused the same way: %v", err)
	}
}
