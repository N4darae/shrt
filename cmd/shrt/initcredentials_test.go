package main

import (
	"strings"
	"testing"
)

func TestInitSaysFirstThatTheLoginCredentialsAreNotExported(t *testing.T) {
	dir := shopWorkspace(t, shopConfig+"auth:\n    call: shop.auth.v1.AuthService/Login\n    body:\n        username: ${env.SHRT_INIT_T_USER}\n        password: ${env.SHRT_INIT_T_PASSWORD}\n    token_path: access_token\n")
	restore := chdir(t, dir)
	defer restore()
	run := func() string {
		return captureStdout(t, func() {
			if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
				t.Fatalf("init: %v", err)
			}
		})
	}
	out := run()
	first, _, _ := strings.Cut(out, "\n")
	if first != "credentials not exported (SHRT_INIT_T_PASSWORD, SHRT_INIT_T_USER): export them first, then shrt init observes the envelope; continuing without" {
		t.Fatalf("init must open with the unexported credentials, got:\n%s", out)
	}
	if strings.Count(out, "credentials not exported") != 1 || !strings.Contains(out, "keep  .shrt/config.yaml") {
		t.Fatalf("the rest of init's output must follow once:\n%s", out)
	}
	t.Setenv("SHRT_INIT_T_USER", "u")
	t.Setenv("SHRT_INIT_T_PASSWORD", "p")
	if out := run(); strings.Contains(out, "credentials not exported") {
		t.Fatalf("exported credentials must not be reported:\n%s", out)
	}
}
