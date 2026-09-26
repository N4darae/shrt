package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func TestAnEnvPasswordAStepBodyReadsIsScrubbedWhereverEchoed(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	r := scrubRunner(t, srv)
	t.Setenv("SHRT_TEST_OTHER_PW", "other-pw-7c1d")
	t.Setenv("SHRT_TEST_OTHER_USER", "otheruser")
	c := normalized(t, &chain.Chain{Name: "step-env", Steps: []*chain.Step{
		{ID: "echo_first", Call: "ThingService/Fetch", Body: map[string]any{"id": "early ${env.SHRT_TEST_OTHER_PW}"}, Expect: okExpect()},
		{ID: "login_other", Call: "AuthService/Login", SkipAuth: true,
			Body: map[string]any{"username": "${env.SHRT_TEST_OTHER_USER}", "password": "${env.SHRT_TEST_OTHER_PW}"}, Expect: okExpect()},
		{ID: "echo_password", Call: "ThingService/Fetch", Body: map[string]any{"id": "pw ${env.SHRT_TEST_OTHER_PW} by otheruser"},
			Expect: []chain.Expectation{{Path: "id", Contains: "${env.SHRT_TEST_OTHER_PW}"}}},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{Redact: config.DefaultRedact()})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusPassed {
		t.Fatalf("the chain should pass: %s %s", rec.Status, rec.Failure)
	}
	if text := recordText(t, rec); strings.Contains(text, "other-pw-7c1d") {
		t.Fatalf("a password a step body read from env reached the run record: %s", text)
	}
	if got := string(rec.Steps[2].Request); !strings.Contains(got, "pw <redacted> by otheruser") {
		t.Fatalf("the password should be replaced in place and the username kept, got %s", got)
	}
	if got := string(rec.Steps[0].Request); !strings.Contains(got, "early <redacted>") {
		t.Fatalf("an echo before the step that reads the password should be scrubbed too, got %s", got)
	}
}
