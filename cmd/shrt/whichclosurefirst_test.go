package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLIWhichSuggestsThePlainClosureFirstWhenItKeepsEveryRelatedWrite(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-writes.yaml", `apiVersion: shrt/v1
name: cli-writes
steps:
    - id: create_a
      call: ThingService/Create
      body:
          name: a
    - id: create_b
      call: ThingService/Create
      body:
          name: b
    - id: create_c
      call: ThingService/Create
      body:
          name: c-${create_a.id}
      expect:
          - path: error.code
            equals: OK
`)
	captureStdout(t, func() { _ = runRun(context.Background(), []string{"cli-writes", "-quiet"}) })
	var err error
	out := captureStdout(t, func() { err = chainWhich([]string{"-code", "OK"}) })
	if err != nil {
		t.Fatalf("chain which: %v\n%s", err, out)
	}
	for _, want := range []string{
		"reproduce: shrt chain slice cli-writes -step create_c  (2 of 3 steps)",
		"if that does not reproduce: shrt chain slice cli-writes -step create_c -keep writes  (3 of 3 steps)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
