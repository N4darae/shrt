package main

import (
	"context"
	"strings"
	"testing"
)

func TestDryRunRefusesABodyTheProtoRejectsLikeARealRun(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	writeFile(t, ".shrt/chains/cli-bogus.yaml", `apiVersion: shrt/v1
name: cli-bogus
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_BOGUS
      expect:
          - path: error.code
            equals: OK
`)
	ctx := context.Background()
	for _, args := range [][]string{{"cli-bogus", "-quiet"}, {"cli-bogus", "-quiet", "-dry-run"}} {
		var err error
		captureStdout(t, func() { err = runRun(ctx, args) })
		if code := exitCodeOf(err); code != 1 {
			t.Errorf("%v: a body the proto rejects is refused before anything is sent, exit 1, got %d: %v", args, code, err)
		}
		if err == nil || !strings.Contains(err.Error(), "nothing was sent") {
			t.Errorf("%v: the refusal says nothing was sent: %v", args, err)
		}
	}
}

func TestAReferenceThatCannotFillItsFieldFailsWithExit1AndDryRunNamesTheTypes(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-qty.yaml", `apiVersion: shrt/v1
name: cli-qty
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
      export:
          thing_id: id
    - id: again
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          qty: ${thing_id}
      expect:
          - path: error.code
            equals: OK
`)
	ctx := context.Background()
	for _, args := range [][]string{{"cli-qty", "-quiet", "-dry-run"}, {"cli-qty", "-quiet"}} {
		var err error
		out := captureStdout(t, func() { err = runRun(ctx, args) })
		if code := exitCodeOf(err); code != 1 {
			t.Errorf("%v: a request the proto rejects is a failure, not a missing verdict: want exit 1, got %d: %v\n%s", args, code, err, out)
		}
		if strings.Contains(out, "again sent") {
			t.Errorf("%v: the refused request was never sent:\n%s", args, out)
		}
	}
	out := captureStdout(t, func() { _ = runRun(ctx, []string{"cli-qty", "-dry-run"}) })
	if !strings.Contains(out, "${thing_id} fills qty, declared int64") || strings.Contains(out, `qty: ""`) {
		t.Errorf("a dry run names the reference and the field types, not the blank synthetic value:\n%s", out)
	}
}
