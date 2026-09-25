package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLISliceVerifyReproducedShowsTheMatchedFailingExpectation(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-red-flow.yaml", `apiVersion: shrt/v1
name: cli-red-flow
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
`)
	ctx := context.Background()
	captureStdout(t, func() { _ = runRun(ctx, []string{"cli-red-flow", "-quiet"}) })
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(ctx, []string{"cli-red-flow", "-step", "fetch", "-verify", "-run", "latest"})
	})
	if !strings.Contains(out, "verify reproduced") {
		t.Fatalf("the slice gives fetch the verdict it had: %v\n%s", err, out)
	}
	if !strings.Contains(out, "failed: name want=gadget source got=widget, slice got=widget") {
		t.Errorf("a reproduced verdict must show the failing expectation it matched, with its values:\n%s", out)
	}
}
