package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

const twoDefectChain = `apiVersion: shrt/v1
name: cli-two-defects
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
          - path: %s
            equals: %s
    - id: fetch_again
      call: ThingService/Fetch
      body:
          id: ${fetch.id}
      expect:
          - path: name
            equals: gadget
`

func twoDefectWorkspace(t *testing.T, path, want string) {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-two-defects.yaml", strings.Replace(strings.Replace(twoDefectChain, "%s", path, 1), "%s", want, 1))
	if err := runRun(context.Background(), []string{"cli-two-defects", "-quiet", "-keep-going", "-var", "tag=T9"}); err == nil {
		t.Fatalf("the chain must fail at fetch and fetch_again")
	}
}

func twoDefectSlice(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = keptRedSlice(context.Background(), append([]string{"cli-two-defects"}, args...))
	})
	return out, err
}

func TestSliceKeptRedOfTheWholeChainWritesTheSliceWithEveryPin(t *testing.T) {
	twoDefectWorkspace(t, "name", "gadget")
	out, err := twoDefectSlice(t, "-step", "fetch_again", "-kept-red=fetch,fetch_again", "-verify", "-var", "tag=T20", "-write")
	if err != nil {
		t.Fatalf("slice: %v\n%s", err, out)
	}
	c, err := chain.LoadFile(".shrt/chains/cli-two-defects-slice-fetch_again.yaml")
	if err != nil {
		t.Fatalf("a kept-red slice of the whole chain is written under its slice name: %v\n%s", err, out)
	}
	pinned := []string{}
	for _, k := range c.KeptRed {
		pinned = append(pinned, k.Step)
	}
	if strings.Join(pinned, ",") != "fetch,fetch_again" {
		t.Fatalf("both failing steps are pinned, got %v\n%s", pinned, out)
	}
	if strings.Contains(out, "written, and") || strings.Contains(out, "itself: no ") {
		t.Fatalf("the slice was written:\n%s", out)
	}
	if err := runRun(context.Background(), []string{"cli-two-defects-slice-fetch_again", "-quiet", "-var", "tag=T21"}); err != nil {
		t.Fatalf("the slice fails exactly as pinned: %v", err)
	}
}

func TestSliceKeptRedIntoTheSourceNamesNoWithoutCommand(t *testing.T) {
	twoDefectWorkspace(t, "name", "gadget")
	out, err := twoDefectSlice(t, "-step", "fetch_again", "-kept-red=fetch", "-write", ".shrt/chains/cli-two-defects.yaml")
	if err != nil {
		t.Fatalf("slice: %v\n%s", err, out)
	}
	c, err := chain.LoadFile(".shrt/chains/cli-two-defects.yaml")
	if err != nil || c.Name != "cli-two-defects" || len(c.KeptRed) != 2 {
		t.Fatalf("kept_red is written into the chain named, under its own name: %v %+v", err, c)
	}
	if strings.Contains(out, "-without") || strings.Contains(out, "mv .shrt/chains/cli-two-defects.yaml") {
		t.Fatalf("leaving the pinned steps out of the chain holding the pins would lose the defect:\n%s", out)
	}
}

func TestSliceRefusesAValueStartingWithEquals(t *testing.T) {
	twoDefectWorkspace(t, "name", "gadget")
	out, err := twoDefectSlice(t, "-step", "fetch_again", "-write", "=fetch")
	if err == nil || !strings.Contains(err.Error(), "did you mean -write=fetch") {
		t.Fatalf("want a did-you-mean, got %v\n%s", err, out)
	}
	entries, _ := os.ReadDir(".shrt/chains")
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), "=") {
			t.Fatalf("no file named after the value: %s", en.Name())
		}
	}
}

func TestSliceKeptRedOnAPassingWriteNamesTheFailingReadsOfIt(t *testing.T) {
	oneDefectWorkspace(t)
	out, err := oneDefectSlice(t, "-step", "create", "-kept-red")
	if err == nil || !strings.Contains(err.Error(), "nothing to pin") {
		t.Fatalf("create passed, so there is nothing to pin on it: %v\n%s", err, out)
	}
}
