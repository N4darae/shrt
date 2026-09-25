package main

import (
	"context"
	"strings"
	"testing"
)

func TestAStepVolatilePatternMasksOnlyItsOwnStepInVerify(t *testing.T) {
	srv := newEchoNameBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-scoped.yaml", `apiVersion: shrt/v1
name: cli-scoped
vars:
    label: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: ${vars.label}
      expect:
          - path: error.code
            equals: OK
    - id: create_masked
      call: ThingService/Create
      body:
          name: ${vars.label}
      volatile:
          - name
      expect:
          - path: error.code
            equals: OK
`)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-scoped", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-scoped", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-scoped", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	out := captureStdout(t, func() { _ = runVerify(ctx, []string{"cli-scoped", "-masked", "-var", "label=second"}) })
	if strings.Contains(out, "create name (first -> second) hidden by name") {
		t.Fatalf("create_masked's volatile pattern hid a change on create:\n%s", out)
	}
	if !strings.Contains(out, "create_masked name (first -> second) hidden by name") {
		t.Fatalf("the pattern still masks its own step:\n%s", out)
	}
	echoes := out[strings.Index(out, "values echoing a fixture name"):]
	if !strings.Contains(echoes, "create name (first -> second)") {
		t.Fatalf("create's name echoes the var, and is listed as such:\n%s", out)
	}
}
