package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/config"
)

func loginWorkspace(t *testing.T, readme string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".shrt", "descriptor.binpb"), string(catalogtest.Descriptor()))
	if readme != "" {
		writeFile(t, filepath.Join(dir, "README.md"), readme)
	}
	return dir
}

const accountsReadme = "# backend\n\n| username | password | role |\n|---|---|---|\n| admin | s3cret | ADMIN |\n| clerk | s3cret | CLERK |\n"

func TestInitAddsAProfileForASecondCredentialSetTheReadmeNames(t *testing.T) {
	dir := loginWorkspace(t, accountsReadme)
	restore := chdir(t, dir)
	defer restore()
	t.Setenv("CLERK_USER", "clerk")
	t.Setenv("CLERK_PASSWORD", "x")
	t.Setenv("DB_USER", "postgres")
	t.Setenv("DB_PASSWORD", "x")
	out := captureStdout(t, func() {
		if err := initAllowingIncomplete(t, []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth == nil {
		t.Fatalf("init found a login and must write auth:\n%s", out)
	}
	clerk := cfg.Auth.Profiles["clerk"]
	if clerk == nil {
		t.Fatalf("CLERK_USER and CLERK_PASSWORD are set and the README names clerk, so init scaffolds a clerk profile on the same login:\n%s", out)
	}
	if clerk.Call != cfg.Auth.Call || clerk.Body["username"] != "${env.CLERK_USER}" || clerk.Body["password"] != "${env.CLERK_PASSWORD}" {
		t.Fatalf("the profile logs in through the same rpc with its own credentials: %+v", clerk)
	}
	if _, bogus := cfg.Auth.Profiles["db"]; bogus {
		t.Fatalf("DB_USER is set but nothing in the README names db; it is not a role of this backend")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".shrt", "config.yaml"))
	if strings.Contains(string(raw), "#") {
		t.Fatalf("the scaffolded config carries no comments:\n%s", raw)
	}
	if !strings.Contains(out, "profile clerk") {
		t.Fatalf("init names the profile it added:\n%s", out)
	}
}

func TestInitSaysHowToAddAProfilePerRoleWhenItCannotTell(t *testing.T) {
	dir := loginWorkspace(t, accountsReadme)
	restore := chdir(t, dir)
	defer restore()
	out := captureStdout(t, func() {
		if err := initAllowingIncomplete(t, []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	if !strings.Contains(out, "auth.profiles.clerk") || !strings.Contains(out, "CLERK_USER") {
		t.Fatalf("the README lists accounts admin and clerk and no credential set for clerk is exported, so init says how to add one:\n%s", out)
	}
}
