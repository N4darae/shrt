package main

import (
	"context"
	"strings"
	"testing"
)

const keepGoingFlow = `apiVersion: shrt/v1
name: cli-keep-going
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: id
            exists: true
    - id: create_bad
      call: ThingService/Create
      body:
          name: gadget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: id
            equals: not-the-id
    - id: fetch_bad
      call: ThingService/Fetch
      body:
          id: ${create_bad.id}
    - id: fetch_bad_again
      call: ThingService/Fetch
      body:
          id: ${create_bad.id}
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: widget
    - id: fetch_compared
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: id
            equals: ${create_bad.id}
`

func TestKeepGoingPrintsOnlyTheFailuresAGroupPerCauseAndAPassedCount(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-keep-going.yaml", keepGoingFlow)
	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-keep-going", "-keep-going"}) })
	if err == nil {
		t.Fatalf("create_bad fails, so the run fails:\n%s", out)
	}
	for _, want := range []string{"FAIL   2 create_bad", "3 step(s) unevaluated behind create_bad: fetch_compared answered id=", "2 step(s) passed (-v prints every step)\n", "-keep-going: 4 of 6 steps did not pass (above)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q in:\n%s", want, out)
		}
	}
	for _, not := range []string{"ok     1 create", "fetch_bad ", "not evaluated", "not sent:"} {
		if strings.Contains(out, not) {
			t.Fatalf("without -v, %q is not printed:\n%s", not, out)
		}
	}
	out = captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-keep-going", "-keep-going", "-v"}) })
	for _, want := range []string{"ok     1 create", "SKIP   3 fetch_bad", "not evaluated: ${create_bad.id}"} {
		if !strings.Contains(out, want) {
			t.Fatalf("-v prints every step; want %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "unevaluated behind") || strings.Contains(out, "step(s) passed") {
		t.Fatalf("-v prints the table instead of the counts:\n%s", out)
	}
}
