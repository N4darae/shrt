package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func memberConfig(baseURL string, extra ...string) *config.Config {
	cfg := testConfig(baseURL)
	cfg.Auth.Profiles = map[string]*config.Auth{
		"member": {
			Call: "PartnerAuthService/Login",
			Body: map[string]any{"username": "alice", "password": "alice-pin"},
		},
	}
	for _, name := range extra {
		cfg.Auth.Profiles[name] = &config.Auth{
			Call: "PartnerAuthService/Login",
			Body: map[string]any{"username": name, "password": name + "-pin"},
		}
	}
	return cfg
}

func principalSwapChain(loginAs string) *chain.Chain {
	return &chain.Chain{Name: "principal-swap", Steps: []*chain.Step{
		{ID: "login_other", Call: "PartnerAuthService/Login", SkipAuth: true,
			Body: map[string]any{"username": loginAs, "password": loginAs + "-pin"}, Expect: okExpect()},
		{ID: "mine", Call: "PartnerService/FetchMine", Auth: "member",
			Body: map[string]any{"id": "p-1"}, Expect: okExpect()},
	}}
}

func stepByID(t *testing.T, rec *runner.Record, id string) *runner.StepRecord {
	t.Helper()
	s, ok := rec.Step(id)
	if !ok {
		t.Fatalf("no step %q in the record", id)
	}
	return s
}

func TestALoginWithOtherCredentialsDoesNotSeedTheOnlyProfileOnThatRpc(t *testing.T) {
	for _, extra := range [][]string{nil, {"carol"}} {
		srv := newFakeServer()
		c := normalized(t, principalSwapChain("bob"))
		rec, err := profileRunner(t, srv, memberConfig(srv.URL, extra...)).Run(context.Background(), c, runner.Options{})
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !rec.Passed() {
			t.Fatalf("profiles %v: want passed, got %s: %s", extra, rec.Status, rec.Failure)
		}
		bob := srv.partnerTokenIssuedTo(t, "bob")
		alice := srv.partnerTokenIssuedTo(t, "alice")
		mine := srv.headerFor("/shrt.test.v1.PartnerService/FetchMine", "Authorization")
		if mine != "Bearer "+alice {
			t.Fatalf("profiles %v: the auth: member step carried %q, want alice's %q (bob's is %q) — a login "+
				"with bob's credentials must never become the member token, however many profiles share the rpc",
				extra, mine, alice, bob)
		}
		note := stepByID(t, rec, "login_other").Note
		if !strings.Contains(note, "did not seed") || !strings.Contains(note, "member") {
			t.Errorf("profiles %v: login_other note = %q, want it to say the member token was not seeded and why", extra, note)
		}
		srv.Close()
	}
}

func TestALoginWithAProfilesOwnCredentialsSeedsOnlyThatProfile(t *testing.T) {
	for _, extra := range [][]string{nil, {"carol"}} {
		srv := newFakeServer()
		c := normalized(t, principalSwapChain("alice"))
		rec, err := profileRunner(t, srv, memberConfig(srv.URL, extra...)).Run(context.Background(), c, runner.Options{})
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !rec.Passed() {
			t.Fatalf("profiles %v: want passed, got %s: %s", extra, rec.Status, rec.Failure)
		}
		if got := srv.partnerLoginCount(); got != 1 {
			t.Fatalf("profiles %v: the explicit login used the member credentials, so it must seed member: want 1 partner login, got %d", extra, got)
		}
		if note := stepByID(t, rec, "login_other").Note; !strings.Contains(note, "seeded the member") {
			t.Errorf("profiles %v: login note = %q, want it to name the member profile", extra, note)
		}
		srv.Close()
	}
}

func TestEachStepRecordsTheAuthProfileItRanUnder(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "who-ran", Steps: []*chain.Step{
		{ID: "login_other", Call: "PartnerAuthService/Login", SkipAuth: true,
			Body: map[string]any{"username": "bob", "password": "bob-pin"}, Expect: okExpect()},
		{ID: "create", Call: "ThingService/Create",
			Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "mine", Call: "PartnerService/FetchMine", Auth: "member",
			Body: map[string]any{"id": "p-1"}, Expect: okExpect()},
	}})
	rec, err := profileRunner(t, srv, memberConfig(srv.URL)).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := map[string]string{"login_other": "none", "create": "default", "mine": "member"}
	for id, profile := range want {
		if got := stepByID(t, rec, id).AuthProfile; got != profile {
			t.Errorf("step %s auth_profile = %q, want %q", id, got, profile)
		}
	}
}
