package main

import (
	"context"
	"strings"
	"testing"
)

const twoProductChain = `apiVersion: shrt/v1
name: cli-two
vars:
    tag: base
steps:
    - id: create
      call: ThingService/Create
      body:
          name: sku-${vars.tag}-
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: create_2
      call: ThingService/Create
      body:
          name: sku-${vars.tag}-2-
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

func TestACollisionWithAnotherTagOfTheSameChainNamesTheRunAndStepThatSentIt(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-two.yaml", twoProductChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-two", "-quiet", "-var", "tag=x"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
	var err error
	out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-two", "-quiet", "-var", "tag=x-2"}) })
	if err == nil {
		t.Fatalf("sku-x-2- was created by the first run's create_2:\n%s", out)
	}
	text := err.Error() + out
	if strings.Contains(text, "no recorded run of this chain used that value") {
		t.Fatalf("a recorded run of this chain did send the value, at another step:\n%s", text)
	}
	if !strings.Contains(text, `already sent that value at step "create_2" with tag=x`) {
		t.Fatalf("the collision names the run's step and tag that sent the value:\n%s", text)
	}
}
