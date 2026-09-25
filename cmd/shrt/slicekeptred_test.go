package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

const oneDefectChain = `apiVersion: shrt/v1
name: cli-one-defect
vars:
    tag: T1
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget-${vars.tag}
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
          id: ${fetch.id}
      expect:
          - path: error.code
            equals: OK
    - id: other
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: widget
`

func oneDefectWorkspace(t *testing.T) {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-one-defect.yaml", oneDefectChain)
	if err := runRun(context.Background(), []string{"cli-one-defect", "-quiet", "-keep-going", "-var", "tag=T9"}); err == nil {
		t.Fatalf("the chain must fail at fetch")
	}
}

func oneDefectSlice(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), append([]string{"cli-one-defect"}, args...))
	})
	return out, err
}

func TestSliceKeptRedWritesAMinimalChainPinnedOnTheStepsFailingExpectations(t *testing.T) {
	oneDefectWorkspace(t)
	out, err := oneDefectSlice(t, "-step", "fetch", "-kept-red", "-write", ".shrt/chains/one-defect-red.yaml")
	if err != nil {
		t.Fatalf("slice -kept-red: %v\n%s", err, out)
	}
	c, err := chain.LoadFile(".shrt/chains/one-defect-red.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.KeptRed) != 1 || c.KeptRed[0].Step != "fetch" || c.KeptRed[0].Path != "name" {
		t.Fatalf("the slice pins fetch on name, the expectation that failed: %+v", c.KeptRed)
	}
	if _, kept := c.Step("other"); kept {
		t.Fatalf("the slice keeps only what fetch needs")
	}
	if !strings.Contains(out, "kept_red") {
		t.Fatalf("the output says the slice is kept red:\n%s", out)
	}
	if err := runRun(context.Background(), []string{"one-defect-red", "-quiet", "-var", "tag=T10"}); err != nil {
		t.Fatalf("the kept-red slice fails exactly as pinned, so its run exits 0: %v", err)
	}
}

func TestSliceKeptRedWithVerifyPinsOnlyAReproducedSlice(t *testing.T) {
	oneDefectWorkspace(t)
	out, err := oneDefectSlice(t, "-step", "fetch", "-kept-red", "-verify", "-var", "tag=T12", "-write", ".shrt/chains/one-defect-verified.yaml")
	if err != nil {
		t.Fatalf("slice -kept-red -verify: %v\n%s", err, out)
	}
	c, err := chain.LoadFile(".shrt/chains/one-defect-verified.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.KeptRed) != 1 || c.KeptRed[0].Path != "name" || !chain.HasVerifiedVerdict(c.Description) {
		t.Fatalf("a reproduced slice is written verified and pinned: %+v\n%s", c.KeptRed, c.Description)
	}
	if strings.Contains(out, "hypothesis until run") {
		t.Fatalf("a verified slice is not called a hypothesis:\n%s", out)
	}
}

func TestSliceKeptRedRefusesAStepThatDidNotFail(t *testing.T) {
	oneDefectWorkspace(t)
	if out, err := oneDefectSlice(t, "-step", "other", "-kept-red"); err == nil || !strings.Contains(err.Error(), "nothing to pin") {
		t.Fatalf("other passed, so there is nothing to pin: %v\n%s", err, out)
	}
}

func TestSliceWithoutFailedWritesTheRestOfTheChainThatStillRuns(t *testing.T) {
	oneDefectWorkspace(t)
	out, err := oneDefectSlice(t, "-without", "failed", "-write", ".shrt/chains/one-defect-rest.yaml")
	if err != nil {
		t.Fatalf("slice -without failed: %v\n%s", err, out)
	}
	c, err := chain.LoadFile(".shrt/chains/one-defect-rest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, s := range c.Steps {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "create,other" {
		t.Fatalf("fetch failed and fetch_again reads it, so both go: %v", ids)
	}
	for _, want := range []string{"fetch", "fetch_again", "reads fetch"} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q in:\n%s", want, out)
		}
	}
	if err := runRun(context.Background(), []string{"one-defect-rest", "-quiet", "-var", "tag=T11"}); err != nil {
		t.Fatalf("the rest passes: %v", err)
	}
	if _, err := os.Stat(".shrt/chains/cli-one-defect.yaml"); err != nil {
		t.Fatalf("the source chain is left alone: %v", err)
	}
}
