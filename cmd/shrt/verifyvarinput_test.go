package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

const varFlowChain = `apiVersion: shrt/v1
name: cli-var-flow
vars:
    label: widget
steps:
    - id: create
      call: ThingService/Create
      body:
          name: ${vars.label}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

func TestVerifyBlamesTheVarNotTheChainWhenOnlyAVarChangedTheInput(t *testing.T) {
	srv := newEchoNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-var-flow.yaml", varFlowChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-var-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-var-flow", "-note", "create echoes the name"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-var-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	check := func(label string, args ...string) {
		t.Helper()
		var err error
		out := captureStdout(t, func() { err = runVerify(ctx, append([]string{"cli-var-flow", "-quiet"}, args...)) })
		if err == nil {
			t.Fatalf("%s: the response changed, so verify must not pass:\n%s", label, out)
		}
		said := out + err.Error()
		if strings.Contains(said, "its input changed since it was confirmed") || strings.Contains(said, "Restore the chain's input") {
			t.Errorf("%s: the chain file did not change, a var did; do not blame the chain:\n%s", label, said)
		}
		if !strings.Contains(said, "label=gadget") {
			t.Errorf("%s: name the var that changed the input:\n%s", label, said)
		}
	}
	check("live -var", "-var", "label=gadget")
	entries, err := os.ReadDir(".shrt/runs/cli-var-flow")
	if err != nil {
		t.Fatal(err)
	}
	gadget := ""
	for _, e := range entries {
		raw, err := os.ReadFile(".shrt/runs/cli-var-flow/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"label": "gadget"`) {
			gadget = strings.TrimSuffix(e.Name(), ".json")
		}
	}
	if gadget == "" {
		t.Fatal("the -var run was not recorded")
	}
	check("-run of a -var run", "-run", gadget)
}
