package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func runIDsOf(t *testing.T, chainName string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(".shrt", "runs", chainName))
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, e := range entries {
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}
	return ids
}

func writeEnvFetchChain(t *testing.T) {
	t.Helper()
	writeEnvFetchChainReading(t, "${env.SHRT_LAB5_NAME}")
}

func writeEnvFetchChainReading(t *testing.T, id string) {
	t.Helper()
	writeFile(t, ".shrt/chains/cli-env-flow.yaml", `apiVersion: shrt/v1
name: cli-env-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: `+id+`
      expect:
          - path: error.code
            equals: OK
`)
}

func TestCLISlicePinLatestUsesTheNewestRunThatReachedTheStep(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeEnvFetchChain(t)
	t.Setenv("SHRT_LAB5_NAME", "thing-1")
	if err := runRun(context.Background(), []string{"cli-env-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	reaching := runIDsOf(t, "cli-env-flow")[0]
	writeFile(t, ".shrt/chains/cli-env-flow.yaml", `apiVersion: shrt/v1
name: cli-env-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: NOT_THIS
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${env.SHRT_LAB5_NAME}
      expect:
          - path: error.code
            equals: OK
`)
	_ = runRun(context.Background(), []string{"cli-env-flow", "-quiet"})
	writeEnvFetchChain(t)
	ids := runIDsOf(t, "cli-env-flow")
	if len(ids) != 2 {
		t.Fatalf("want two run records, got %v", ids)
	}
	stopped := ids[0]
	if stopped == reaching {
		stopped = ids[1]
	}

	var err error
	var out string
	stderr := captureStderr(t, func() {
		out = captureStdout(t, func() {
			err = chainSlice(context.Background(), []string{"cli-env-flow", "-step", "fetch", "-mode", "pin", "-run", "latest"})
		})
	})
	if err != nil {
		t.Fatalf("pin -run latest must fall back to the newest run that reached the step: %v", err)
	}
	if !strings.Contains(out, "run "+reaching) {
		t.Errorf("pin must use run %s, which reached fetch:\n%s", reaching, out)
	}
	if !strings.Contains(stderr, stopped) {
		t.Errorf("the note must name the newest run that did not reach the step: %q", stderr)
	}

	captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-env-flow", "-step", "fetch", "-mode", "pin", "-run", stopped})
	})
	if err == nil {
		t.Fatal("an explicit run that never reached the step has nothing to pin and must be refused")
	}
	if !strings.Contains(err.Error(), "-run "+reaching) {
		t.Errorf("the refusal must name a run that did reach the step: %v", err)
	}
}

func TestCLISliceVerifyWithoutRunNamesWhatIsRequired(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	err := chainSlice(context.Background(), []string{"cli-thing-flow", "-step", "fetch", "-verify"})
	if err == nil {
		t.Fatal("-verify without -run must be refused")
	}
	if strings.Contains(err.Error(), "-mode closure") || !strings.Contains(err.Error(), "-verify needs -run") {
		t.Errorf("closure mode needs no run; only -verify does: %v", err)
	}
}

func TestCLISliceVerifySaysNotReproducedWhenTheFailureGotAnotherValue(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-got-flow.yaml", `apiVersion: shrt/v1
name: cli-got-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
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
          - path: id
            equals: thing-999
`)
	if err := runRun(context.Background(), []string{"cli-got-flow", "-quiet"}); err == nil {
		t.Fatal("the fetch asserts an id the backend never returns, so the run must fail")
	}
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-got-flow", "-step", "fetch", "-run", "latest", "-verify"})
	})
	if !strings.Contains(out, "NOT REPRODUCED") {
		t.Errorf("source got thing-1 and the slice got thing-2: the same failure with another value is not a reproduction:\n%s", out)
	}
	if !strings.Contains(out, "source got thing-1") || !strings.Contains(out, "slice got thing-2") {
		t.Errorf("the difference must say both values:\n%s", out)
	}
	if got := exitCodeOf(err); got != 1 {
		t.Errorf("not reproduced exits %d, want 1", got)
	}
}

func TestCLISliceVerifyLeavesAWriteThatFailedInTheSourceRunOutOfNext(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-failwrite-flow.yaml", `apiVersion: shrt/v1
name: cli-failwrite-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: fill
      call: ThingService/Create
      body:
          name: filler
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: id
            equals: never
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
`)
	_ = runRun(context.Background(), []string{"cli-failwrite-flow", "-quiet", "-keep-going"})
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-failwrite-flow", "-step", "fetch", "-run", "latest", "-verify"})
	})
	if !strings.Contains(out, "INCONCLUSIVE") {
		t.Fatalf("the slice dropped the write fill, so a match is inconclusive:\n%s", out)
	}
	if strings.Contains(out, "  next: ") {
		t.Errorf("keeping fill, which failed in the source run, can only stop the slice before fetch:\n%s", out)
	}
	if !strings.Contains(out, "fill (failed)") {
		t.Errorf("the output must say why fill is left out:\n%s", out)
	}
	if got := exitCodeOf(err); got != 3 {
		t.Errorf("inconclusive exits %d, want 3", got)
	}
}

func TestCLISliceVerifyRefusesADeclaredVarTheSourceRunAlreadyUsed(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-tag-flow.yaml", `apiVersion: shrt/v1
name: cli-tag-flow
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
`)
	if err := runRun(context.Background(), []string{"cli-tag-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	var err error
	captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-tag-flow", "-step", "fetch", "-run", "latest", "-verify"})
	})
	if err == nil || !strings.Contains(err.Error(), "-var tag=<fresh>") {
		t.Fatalf("tag is declared, interpolated into a name, and the run used its default: -verify must refuse and ask for a fresh one, got %v", err)
	}
	captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-tag-flow", "-step", "fetch", "-run", "latest", "-verify", "-var", "tag=T2"})
	})
	if err != nil {
		t.Fatalf("a fresh -var tag must let -verify run: %v", err)
	}
}

func TestCLISliceVerifyWriteOfAWholeChainRecordsTheVerdictInIt(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-thing-flow", "-step", "fetch", "-run", "latest", "-verify", "-write"})
	})
	if err != nil {
		t.Fatalf("slice -verify -write: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(".shrt/chains/cli-thing-flow-slice-fetch.yaml"); statErr == nil {
		t.Error("the slice keeps every step, so it is the chain itself; a copy under another name is a second chain to keep in step")
	}
	c, loadErr := chain.LoadFile(".shrt/chains/cli-thing-flow.yaml")
	if loadErr != nil {
		t.Fatalf("the chain must still load after its verdict is recorded: %v", loadErr)
	}
	if !strings.Contains(c.Description, "VERIFIED by 'shrt chain slice -verify'") {
		t.Errorf("the verdict must be recorded in the chain named:\n%s", c.Description)
	}
	if len(c.Steps) != 2 || c.Name != "cli-thing-flow" {
		t.Errorf("recording the verdict must not change the chain: %+v", c)
	}
}
