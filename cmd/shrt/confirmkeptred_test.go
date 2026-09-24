package main

import (
	"context"
	"strings"
	"testing"
)

const keptRedThingChain = `apiVersion: shrt/v1
name: cli-red-flow
kept_red:
    - step: fetch
      path: name
      got: widget
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
`

func TestConfirmSaysAKeptRedChainIsNeverConfirmed(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-red-flow.yaml", keptRedThingChain)
	if err := runRun(context.Background(), []string{"cli-red-flow", "-quiet"}); err != nil {
		t.Fatalf("the chain fails as pinned, so run exits 0: %v", err)
	}
	err := runConfirm(context.Background(), []string{"cli-red-flow", "-note", "fetch names the defect"})
	if err == nil {
		t.Fatal("a kept_red chain must never be proposed")
	}
	for _, want := range []string{"kept red on purpose", "never confirmed", "evidence", "not baselines"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %v", want, err)
		}
	}
}
