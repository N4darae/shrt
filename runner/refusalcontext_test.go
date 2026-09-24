package runner_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAnInBandRefusalLineCarriesTheRefusalsOwnMessage(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.createRefusal = "out_of_stock"

	c := normalized(t, &chain.Chain{Name: "refused", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	line := rec.Steps[0].Expect[0].String()
	if !strings.Contains(line, "got=out_of_stock") || !strings.Contains(line, `message="refused"`) {
		t.Fatalf("the failure line must carry the refusal's own message, got %q", line)
	}
	if !strings.Contains(rec.Warning, "conventions.envelope_ok") || !strings.Contains(rec.Warning, "out_of_stock (1)") {
		t.Fatalf("a run where no response carried envelope_ok must warn and name what was seen, got %q", rec.Warning)
	}
}

func TestARunThatSawTheSuccessValueDoesNotWarnAboutEnvelopeOK(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	rec, err := newRunner(t, srv).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Warning != "" {
		t.Fatalf("no warning when the success value was seen, got %q", rec.Warning)
	}
}

func TestPerItemRefusalsCarryTheItemsReasonAndPointAtEnvelopeOKWhenTheyMatchTheTopLevel(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newBatchRunner(t, srv).Run(context.Background(),
		batchChain("BatchService/Preview", []any{"ok", "bad"}, chain.Expectation{Path: "error.code", Equals: "OK"}), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got, ok := itemEnvelopeResult(t, rec)
	if !ok || !strings.Contains(fmt.Sprint(got.Got), `message="bad was refused"`) {
		t.Fatalf("each refused item names its own reason, got %+v", got)
	}

	defer chain.ApplyConventions(nil, "", "")
	chain.ApplyConventions(nil, "error.code", "SUCCESS")
	rec, err = newBatchRunner(t, srv).Run(context.Background(),
		batchChain("BatchService/Preview", []any{"ok"}, chain.Expectation{Path: "id", Exists: new(bool)}), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got, ok = itemEnvelopeResult(t, rec)
	if !ok || !strings.Contains(got.Detail, "conventions.envelope_ok: OK") || strings.Contains(got.Detail, "were refused while") {
		t.Fatalf("an item 'refusal' equal to the top-level value points at envelope_ok, got %+v", got)
	}
}

func TestATokenlessLoginNamesTheAuthProfileAndTheEnvVarsItsBodyReads(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.refuseLogin = true
	t.Setenv("SHRT_TEST_LOGIN_USER", "staff")
	t.Setenv("SHRT_TEST_LOGIN_PASSWORD", "wrong")
	cfg := testConfig(srv.URL)
	cfg.Auth.Body = map[string]any{"username": "${env.SHRT_TEST_LOGIN_USER}", "password": "${env.SHRT_TEST_LOGIN_PASSWORD}"}
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, Auth: deps.Bindings}
	rec, err := r.Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	msg := rec.Steps[0].Error
	for _, want := range []string{"no token at", `auth profile "default"`, "${env.SHRT_TEST_LOGIN_PASSWORD}", "${env.SHRT_TEST_LOGIN_USER}"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the login failure must mention %q, got %q", want, msg)
		}
	}
}
