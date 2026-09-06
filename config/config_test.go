package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func write(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, config.DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

const twoProfiles = `
target: {base_url: "http://localhost:8080"}
auth:
  call: iam.v1.StaffAuthService/Login
  token_path: access_token
  expires_path: expires_at
  scheme: Bearer
  leeway_seconds: 30
  profiles:
    partner:
      call: partner.v1.PartnerAuthService/Login
      body: {username: p, password: q}
      calls: [acme.partner.*]
`

func TestAProfileInheritsTheEnvelopeItDoesNotRestate(t *testing.T) {
	cfg, err := config.Load(write(t, twoProfiles))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	partner := cfg.AuthProfiles()["partner"]
	if partner == nil {
		t.Fatal("the partner profile is missing")
	}
	if partner.TokenPath != "access_token" || partner.ExpiresPath != "expires_at" {
		t.Fatalf("token paths not inherited: %+v", partner)
	}
	if partner.LeewaySecs != 30 {
		t.Fatalf("leeway not inherited, got %d", partner.LeewaySecs)
	}
	if _, scheme := partner.HeaderScheme(); scheme != "Bearer" {
		t.Fatalf("scheme not inherited, got %q", scheme)
	}
	if partner.Call != "partner.v1.PartnerAuthService/Login" {
		t.Fatalf("a profile must keep its own call, got %q", partner.Call)
	}
	if len(partner.Calls) != 1 || partner.Calls[0] != "acme.partner.*" {
		t.Fatalf("calls not preserved: %v", partner.Calls)
	}
}

func TestInheritanceDoesNotMutateTheDeclaredProfile(t *testing.T) {
	cfg, err := config.Load(write(t, twoProfiles))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	_ = cfg.AuthProfiles()
	if got := cfg.Auth.Profiles["partner"].TokenPath; got != "" {
		t.Fatalf("resolving must not write back into the config, token_path became %q", got)
	}
}

func TestTheTopLevelBlockIsTheDefaultProfile(t *testing.T) {
	cfg, err := config.Load(write(t, twoProfiles))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	profiles := cfg.AuthProfiles()
	if profiles[config.DefaultAuthProfile] != cfg.Auth {
		t.Fatal("the top-level auth block must be addressable as the default profile")
	}
	names := cfg.AuthProfileNames()
	if strings.Join(names, ",") != "default,partner" {
		t.Fatalf("profile names = %v, want sorted default,partner", names)
	}
}

func TestNoAuthMeansNoProfiles(t *testing.T) {
	cfg, err := config.Load(write(t, `target: {base_url: "http://localhost:8080"}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.AuthProfiles()) != 0 || len(cfg.AuthProfileNames()) != 0 {
		t.Fatalf("a config with no auth declares no profiles, got %v", cfg.AuthProfileNames())
	}
	if header, scheme := cfg.AuthHeader(); header != "Authorization" || scheme != "Bearer" {
		t.Fatalf("defaults must survive a nil auth block, got %q %q", header, scheme)
	}
}

func TestAMalformedProfileIsRejectedAtLoad(t *testing.T) {
	cases := map[string]string{
		"no call": `
auth:
  call: a/B
  profiles:
    partner: {body: {username: p}}
`,
		"named default": `
auth:
  call: a/B
  profiles:
    default: {call: c/D}
`,
		"named invalid": `
auth:
  call: a/B
  profiles:
    invalid: {call: c/D}
`,
		"nested profiles": `
auth:
  call: a/B
  profiles:
    partner:
      call: c/D
      profiles:
        deeper: {call: e/F}
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Load(write(t, body)); err == nil {
				t.Fatal("want an error at load time, before anything tries to use the profile")
			}
		})
	}
}
