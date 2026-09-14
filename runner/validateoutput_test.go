package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func TestValidateOutputIsOffByDefaultAndReachableFromConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Root = t.TempDir()
	cfg.Target.BaseURL = "http://127.0.0.1:1"

	r, _, err := runner.NewFromConfig(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.ValidateOutput {
		t.Error("default must stay false: a descriptor that has drifted from the deployed binary should " +
			"degrade to a warning, not fail every chain in the corpus at once")
	}

	cfg.Conventions.ValidateOutput = true
	r, _, err = runner.NewFromConfig(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.ValidateOutput {
		t.Fatal("conventions.validate_output does not reach the Runner, so the ValidateOutput branch is " +
			"unreachable in production and a response the descriptor rejects always yields a passing verdict")
	}
}

func TestValidateOutputTurnsADescriptorMismatchIntoAFailure(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.unknownField = true

	c := testChain()
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	r := newRunner(t, srv)
	r.ValidateOutput = true

	rec, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatalf("with validate_output on, a response the descriptor does not recognise must fail the "+
			"step — descriptor/binary skew is the thing a replay tool exists to catch. status=%s", rec.Status)
	}
	if !strings.Contains(rec.Failure, "no_such_field_in_the_proto") {
		t.Errorf("the failure should name the field that did not match: %q", rec.Failure)
	}
	if rec.Status != runner.StatusFailed {
		t.Fatalf("status = %q. GRAMMAR section 8 and PITFALLS 4 both define 'error' as no request "+
			"having been sent, and say it is evidence about the fixture rather than the backend. "+
			"The request was sent here and the backend answered 200, so the one status "+
			"validate_output can produce contradicted the vocabulary the docs teach for reading it",
			rec.Status)
	}
	if !rec.Steps[len(rec.Steps)-1].Drift {
		t.Error("the step should be marked as descriptor drift, so allow_fail does not swallow it " +
			"and a reader can tell it apart from a server refusal")
	}
}

func TestAllowFailDoesNotSwallowADescriptorMismatch(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.unknownField = true

	c := testChain()
	c.Steps[len(c.Steps)-1].AllowFail = true
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	r := newRunner(t, srv)
	r.ValidateOutput = true

	rec, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("allow_fail says a refusal is tolerated. A response the descriptor cannot read is " +
			"not a refusal — it is skew between the descriptor and the deployed binary, and " +
			"swallowing it hides the one thing validate_output was turned on to find")
	}
	if !strings.Contains(rec.Failure, "not a response the descriptor cannot read") {
		t.Errorf("the failure should say why allow_fail did not cover it: %q", rec.Failure)
	}
}
