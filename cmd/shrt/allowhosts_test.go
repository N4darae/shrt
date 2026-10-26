package main

import (
	"context"
	"strings"
	"testing"
)

func TestRunAndGateRefuseAHostNotAllowedButDryRunStillResolves(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://api.example.test")
	err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"})
	if err == nil || !strings.Contains(err.Error(), "target.base_url names api.example.test") ||
		!strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("run: want a refusal before sending, got %v", err)
	}
	if code := exitCodeOf(err); code != 1 {
		t.Fatalf("run: want exit 1, got %d", code)
	}
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-dry-run", "-quiet"}); err != nil {
		t.Fatalf("run -dry-run sends nothing, so the host does not matter: %v", err)
	}
	if err := runGate(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "allow_hosts") {
		t.Fatalf("gate: want one refusal up front, got %v", err)
	}
}

func TestInitAgainstAHostNotAllowedSaysToAllowItNotToExportCredentials(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://api.example.test")
	writeFile(t, ".shrt/config.yaml", `target:
    base_url: http://api.example.test
descriptor:
    file: .shrt/descriptor.binpb
auth:
    call: AuthService/Login
    body: {username: "${env.API_USER}", password: "${env.API_PASSWORD}"}
    token_path: access_token
`)
	t.Setenv("API_USER", "u")
	t.Setenv("API_PASSWORD", "p")
	out := captureStdout(t, func() {
		_ = runInit(context.Background(), []string{"-build=false", "-agents=false", "-force-config", "-base-url", "http://api.example.test"})
	})
	if !strings.Contains(out, "allow_hosts") || strings.Contains(out, "Export the login credentials") {
		t.Fatalf("want the host refusal and no credentials advice:\n%s", out)
	}
}
