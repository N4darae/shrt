package main

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func okFetch(id string) (int, map[string]any) {
	return 200, map[string]any{"error": map[string]any{"code": "OK"}, "id": id, "name": "widget"}
}

func sliceCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = chainSlice(context.Background(), args) })
	return out, err
}

func TestCLISliceWriteRecordsNotReproducedInsteadOfTheHypothesis(t *testing.T) {
	message := "qty must be greater than zero"
	newSliceBackend(t, &sliceBackend{fetch: func(string) (int, map[string]any) {
		return 200, map[string]any{"error": map[string]any{"code": "REJECTED", "message": message}, "id": "thing-0"}
	}})
	captureStdout(t, func() { _ = runRun(context.Background(), []string{"cli-pair", "-quiet"}) })
	message = "caller must hold role ADMIN"
	out, err := sliceCmd(t, "cli-pair", "-step", "fetch", "-run", "latest", "-verify", "-write", "probe")
	if exitCodeOf(err) != 1 {
		t.Fatalf("not reproduced exits 1, got %v:\n%s", err, out)
	}
	desc := string(mustRead(t, ".shrt/chains/probe.yaml"))
	if strings.Contains(desc, "HYPOTHESIS") || !strings.Contains(desc, "NOT REPRODUCED by 'shrt chain slice -verify'") {
		t.Fatalf("the written slice must record that -verify did not reproduce it:\n%s", desc)
	}
}

func TestCLISliceOfEveryStepSaysItReranTheChainItself(t *testing.T) {
	newSliceBackend(t, &sliceBackend{fetch: okFetch})
	captureStdout(t, func() { _ = runRun(context.Background(), []string{"cli-pair", "-quiet"}) })
	out, err := sliceCmd(t, "cli-pair", "-step", "fetch", "-run", "latest", "-verify", "-write")
	if err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	desc := string(mustRead(t, ".shrt/chains/cli-pair.yaml"))
	if strings.Contains(desc, "in source run") || !strings.Contains(desc, "own run") {
		t.Fatalf("the slice is the chain itself, so its verdict is against the chain's own run, not a source:\n%s", desc)
	}
}

func TestCLISliceOfEveryStepOfASliceKeepsItsVerdictAgainstTheSource(t *testing.T) {
	newSliceBackend(t, &sliceBackend{fetch: okFetch})
	writeFile(t, ".shrt/chains/cli-trio.yaml", `apiVersion: shrt/v1
name: cli-trio
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: look
      call: ThingService/Fetch
      body:
          id: thing-0
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
            equals: thing-0
`)
	captureStdout(t, func() { _ = runRun(context.Background(), []string{"cli-trio", "-quiet"}) })
	if out, err := sliceCmd(t, "cli-trio", "-step", "fetch", "-run", "latest", "-verify", "-write"); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	first := string(mustRead(t, ".shrt/chains/cli-trio-slice-fetch.yaml"))
	verified := regexp.MustCompile(`VERIFIED by [^\n]*source run [^\n]*`).FindString(first)
	if verified == "" {
		t.Fatalf("the slice must be verified against cli-trio's run:\n%s", first)
	}
	if out, err := sliceCmd(t, "cli-trio-slice-fetch", "-step", "fetch", "-run", "latest", "-verify", "-write"); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	second := string(mustRead(t, ".shrt/chains/cli-trio-slice-fetch.yaml"))
	if !strings.Contains(second, verified) {
		t.Fatalf("re-running the slice against its own run must not replace its verdict against the source:\n%s", second)
	}
	if !strings.Contains(second, "own run") {
		t.Fatalf("the re-run is recorded as a run against the slice's own run:\n%s", second)
	}
}
