package main

import (
	"context"
	"strings"
	"testing"
)

func TestDryRunRefusesABodyTheProtoRejectsLikeARealRun(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	writeFile(t, ".shrt/chains/cli-bogus.yaml", `apiVersion: shrt/v1
name: cli-bogus
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_BOGUS
      expect:
          - path: error.code
            equals: OK
`)
	ctx := context.Background()
	for _, args := range [][]string{{"cli-bogus", "-quiet"}, {"cli-bogus", "-quiet", "-dry-run"}} {
		var err error
		captureStdout(t, func() { err = runRun(ctx, args) })
		if code := exitCodeOf(err); code != 1 {
			t.Errorf("%v: a body the proto rejects is refused before anything is sent, exit 1, got %d: %v", args, code, err)
		}
		if err == nil || !strings.Contains(err.Error(), "nothing was sent") {
			t.Errorf("%v: the refusal says nothing was sent: %v", args, err)
		}
	}
}
