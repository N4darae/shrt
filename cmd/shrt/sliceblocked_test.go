package main

import (
	"context"
	"strings"
	"testing"
)

const blockedReadChain = `apiVersion: shrt/v1
name: cli-blocked
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: gadget
    - id: fetch_again
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: ${fetch.name}
`

func blockedWorkspace(t *testing.T) {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-blocked.yaml", blockedReadChain)
	if err := runRun(context.Background(), []string{"cli-blocked", "-quiet", "-keep-going"}); err == nil {
		t.Fatalf("the chain must fail at fetch")
	}
}

func TestSliceVerifyOfAReadAnUpstreamFailureLeftUnevaluatedNamesTheUpstreamStep(t *testing.T) {
	blockedWorkspace(t)
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-blocked", "-step", "fetch_again", "-run", "latest", "-verify"})
	})
	if exitCodeOf(err) != 3 || !strings.Contains(out, "INCONCLUSIVE") {
		t.Fatalf("the source has no verdict on the held-back expectation, so an evaluation of it cannot be NOT REPRODUCED (exit %d):\n%s", exitCodeOf(err), out)
	}
	for _, want := range []string{
		`not evaluated in source run`,
		`it reads step fetch, which failed there`,
		`the slice evaluated it: name equals passed`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "-keep writes") || strings.Contains(out, "next:") {
		t.Fatalf("no dropped write explains an upstream break, so no -keep is suggested:\n%s", out)
	}
}

func TestWhichSaysWhichUpstreamStepLeftAnExpectationUnevaluated(t *testing.T) {
	blockedWorkspace(t)
	out := whichOut(t, "-rpc", "ThingService/Fetch")
	if !strings.Contains(out, "not evaluated: it reads step fetch, which failed in run") {
		t.Fatalf("which names the upstream step that blocked the expectation:\n%s", out)
	}
	if !strings.Contains(out, "shrt chain slice cli-blocked -step fetch_again -run ") || !strings.Contains(out, " -verify") {
		t.Fatalf("which says how to evaluate it on its own:\n%s", out)
	}
}
