package runner_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func TestAuthCoverageAnswersWhatTheMiddlewareWouldDo(t *testing.T) {
	cfg := testConfig("http://unused")
	cfg.Auth.Profiles = map[string]*config.Auth{
		"partner": {
			Call:   "PartnerAuthService/Login",
			Body:   map[string]any{"username": "${env.PARTNER_USER}", "password": "${env.PARTNER_PASSWORD}"},
			Header: "X-Api-Key",
			Calls:  []string{"shrt.test.v1.PartnerService/*"},
		},
	}
	covers, err := runner.AuthCoverage(cfg, catalogtest.New())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		step    *chain.Step
		profile string
		header  string
		covered bool
	}{
		{"default principal", &chain.Step{Call: "ThingService/Create"}, "default", "Authorization", true},
		{"owned by a profile", &chain.Step{Call: "PartnerService/FetchMine"}, "partner", "X-Api-Key", true},
		{"named profile", &chain.Step{Call: "ThingService/Fetch", Auth: "partner"}, "partner", "X-Api-Key", true},
		{"skip_auth", &chain.Step{Call: "ThingService/Fetch", SkipAuth: true}, "", "", false},
		{"a login call", &chain.Step{Call: "AuthService/Login"}, "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			profile, header, covered := covers(c.step)
			if profile != c.profile || header != c.header || covered != c.covered {
				t.Errorf("got (%q, %q, %v), want (%q, %q, %v)", profile, header, covered, c.profile, c.header, c.covered)
			}
		})
	}
}

func TestAuthCoverageWithNoAuthBlockCoversNothing(t *testing.T) {
	cfg := testConfig("http://unused")
	cfg.Auth = nil
	covers, err := runner.AuthCoverage(cfg, catalogtest.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, covered := covers(&chain.Step{Call: "ThingService/Create"}); covered {
		t.Fatal("with no auth block nothing overwrites a header, so a hand-written one is what is sent")
	}
}
