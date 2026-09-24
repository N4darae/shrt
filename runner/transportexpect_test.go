package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

func probeChain(t *testing.T, probe *chain.Step) *chain.Chain {
	t.Helper()
	return normalized(t, &chain.Chain{Name: "probe", Steps: []*chain.Step{
		probe,
		{ID: "after", Call: "ThingService/Fetch", Body: map[string]any{"id": "thing-1"}, Expect: okExpect()},
	}})
}

func unauthenticatedExpect() []chain.Expectation {
	return []chain.Expectation{
		{Path: "transport.code", Equals: "unauthenticated"},
		{Path: "transport.http_status", Equals: 401},
	}
}

func TestATransportRefusalTheStepAssertsIsAPass(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := probeChain(t, &chain.Step{ID: "no_token", Call: "ThingService/Fetch", SkipAuth: true,
		Body: map[string]any{"id": "thing-1"}, Expect: unauthenticatedExpect()})

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("the step said the call would be refused with unauthenticated/401, and it was: %s", rec.Failure)
	}
	sr := rec.Steps[0]
	if sr.Status != runner.StatusPassed {
		t.Fatalf("step status = %s, want passed", sr.Status)
	}
	if len(sr.Expect) != 2 || !sr.Expect[0].Passed || !sr.Expect[1].Passed {
		t.Fatalf("both transport assertions should have been evaluated and held: %+v", sr.Expect)
	}
	if sr.Transport == nil || sr.Transport.Code != "unauthenticated" {
		t.Fatalf("the refusal must still be recorded: %+v", sr.Transport)
	}
	if got := srv.headerFor("/shrt.test.v1.ThingService/Fetch", "Authorization"); got != "" {
		t.Fatalf("skip_auth must send no credential, sent %q", got)
	}
	if len(rec.Steps) != 2 {
		t.Fatalf("the chain should continue past an expected refusal, got %d steps", len(rec.Steps))
	}
}

func TestATransportRefusalWithTheWrongCodeFails(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := probeChain(t, &chain.Step{ID: "no_token", Call: "ThingService/Fetch", SkipAuth: true, AllowFail: true,
		Body:   map[string]any{"id": "thing-1"},
		Expect: []chain.Expectation{{Path: "transport.code", Equals: "permission_denied"}}})

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("the step named permission_denied and got unauthenticated; allow_fail must not tolerate the wrong refusal")
	}
	got := rec.Steps[0].Expect[0]
	if got.Passed || got.Rule != "equals" || got.Got != "unauthenticated" {
		t.Fatalf("the assertion should have fired and reported what came back: %+v", got)
	}
}

func TestATransportRefusalProbeFailsWhenTheCallSucceeds(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := probeChain(t, &chain.Step{ID: "should_refuse", Call: "ThingService/Fetch", AllowFail: true,
		Body: map[string]any{"id": "thing-1"}, Expect: unauthenticatedExpect()})

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("a probe that expects a 401 and gets a 200 must fail, allow_fail or not")
	}
	got := rec.Steps[0].Expect[0]
	if got.Got != chain.TransportOK {
		t.Fatalf("on success transport.code reads %q, got %+v", chain.TransportOK, got)
	}
}

func TestTransportPathsReadTheSuccessOutcomeToo(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "ok", Steps: []*chain.Step{{
		ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "thing-1"},
		Expect: []chain.Expectation{
			{Path: "transport.code", Equals: "ok"},
			{Path: "transport.http_status", Equals: 200},
			{Path: "transport.message", Exists: no()},
		},
	}}})

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("a successful call is transport code ok, status 200, no message: %s %+v", rec.Failure, rec.Steps[0].Expect)
	}
}

func TestABodyAssertionOnARefusedCallIsStillUnevaluated(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := probeChain(t, &chain.Step{ID: "no_token", Call: "ThingService/Fetch", SkipAuth: true,
		Body: map[string]any{"id": "thing-1"},
		Expect: []chain.Expectation{
			{Path: "transport.code", Equals: "unauthenticated"},
			{Path: "error.code", Equals: "OK"},
		}})

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Passed() {
		t.Fatal("an assertion on a body that never existed cannot pass because a transport assertion beside it did")
	}
	body := rec.Steps[0].Expect[1]
	if body.Rule != "unevaluated" || body.Passed {
		t.Fatalf("the body assertion should be reported unevaluated: %+v", body)
	}
}

func TestAuthInvalidSendsATokenTheBackendNeverIssued(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := probeChain(t, &chain.Step{ID: "bad_token", Call: "ThingService/Fetch", Auth: transport.InvalidTokenProfile,
		Body: map[string]any{"id": "thing-1"}, Expect: unauthenticatedExpect()})

	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("auth: invalid should be refused as unauthenticated and the chain continue: %s", rec.Failure)
	}
	sent := srv.headerFor("/shrt.test.v1.ThingService/Fetch", "Authorization")
	if sent != "Bearer "+transport.InvalidToken {
		t.Fatalf("auth: invalid must send the profile's header and scheme with a token never issued, sent %q", sent)
	}
	if srv.loginCount() != 1 {
		t.Fatalf("the probe must neither log in nor retry after its own 401; only the later step logs in, got %d logins", srv.loginCount())
	}
}

func TestAuthInvalidUsesTheHeaderOfTheProfileThatOwnsTheCall(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	cfg := partnerConfig(srv.URL)
	cfg.Auth.Profiles["partner"].Calls = []string{"shrt.test.v1.PartnerService/*"}
	cfg.Auth.Profiles["partner"].Header = "X-Partner-Token"
	cfg.Auth.Profiles["partner"].Scheme = "Token"
	c := normalized(t, &chain.Chain{Name: "partner-probe", Steps: []*chain.Step{{
		ID: "bad", Call: "PartnerService/FetchMine", Auth: transport.InvalidTokenProfile,
		Body: map[string]any{"id": "p-1"}, Expect: unauthenticatedExpect(),
	}}})

	rec, err := profileRunner(t, srv, cfg).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("probe failed: %s", rec.Failure)
	}
	if got := srv.headerFor("/shrt.test.v1.PartnerService/FetchMine", "X-Partner-Token"); got != "Token "+transport.InvalidToken {
		t.Fatalf("the invalid token belongs where the owning profile puts its real one, got %q", got)
	}
}

func TestTheReservedProfileNameIsOneName(t *testing.T) {
	if config.InvalidTokenProfile != transport.InvalidTokenProfile || chain.InvalidTokenAuth != transport.InvalidTokenProfile {
		t.Fatalf("config reserves %q, lint checks %q and the transport honours %q: they must be one name",
			config.InvalidTokenProfile, chain.InvalidTokenAuth, transport.InvalidTokenProfile)
	}
}

func TestAuthInvalidWithNoAuthConfiguredFailsBeforeTraffic(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	cfg := config.Default()
	cfg.Target.BaseURL = srv.URL
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, Auth: deps.Bindings}
	c := probeChain(t, &chain.Step{ID: "bad_token", Call: "ThingService/Fetch", Auth: transport.InvalidTokenProfile,
		Body: map[string]any{"id": "thing-1"}, Expect: unauthenticatedExpect()})

	_, err = r.Run(context.Background(), c, runner.Options{})
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("with no auth block there is no header to put an invalid token in; the run must refuse, got %v", err)
	}
	if len(srv.calls) != 0 {
		t.Fatalf("nothing should have been sent, got %v", srv.calls)
	}
}
