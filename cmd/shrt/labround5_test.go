package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func deadCLITarget(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return "http://" + addr
}

func TestCLIKeepGoingAgainstADeadTargetPrintsOneLineForTheUnsentSteps(t *testing.T) {
	target := deadCLITarget(t)
	chdirToFreshCLIWorkspace(t, target)
	writeFile(t, ".shrt/chains/cli-three.yaml", `apiVersion: shrt/v1
name: cli-three
steps:
    - id: one
      call: ThingService/Create
      body: {name: a, kind: KIND_A}
    - id: two
      call: ThingService/Create
      body: {name: b, kind: KIND_A}
    - id: three
      call: ThingService/Create
      body: {name: c, kind: KIND_A}
`)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runRun(context.Background(), []string{"cli-three", "-keep-going", "-save=false"})
	})
	if runErr == nil {
		t.Fatal("a run against a dead target must not pass")
	}
	if n := strings.Count(out, "every remaining step"); n != 1 {
		t.Fatalf("the unsent steps are reported on one progress line, got %d:\n%s", n, out)
	}
	if strings.Contains(out, " 3 three") {
		t.Fatalf("a step not sent behind a dead target gets no progress line of its own:\n%s", out)
	}
	if strings.Count(out, "dial tcp") > 2 {
		t.Fatalf("the dial error is not repeated for every unsent step:\n%s", out)
	}
}

func TestAConfigThatDoesNotParseIsNotReportedAsMissing(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	writeFile(t, ".shrt/config.yaml", `target:
    base_url: http://127.0.0.1:1
conventions:
    envelope_path: status.code
conventions:
    envelope_ok: SUCCESS
`)
	err := runDoctor(context.Background(), nil)
	if err == nil {
		t.Fatal("a config that does not parse must fail doctor")
	}
	msg := err.Error()
	if strings.Contains(msg, "shrt init' first") || !strings.Contains(msg, "does not parse") || !strings.Contains(msg, "conventions") {
		t.Fatalf("the config exists: say it does not parse and quote the yaml error, got %q", msg)
	}
}

func TestCLIVerifyComparesEveryReachableStepAndNamesTheFirstFailure(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-three-flow.yaml", `apiVersion: shrt/v1
name: cli-three-flow
steps:
    - id: create
      call: ThingService/Create
      body: {name: widget, kind: KIND_A}
      expect:
          - path: id
            not_empty: true
    - id: fetch
      call: ThingService/Fetch
      body: {id: "${create.id}"}
    - id: other
      call: ThingService/Fetch
      body: {id: thing-9}
`)
	if err := runRun(context.Background(), []string{"cli-three-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	entries, err := os.ReadDir(".shrt/runs/cli-three-flow")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(".shrt/runs/cli-three-flow", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	spotRaw, _ := json.Marshal(map[string]any{
		"chain": "cli-three-flow", "run_id": rec["run_id"], "target": rec["target"],
		"confirmed_by": "test", "confirmed_at": "2026-01-01T00:00:00Z", "digest": "d", "steps": rec["steps"],
	})
	writeFile(t, ".shrt/safespots/cli-three-flow.json", string(spotRaw))
	writeFile(t, ".shrt/chains/cli-three-flow.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-three-flow.yaml")), "not_empty: true", "equals: nope", 1))

	var verr error
	out := captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-three-flow", "-quiet", "-save=false"})
	})
	if verr == nil {
		t.Fatal("a failing replay must not verify clean")
	}
	if strings.Contains(out, "length") {
		t.Fatalf("verify replays with keep-going: no step is missing, so nothing changed length:\n%s", out)
	}
	if !strings.Contains(out, "first failing step: step 1 create") || !strings.Contains(out, "[fetch] not_reached") {
		t.Fatalf("verify names the first failure and the step skipped behind it:\n%s", out)
	}
	if strings.Contains(out, "[other] not_reached") {
		t.Fatalf("a step independent of the failure is still sent and compared:\n%s", out)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCLIRunExitsThreeForAnErroredRunAndOneForAFailedOne(t *testing.T) {
	chdirToFreshCLIWorkspace(t, deadCLITarget(t))
	err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet", "-save=false"})
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatalf("a run whose status is error exits 3, got %v", err)
	}

	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml")), "equals: widget", "equals: gadget", 1))
	err = runRun(context.Background(), []string{"cli-thing-flow", "-quiet", "-save=false"})
	if err == nil || errors.As(err, &coded) {
		t.Fatalf("a failed run keeps exit 1, got %v", err)
	}
}

func TestRunDashHDocumentsTheExitCodes(t *testing.T) {
	saved := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	_ = runRun(context.Background(), []string{"-h"})
	_ = w.Close()
	os.Stderr = saved
	buf := make([]byte, 1<<16)
	n, _ := io.ReadFull(r, buf)
	out := string(buf[:n])
	if !strings.Contains(out, "exit codes:") || !strings.Contains(out, "  3  error") {
		t.Fatalf("run -h must document its exit codes, got:\n%s", out)
	}
}
