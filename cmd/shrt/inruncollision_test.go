package main

import (
	"context"
	"strings"
	"testing"
)

const inRunCollisionChain = `apiVersion: shrt/v1
name: cli-twice
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: w-${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: create_2
      call: ThingService/Create
      body:
          name: w-${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

func TestACollisionWithAnEarlierStepOfTheSameRunIsAChainDefect(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-twice.yaml", inRunCollisionChain)
	ctx := context.Background()
	for i, tag := range []string{"f1", "f2"} {
		var err error
		out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-twice", "-quiet", "-var", "tag=" + tag}) })
		if err == nil {
			t.Fatalf("run %d: the second create is refused, so the run fails\n%s", i+1, out)
		}
		if !strings.Contains(out, "collides with itself") || !strings.Contains(out, `step "create"`) || !strings.Contains(out, "w-"+tag) {
			t.Fatalf("run %d: the refusal is the chain colliding with its own step create in this run:\n%s", i+1, out)
		}
		if !strings.Contains(out, "a fresh -var does not help") {
			t.Fatalf("run %d: say a fresh -var cannot fix it:\n%s", i+1, out)
		}
		for _, wrong := range []string{"fixture collision", "created by something else", "points at the backend", "re-run with a fresh value"} {
			if strings.Contains(out, wrong) {
				t.Fatalf("run %d: %q blames something outside the chain:\n%s", i+1, wrong, out)
			}
		}
	}
}
