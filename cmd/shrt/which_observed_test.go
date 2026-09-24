package main

import (
	"context"
	"strings"
	"testing"
)

func writeRefusalChain(t *testing.T) {
	t.Helper()
	writeFile(t, ".shrt/chains/cli-refusal-flow.yaml", `apiVersion: shrt/v1
name: cli-refusal-flow
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
      export:
          thing_id: id
    - id: outsider_reads
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: PERMISSION_DENIED
`)
}

func whichLine(t *testing.T, out, step string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if !strings.Contains(l, " "+step+" ") || !strings.Contains(l, "OBSERVED") {
			continue
		}
		if !strings.Contains(l, "run ") && i+1 < len(lines) {
			return l + " " + strings.TrimSpace(lines[i+1])
		}
		return l
	}
	t.Fatalf("no OBSERVED line for step %s in:\n%s", step, out)
	return ""
}

func TestCLIWhichShowsTheRecordedCodeNotTheAssertedOne(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeRefusalChain(t)

	if err := runRun(context.Background(), []string{"cli-refusal-flow", "-quiet"}); err == nil {
		t.Fatal("the backend answers OK where the chain asserts a refusal, so the run must fail")
	}

	var werr error
	out := captureStdout(t, func() { werr = chainWhich([]string{"-rpc", "ThingService/Fetch"}) })
	if werr != nil {
		t.Fatalf("chain which: %v", werr)
	}
	line := whichLine(t, out, "outsider_reads")
	observed := line[strings.Index(line, "run "):]
	if strings.Contains(observed, "PERMISSION_DENIED") {
		t.Errorf("the record's response carried OK, yet the observed part names the asserted code: %q", line)
	}
	if !strings.Contains(observed, "OK") {
		t.Errorf("the observed part must show the code the recorded response carried: %q", line)
	}
	if !strings.Contains(observed, "FAILED") {
		t.Errorf("the recorded step failed its assertions, and the line must say so: %q", line)
	}
}

func TestCLIWhichMarksAPassingObservationAsPassed(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)

	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	var werr error
	out := captureStdout(t, func() { werr = chainWhich([]string{"-rpc", "ThingService/Fetch"}) })
	if werr != nil {
		t.Fatalf("chain which: %v", werr)
	}
	line := whichLine(t, out, "fetch")
	if strings.Contains(line, "FAILED") || !strings.Contains(line, "passed") {
		t.Errorf("a step that passed must read as passed: %q", line)
	}
}
