package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func partnerConfig(baseURL string) *config.Config {
	cfg := testConfig(baseURL)
	cfg.Auth.Profiles = map[string]*config.Auth{
		"partner": {
			Call: "PartnerAuthService/Login",
			Body: map[string]any{"username": "partner@example.com", "password": "portal"},
		},
	}
	return cfg
}

func profileRunner(t *testing.T, srv *fakeServer, cfg *config.Config) *runner.Runner {
	t.Helper()
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatalf("build deps: %v", err)
	}
	return &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, ValidateOutput: true, Auth: deps.Bindings}
}

func normalized(t *testing.T, c *chain.Chain) *chain.Chain {
	t.Helper()
	if err := c.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	return c
}

func partnerChain(steps ...*chain.Step) *chain.Chain {
	return &chain.Chain{Name: "partner-flow", Steps: steps}
}

func okExpect() []chain.Expectation {
	return []chain.Expectation{{Path: "error.code", Equals: "OK"}}
}

func TestAStepCanAskForANamedAuthProfile(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, partnerChain(
		&chain.Step{ID: "create", Call: "ThingService/Create",
			Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
		&chain.Step{ID: "mine", Call: "PartnerService/FetchMine", Auth: "partner",
			Body: map[string]any{"id": "p-1"}, Expect: okExpect()},
	))

	rec, err := profileRunner(t, srv, partnerConfig(srv.URL)).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("want passed, got %s: %s", rec.Status, rec.Failure)
	}
	staff := srv.headerFor("/shrt.test.v1.ThingService/Create", "Authorization")
	partner := srv.headerFor("/shrt.test.v1.PartnerService/FetchMine", "Authorization")
	if !strings.Contains(staff, "token-") || strings.Contains(staff, "partner-token-") {
		t.Fatalf("the staff step must carry the staff token, got %q", staff)
	}
	if !strings.Contains(partner, "partner-token-") {
		t.Fatalf("the partner step must carry the partner token, got %q", partner)
	}
}

func TestAProfileClaimsItsOwnProceduresWithoutAPerStepKey(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	cfg := partnerConfig(srv.URL)
	cfg.Auth.Profiles["partner"].Calls = []string{"shrt.test.v1.PartnerService/*"}

	c := normalized(t, partnerChain(
		&chain.Step{ID: "mine", Call: "PartnerService/FetchMine",
			Body: map[string]any{"id": "p-1"}, Expect: okExpect()},
	))

	rec, err := profileRunner(t, srv, cfg).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("a calls pattern must route the step without a per-step auth key, got %s: %s", rec.Status, rec.Failure)
	}
	if got := srv.headerFor("/shrt.test.v1.PartnerService/FetchMine", "Authorization"); !strings.Contains(got, "partner-token-") {
		t.Fatalf("routed step carried %q", got)
	}
	if srv.loginCount() != 0 {
		t.Fatalf("no staff step ran, so the staff profile must never log in, got %d logins", srv.loginCount())
	}
}

func TestAnInChainPartnerLoginSeedsItsOwnProfile(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, partnerChain(
		&chain.Step{ID: "partner_login", Call: "PartnerAuthService/Login",
			Body: map[string]any{"username": "partner@example.com", "password": "portal"}, Expect: okExpect()},
		&chain.Step{ID: "mine", Call: "PartnerService/FetchMine", Auth: "partner",
			Body: map[string]any{"id": "p-1"}, Expect: okExpect()},
	))

	rec, err := profileRunner(t, srv, partnerConfig(srv.URL)).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("want passed, got %s: %s", rec.Status, rec.Failure)
	}
	if got := srv.partnerLoginCount(); got != 1 {
		t.Fatalf("an explicit partner login step must seed the partner profile, want 1 login, got %d", got)
	}
	if got := srv.loginCount(); got != 0 {
		t.Fatalf("a partner step must not fall back to the staff login, got %d staff logins", got)
	}
	if note := rec.Steps[0].Note; !strings.Contains(note, "partner") {
		t.Fatalf("the seeding step should say which profile it seeded, got %q", note)
	}
}

func TestAnUnknownProfileFailsBeforeAnyTrafficIsSent(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, partnerChain(
		&chain.Step{ID: "mine", Call: "PartnerService/FetchMine", Auth: "prtner",
			Body: map[string]any{"id": "p-1"}, Expect: okExpect()},
	))

	_, err := profileRunner(t, srv, partnerConfig(srv.URL)).Run(context.Background(), c, runner.Options{})
	if err == nil {
		t.Fatal("a typo in a profile name must be an error, not a silent fallback to the default profile")
	}
	if !strings.Contains(err.Error(), "prtner") || !strings.Contains(err.Error(), "partner") {
		t.Fatalf("the error should name the typo and the profiles that exist, got %v", err)
	}
	if srv.loginCount()+srv.partnerLoginCount() != 0 {
		t.Fatal("nothing may reach the server before the profile names are checked")
	}
}

func TestTheDefaultProfileStillDrivesEveryOtherCall(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	rec, err := profileRunner(t, srv, partnerConfig(srv.URL)).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("declaring a second profile must not disturb the default one, got %s: %s", rec.Status, rec.Failure)
	}
	if got := srv.loginCount(); got != 1 {
		t.Fatalf("want exactly one staff login, got %d", got)
	}
	if got := srv.partnerLoginCount(); got != 0 {
		t.Fatalf("an unused profile must never log in, got %d", got)
	}
}

func TestAProfileIsNotConfusedWithSkipAuth(t *testing.T) {
	c := &chain.Chain{Name: "contradiction", Steps: []*chain.Step{
		{ID: "mine", Call: "PartnerService/FetchMine", Auth: "partner", SkipAuth: true,
			Body: map[string]any{"id": "p-1"}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	issues := chain.Lint(c, catalogtest.New())
	for _, i := range issues {
		if i.Severity == chain.SeverityError && strings.Contains(i.Message, "skip_auth") {
			return
		}
	}
	t.Fatalf("skip_auth plus auth must be reported as a contradiction, got %v", issues)
}
