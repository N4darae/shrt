package main

import (
	"context"
	"strings"
	"testing"
)

const threeStepFlow = `apiVersion: shrt/v1
name: cli-thing-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: name
            equals: not-the-name
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: widget
    - id: fetch_again
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: widget
`

func TestAQuietRunThatStopsCountsTheStepsItNeverRan(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", threeStepFlow)
	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
	if err == nil {
		t.Fatalf("the first step fails, so the run fails:\n%s", out)
	}
	want := "2 later step(s) were not run (fetch, fetch_again)"
	if !strings.Contains(out, want) || !strings.Contains(out, "-keep-going") {
		t.Fatalf("want %q and the -keep-going hint in:\n%s", want, out)
	}

	out = captureStdout(t, func() {
		err = runRun(context.Background(), []string{"cli-thing-flow", "-quiet", "-keep-going"})
	})
	if strings.Contains(out, "were not run") {
		t.Fatalf("-keep-going ran every step, so nothing is left unrun:\n%s", out)
	}
}
