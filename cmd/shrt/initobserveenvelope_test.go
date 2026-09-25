package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func loginServer(t *testing.T, answer string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInitWritesTheEnvelopeItObservedInALoginAnswer(t *testing.T) {
	srv := loginServer(t, `{"error":{"code":"DONE"},"access_token":"tok","expires_at":"0"}`)
	dir := loginWorkspace(t, "")
	restore := chdir(t, dir)
	defer restore()
	t.Setenv("API_USER", "u")
	t.Setenv("API_PASSWORD", "p")
	out := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false", "-base-url", srv.URL}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Conventions.EnvelopePath != "error.code" || cfg.Conventions.EnvelopeOK != "DONE" {
		t.Fatalf("init logged in and saw error.code DONE, so it writes that as the conventions: %+v\n%s", cfg.Conventions, out)
	}
	if strings.Contains(out, "no conventions: block declared") || strings.Count(out, "envelope_ok DONE") != 1 {
		t.Fatalf("an observed envelope is said in one line, with no paste advice:\n%s", out)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".shrt", "chains", "example.yaml.template"))
	if !strings.Contains(string(raw), "equals: DONE") {
		t.Fatalf("the example chain asserts the observed success value:\n%s", raw)
	}
}

func TestInitKeepsAConventionsBlockAlreadyThere(t *testing.T) {
	srv := loginServer(t, `{"error":{"code":"DONE"},"access_token":"tok"}`)
	dir := loginWorkspace(t, "")
	body := "target:\n    base_url: " + srv.URL + "\ndescriptor:\n    file: .shrt/descriptor.binpb\n" +
		"auth:\n    call: shrt.test.v1.AuthService/Login\n    body:\n        username: ${env.API_USER}\n        password: ${env.API_PASSWORD}\n    token_path: access_token\n" +
		"conventions:\n    read_only_prefixes: [Fetch]\n"
	writeFile(t, filepath.Join(dir, ".shrt", "config.yaml"), body)
	restore := chdir(t, dir)
	defer restore()
	t.Setenv("API_USER", "u")
	t.Setenv("API_PASSWORD", "p")
	out := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	raw, _ := os.ReadFile(filepath.Join(dir, ".shrt", "config.yaml"))
	if string(raw) != body || strings.Contains(out, "conventions") {
		t.Fatalf("a conventions: block the user wrote is theirs; init neither rewrites nor advises on it:\n%s\n---\n%s", raw, out)
	}
}

func TestInitPrintsTheConventionsAdviceOnceWhenItCannotObserve(t *testing.T) {
	dir := shopWorkspace(t, "")
	restore := chdir(t, dir)
	defer restore()
	out := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	if strings.Count(out, "status.code") == 0 || strings.Contains(out, "paste this at the TOP LEVEL") ||
		strings.Count(out, "no conventions: block declared") != 1 {
		t.Fatalf("the conventions advice is printed once:\n%s", out)
	}
}
