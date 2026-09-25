package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func saveFreshRefusedAtCreate(t *testing.T, e *env, base *runner.Record, id string) {
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
	create := rec.Steps[0]
	create.AuthRetry = runner.AuthRetryNotResent
	create.Status = runner.StatusError
	create.HTTPStatus = 401
	create.Transport = &runner.TransportError{Code: "unauthenticated", Message: "token rejected"}
	create.Response = json.RawMessage(`{"code":"unauthenticated","message":"token rejected"}`)
	create.Error = "unauthenticated: token rejected\n       the backend refused a token that a login in this run had just issued: " +
		"the credentials work and the token is current, so this may be an auth regression in the backend"
	fetch := rec.Steps[1]
	fetch.Status = runner.StatusSkipped
	fetch.HTTPStatus = 0
	fetch.Response = nil
	fetch.Expect = nil
	if _, err := e.store.SaveRun(&rec); err != nil {
		t.Fatal(err)
	}
}

func TestAWriteRefusedWithAJustIssuedTokenAndNotResentStaysCouldNotVerifyOnARepeat(t *testing.T) {
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
	saveFreshRefusedAtCreate(t, e, base, "20990101T000000Z-fresh1")
	saveFreshRefusedAtCreate(t, e, base, "20990101T000001Z-fresh2")
	var coded *exitError
	out := captureStdout(t, func() {
		err = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-run", "20990101T000000Z-fresh1"})
	})
	if !errors.As(err, &coded) || coded.code != 3 || !strings.Contains(err.Error(), "may be an auth regression") {
		t.Fatalf("a first refusal of a freshly issued token is could-not-verify, exit 3: %v\n%s", err, out)
	}
	out = captureStdout(t, func() {
		err = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-run", "20990101T000001Z-fresh2"})
	})
	if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out, "FINDING") {
		t.Fatalf("the write was never re-sent with a fresh token, so a restart between the login and the call explains it in both runs: exit 3, no finding: %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "not re-sent") {
		t.Fatalf("say why a repeat is no finding here: %v", err)
	}
}
