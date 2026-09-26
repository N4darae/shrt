package main

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

const twoLineDefectChain = `apiVersion: shrt/v1
name: cli-two-lines
vars:
    tag: T1
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget-${vars.tag}
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
    - id: other
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: gizmo
    - id: fine
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: widget
    - id: fetch_again
      call: ThingService/Fetch
      body:
          id: ${fetch.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: gadget
`

func twoLineWorkspace(t *testing.T) {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-two-lines.yaml", twoLineDefectChain)
	if err := runRun(context.Background(), []string{"cli-two-lines", "-quiet", "-keep-going", "-var", "tag=T9"}); err == nil {
		t.Fatalf("the chain must fail at fetch")
	}
}

func twoLineSlice(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), append([]string{"cli-two-lines"}, args...))
	})
	return out, err
}

func keptRedPaths(c *chain.Chain) string {
	parts := []string{}
	for _, k := range c.KeptRed {
		parts = append(parts, k.Step+":"+k.Path)
	}
	return strings.Join(parts, ",")
}

func TestSliceKeptRedPinsAKeptStepThatFailedInsteadOfRelaxingIt(t *testing.T) {
	twoLineWorkspace(t)
	out, err := twoLineSlice(t, "-step", "fetch_again", "-kept-red", "-write", ".shrt/chains/two-red.yaml")
	if err != nil {
		t.Fatalf("slice -kept-red: %v\n%s", err, out)
	}
	c, err := chain.LoadFile(".shrt/chains/two-red.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := keptRedPaths(c); got != "fetch:name,fetch_again:name" {
		t.Fatalf("fetch failed in the run and is kept, so it is pinned with the target, not relaxed: %s\n%s", got, out)
	}
	fetch, _ := c.Step("fetch")
	if len(fetch.Expect) != 2 {
		t.Fatalf("the pinned expectation of fetch stays in the slice: %+v", fetch.Expect)
	}
	if strings.Contains(out, "relaxed:") {
		t.Fatalf("nothing is relaxed under -kept-red:\n%s", out)
	}
	if err := runRun(context.Background(), []string{"two-red", "-quiet", "-var", "tag=T10"}); err != nil {
		t.Fatalf("the slice fails exactly as pinned, so its run exits 0: %v", err)
	}
}

func TestSliceKeptRedNamesMoreStepsOfTheSameDefect(t *testing.T) {
	twoLineWorkspace(t)
	out, err := twoLineSlice(t, "-step", "fetch_again", "-kept-red=other", "-write", ".shrt/chains/two-more.yaml")
	if err != nil {
		t.Fatalf("slice -kept-red=other: %v\n%s", err, out)
	}
	c, err := chain.LoadFile(".shrt/chains/two-more.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := keptRedPaths(c); got != "fetch:name,other:name,fetch_again:name" {
		t.Fatalf("other is kept and pinned next to the target: %s\n%s", got, out)
	}
	if _, kept := c.Step("fine"); kept {
		t.Fatalf("a step not named and not needed stays out")
	}
	if err := runRun(context.Background(), []string{"two-more", "-quiet", "-var", "tag=T11"}); err != nil {
		t.Fatalf("the slice fails exactly as pinned: %v", err)
	}
}

func TestSliceKeptRedRepeatedAddsEachStep(t *testing.T) {
	twoLineWorkspace(t)
	out, err := twoLineSlice(t, "-step", "fetch_again", "-kept-red=other", "-kept-red", "-json")
	if err != nil {
		t.Fatalf("slice: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"id": "other"`) {
		t.Fatalf("a repeated -kept-red keeps the named step:\n%s", out)
	}
}

func TestSliceKeptRedRefusesANamedStepThatDidNotFail(t *testing.T) {
	twoLineWorkspace(t)
	out, err := twoLineSlice(t, "-step", "fetch_again", "-kept-red=fine")
	if err == nil || !strings.Contains(err.Error(), "fine") || !strings.Contains(err.Error(), "nothing to pin") {
		t.Fatalf("fine passed, so -kept-red=fine has nothing to pin: %v\n%s", err, out)
	}
}

func TestSliceKeptRedVerifyPinsEachFailingKeptStepOnce(t *testing.T) {
	twoLineWorkspace(t)
	out, err := twoLineSlice(t, "-step", "fetch_again", "-kept-red", "-verify", "-var", "tag=T12", "-write", ".shrt/chains/two-verified.yaml")
	if err != nil {
		t.Fatalf("slice -kept-red -verify: %v\n%s", err, out)
	}
	c, err := chain.LoadFile(".shrt/chains/two-verified.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := keptRedPaths(c); got != "fetch:name,fetch_again:name" {
		t.Fatalf("each failing kept step is pinned once: %s\n%s", got, out)
	}
}
