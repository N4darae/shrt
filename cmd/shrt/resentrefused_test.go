package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func approvedThingFlowRun(t *testing.T) (context.Context, *env, *runner.Record) {
	t.Helper()
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
	return ctx, e, base
}

func copyRun(t *testing.T, base *runner.Record, id string) *runner.Record {
	t.Helper()
	raw, _ := json.Marshal(base)
	var rec runner.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	rec.RunID = id
	rec.Seal = ""
	for _, st := range rec.Steps {
		st.AuthProfile = "default"
	}
	return &rec
}

func saveResentAndRefusedAtFetch(t *testing.T, e *env, base *runner.Record, id string) {
	t.Helper()
	rec := copyRun(t, base, id)
	rec.Status = runner.StatusError
	fetch := rec.Steps[1]
	fetch.AuthRetry = runner.AuthRetryResent
	fetch.Status = runner.StatusError
	fetch.HTTPStatus = 401
	fetch.Transport = &runner.TransportError{Code: "unauthenticated", Message: "token rejected"}
	fetch.Response = json.RawMessage(`{"code":"unauthenticated","message":"token rejected"}`)
	fetch.Error = "unauthenticated: token rejected\n       the backend refused this call at authentication, then a fresh login in this run " +
		"succeeded and the call was re-sent with the new token, and the backend refused that too: the credentials work and the token " +
		"is current, so this may be an auth regression in the backend (this rpc refusing valid tokens)"
	if _, err := e.store.SaveRun(rec); err != nil {
		t.Fatal(err)
	}
}

func TestAReadRefusedAgainAfterAFreshLoginIsHeadlinedAsAPossibleAuthRegression(t *testing.T) {
	ctx, e, base := approvedThingFlowRun(t)
	saveResentAndRefusedAtFetch(t, e, base, "20990101T000000Z-resent1")
	saveResentAndRefusedAtFetch(t, e, base, "20990101T000001Z-resent2")
	var err error
	var coded *exitError
	out := captureStdout(t, func() {
		err = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-run", "20990101T000000Z-resent1"})
	})
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatalf("a first refusal is could-not-verify, exit 3: %v\n%s", err, out)
	}
	text := out + err.Error()
	if strings.Contains(text, "restarted mid-run") || !strings.Contains(text, "may be an auth regression") {
		t.Fatalf("the re-sent call was refused with the token a fresh login had just issued: a restart does not explain that, and the step itself says it may be an auth regression: %v\n%s", err, out)
	}
	headline, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	if !strings.Contains(headline, "auth regression") {
		t.Fatalf("the headline must say what the step says: %s", headline)
	}
	out = captureStdout(t, func() {
		err = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-run", "20990101T000001Z-resent2"})
	})
	if err == nil || errors.As(err, &coded) || !strings.Contains(out, "FINDING: ") || strings.Contains(out, "restart") && !strings.Contains(out, "not a credentials problem or a restart") {
		t.Fatalf("a repeat at the same rpc is a finding, exit 1: %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "just issued") || !strings.Contains(err.Error(), "20990101T000000Z-resent1") {
		t.Fatalf("the finding says the token was freshly issued and names the earlier run: %v", err)
	}
}
