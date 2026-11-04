package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/N4darae/shrt/config"
)

func authlessInitWorkspace(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"code":"OK"},"access_token":"fixture-token"}`))
	}))
	t.Cleanup(srv.Close)
	t.Chdir(loginWorkspace(t, ""))
	// This regression concerns an existing workspace. Its gate script is already
	// installed, so init does not depend on the unrelated README gate extraction.
	writeFile(t, ".shrt/ci-gate.sh", "#!/usr/bin/env bash\n# previously installed gate\n")
	t.Setenv("API_USER", "fixture-user")
	t.Setenv("API_PASSWORD", "fixture-password")
	return srv.URL, &calls
}

func TestInitKeepsAnExistingAuthlessConfigAndDoesNotLogIn(t *testing.T) {
	for _, tc := range []struct {
		name, auth, newline string
		unsetCredentials    bool
	}{
		{"absent", "", "\n", false},
		{"explicit null", "auth: null # deliberately no login\n", "\n", false},
		{"CRLF comments", "", "\r\n", false},
		{"credentials absent", "", "\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url, calls := authlessInitWorkspace(t)
			if tc.unsetCredentials {
				cliUnsetEnv(t, "API_USER", "API_PASSWORD")
			}
			before := "# This service is open; keep my comments and formatting.\n" +
				"target:\n    base_url: " + url + "\ndescriptor:\n    file: .shrt/descriptor.binpb\n" + tc.auth
			before = strings.ReplaceAll(before, "\n", tc.newline)
			writeFile(t, ".shrt/config.yaml", before)
			var err error
			out := captureStdout(t, func() { err = runInit(context.Background(), []string{"-build=false", "-agents=false"}) })
			if err != nil {
				t.Fatalf("reinitializing an open service must not require login: %v\n%s", err, out)
			}
			after, err := os.ReadFile(".shrt/config.yaml")
			if err != nil || string(after) != before {
				t.Fatalf("a kept config must remain byte-for-byte identical: %v\nbefore=%q\nafter=%q\n%s", err, before, after, out)
			}
			if calls.Load() != 0 {
				t.Fatalf("init must not guess and send login on a kept authless config; got %d requests", calls.Load())
			}
			if !strings.Contains(out, "keep  .shrt/config.yaml") || !strings.Contains(out, "not written") ||
				!strings.Contains(out, "shrt.test.v1.AuthService/Login") || strings.Contains(out, "nothing in the descriptor looked like a login") {
				t.Fatalf("the detected login should be a hint, not a saved configuration or a false no-login claim:\n%s", out)
			}
			cfg, err := config.Load(".")
			if err != nil || cfg.Auth != nil {
				t.Fatalf("the service remains authless: %v %+v", err, cfg)
			}
			captureStdout(t, func() { err = runInit(context.Background(), []string{"-build=false", "-agents=false"}) })
			after, readErr := os.ReadFile(".shrt/config.yaml")
			if err != nil || readErr != nil || string(after) != before || calls.Load() != 0 {
				t.Fatalf("another init must also keep the configuration and stay offline: %v %v requests=%d", err, readErr, calls.Load())
			}
		})
	}
}

func TestInitKeepsDeclaredAuthAndConventions(t *testing.T) {
	url, calls := authlessInitWorkspace(t)
	before := "# This login is already configured by the user.\n" +
		"target:\n    base_url: " + url + "\ndescriptor:\n    file: .shrt/descriptor.binpb\n" +
		"auth:\n    call: shrt.test.v1.AuthService/Login\n    token_path: access_token\n" +
		"    body: {username: '${env.API_USER}', password: '${env.API_PASSWORD}'}\n" +
		"conventions:\n    envelope_path: error.code\n    envelope_ok: OK\n"
	writeFile(t, ".shrt/config.yaml", before)
	var err error
	out := captureStdout(t, func() { err = runInit(context.Background(), []string{"-build=false", "-agents=false"}) })
	after, readErr := os.ReadFile(".shrt/config.yaml")
	if err != nil || readErr != nil || string(after) != before || calls.Load() != 0 {
		t.Fatalf("existing auth/conventions must keep their own path: %v %v requests=%d\n%s", err, readErr, calls.Load(), out)
	}
}

func TestInitStillScaffoldsAuthWhenWritingTheConfig(t *testing.T) {
	for _, forceConfig := range []bool{false, true} {
		name := "new config"
		if forceConfig {
			name = "explicit force-config"
		}
		t.Run(name, func(t *testing.T) {
			url, calls := authlessInitWorkspace(t)
			args := []string{"-build=false", "-agents=false", "-base-url", url}
			if forceConfig {
				writeFile(t, ".shrt/config.yaml", "# explicitly replaced by -force-config\ntarget:\n    base_url: "+url+"\n")
				args = append(args, "-force-config")
			}
			var err error
			out := captureStdout(t, func() { err = runInit(context.Background(), args) })
			if err != nil {
				t.Fatalf("explicit initialization should still scaffold and observe login: %v\n%s", err, out)
			}
			cfg, err := config.Load(".")
			if err != nil || cfg.Auth == nil || cfg.Auth.Call != "shrt.test.v1.AuthService/Login" || cfg.Conventions.EnvelopeOK != "OK" {
				t.Fatalf("newly written configuration should retain scaffolding: %v %+v\n%s", err, cfg, out)
			}
			if calls.Load() != 1 || !strings.Contains(out, "GUESSED from the descriptor") {
				t.Fatalf("newly written auth should be observed once: %d requests\n%s", calls.Load(), out)
			}
		})
	}
}
