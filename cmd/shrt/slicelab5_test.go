package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
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

func TestCLISliceLatestRefusesWhenTheNewestRunDidNotReachTheStep(t *testing.T) {
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
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-env-flow", "-step", "fetch", "-mode", "pin", "-run", "latest"})
	})
	if exitCodeOf(err) != 3 || strings.Contains(out, "run "+reaching) {
		t.Fatalf("-run latest is the newest run, which never reached fetch: refuse with exit 3, never fall back to an older run: %v\n%s", err, out)
	}
	for _, want := range []string{stopped, "step fetch", "-step create -run " + stopped, "-run " + reaching} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q: %v", want, err)
		}
	}
	if strings.Count(err.Error(), "\n") != 0 {
		t.Errorf("the refusal is one line: %q", err)
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
	names := []string{"widget", "gadget"}
	calls := 0
	newSliceBackend(t, &sliceBackend{fetch: func(id string) (int, map[string]any) {
		name := names[min(calls, len(names)-1)]
		calls++
		return 200, map[string]any{"error": map[string]any{"code": "OK"}, "id": id, "name": name}
	}})
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
          - path: name
            equals: thing-999
`)
	if err := runRun(context.Background(), []string{"cli-got-flow", "-quiet"}); err == nil {
		t.Fatal("the fetch asserts a name the backend never returns, so the run must fail")
	}
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-got-flow", "-step", "fetch", "-run", "latest", "-verify"})
	})
	if !strings.Contains(out, "NOT REPRODUCED") {
		t.Errorf("source got widget and the slice got gadget: the same failure with another value is not a reproduction:\n%s", out)
	}
	if !strings.Contains(out, "source got widget") || !strings.Contains(out, "slice got gadget") {
		t.Errorf("the difference must say both values:\n%s", out)
	}
	if got := exitCodeOf(err); got != 1 {
		t.Errorf("not reproduced exits %d, want 1", got)
	}
}

func TestCLISliceVerifyKeepsAWriteThatAnsweredButFailedAnExpectation(t *testing.T) {
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
          meta:
              trace_id: thing-1
      expect:
          - path: error.code
            equals: OK
          - path: name
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
		t.Fatalf("the slice dropped fill, a write on the thing fetch reads, so a match is inconclusive:\n%s", out)
	}
	if got := exitCodeOf(err); got != 3 {
		t.Errorf("inconclusive exits %d, want 3", got)
	}
	next := regexp.MustCompile(`next: shrt chain slice (.*)`).FindStringSubmatch(out)
	if next == nil {
		t.Fatalf("fill was answered and only an expectation failed, so its write took effect and next: must keep it:\n%s", out)
	}
	cmd := strings.Fields(next[1])
	again := captureStdout(t, func() { err = chainSlice(context.Background(), cmd) })
	if !strings.Contains(again, "verify reproduced") {
		t.Fatalf("with fill kept and its failed expectation relaxed the slice reaches fetch (err %v):\n%s", err, again)
	}
	if !strings.Contains(again, "relaxed:") || !strings.Contains(again, "fill name equals") {
		t.Errorf("the slice must say it dropped fill's failed expectation:\n%s", again)
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
