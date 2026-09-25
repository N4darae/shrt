package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func TestInitRerunAddsAMissingRoleProfileAndKeepsTheRestOfTheConfig(t *testing.T) {
	dir := loginWorkspace(t, accountsReadme)
	restore := chdir(t, dir)
	defer restore()
	t.Setenv("CLERK_USER", "")
	t.Setenv("CLERK_PASSWORD", "")
	captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	path := filepath.Join(dir, ".shrt", "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "target:\n    base_url: http://127.0.0.1:8080", "target:\n    base_url: http://127.0.0.1:9999", 1)
	edited = strings.Replace(edited, "    - '**.created_at'\n", "    - '**.created_at'\n    - '**.kept_by_hand'\n", 1)
	writeFile(t, path, edited)

	t.Setenv("CLERK_USER", "clerk")
	t.Setenv("CLERK_PASSWORD", "x")
	out := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init again: %v", err)
		}
	})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("the edited config must still load: %v", err)
	}
	clerk := cfg.Auth.Profiles["clerk"]
	if clerk == nil || clerk.Body["username"] != "${env.CLERK_USER}" || clerk.Call != cfg.Auth.Call {
		t.Fatalf("CLERK_USER and CLERK_PASSWORD are exported now and the README names clerk, so init adds the profile it said it would:\n%s", out)
	}
	if cfg.Target.BaseURL != "http://127.0.0.1:9999" || !strings.Contains(strings.Join(cfg.Volatile, ","), "**.kept_by_hand") {
		t.Fatalf("adding a profile keeps everything else in the config: base_url %s, volatile %v", cfg.Target.BaseURL, cfg.Volatile)
	}
	if !strings.Contains(out, "profile clerk") {
		t.Fatalf("init names the profile it added to the existing config:\n%s", out)
	}
	again := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init a third time: %v", err)
		}
	})
	if strings.Contains(again, "profile clerk") {
		t.Fatalf("the profile is there now, so a third init adds nothing:\n%s", again)
	}
}
