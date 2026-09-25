package main

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

const sharedTagChain = `apiVersion: shrt/v1
name: cli-shared-tag
steps:
    - id: first
      call: ThingService/Create
      body:
          name: w-${vars.batch}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: again
      call: ThingService/Create
      body:
          name: w-${vars.batch}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

func TestWhichPinnedCommandReusesAVarADroppedStepAlsoInterpolated(t *testing.T) {
	round2Workspace(t)
	writeFile(t, ".shrt/chains/cli-shared-tag.yaml", sharedTagChain)
	if err := runRun(context.Background(), []string{"cli-shared-tag", "-quiet", "-var", "batch=T9"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	e, err := loadEnv(true)
	if err != nil {
		t.Fatal(err)
	}
	c, err := chain.Resolve(e.chainsDir(), "cli-shared-tag")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := e.store.LatestRun("cli-shared-tag")
	if err != nil {
		t.Fatal(err)
	}
	lib, err := e.library()
	if err != nil {
		t.Fatal(err)
	}
	fresh := freshVarsOf(e, lib)
	if got := fresh(c, "again", rec.RunID); len(got) != 0 {
		t.Fatalf("first, dropped from the pinned slice, created w-T9 in that run; again depends on that state, so batch must come from the run, not be fresh: %v", got)
	}
	if got := strings.Join(fresh(c, "again", ""), ","); got != "batch" {
		t.Fatalf("a closure slice re-creates everything and needs a fresh batch, got %q", got)
	}
}
