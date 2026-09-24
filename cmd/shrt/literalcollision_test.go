package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const literalNameChain = `apiVersion: shrt/v1
name: cli-unique
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: fixed-widget
          idempotency_key: key ${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

func TestVerifyCallsACollisionOnALiteralFieldAChainDefect(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", literalNameChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=l9"}) })
	var coded *exitError
	if err == nil || (errors.As(err, &coded) && coded.code != 1) {
		t.Fatalf("a chain that collides with itself is a chain defect, exit 1: %v\n%s", err, out)
	}
	for _, want := range []string{"collides with itself", "name is the literal fixed-widget", "name: name-${vars.tag}"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "fixture collision") || strings.Contains(err.Error(), "tag=l9") {
		t.Errorf("the literal is to blame, not the var: %v", err)
	}
	if !strings.Contains(out, "CHAIN DEFECT") {
		t.Errorf("the human output must say it is a chain defect:\n%s", out)
	}
}

func TestRunNamesALiteralCollisionInsteadOfTheVar(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", literalNameChain)
	ctx := context.Background()
	captureStdout(t, func() { _ = runRun(ctx, []string{"cli-unique", "-quiet"}) })
	out := captureStdout(t, func() { _ = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=two"}) })
	if !strings.Contains(out, "collides with itself") || strings.Contains(out, "fixture collision") {
		t.Fatalf("run must blame the literal, not the var:\n%s", out)
	}
}
