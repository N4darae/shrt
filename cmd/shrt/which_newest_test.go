package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newPhasedBackend(phase *int) *httptest.Server {
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			if *phase == 3 {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "INTERNAL"}})
				return
			}
			next++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next)})
		case "/shrt.test.v1.ThingService/Fetch":
			code := "OK"
			if *phase == 1 {
				code = "PERMISSION_DENIED"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code}, "name": "widget"})
		default:
			w.WriteHeader(404)
		}
	}))
}

func whichOut(t *testing.T, args ...string) string {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = chainWhich(args) })
	if err != nil {
		t.Fatalf("chain which %v: %v", args, err)
	}
	return out
}

func latestRunID(t *testing.T, chainName string) string {
	t.Helper()
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := e.store.LatestRun(chainName)
	if err != nil {
		t.Fatal(err)
	}
	return rec.RunID
}

func TestCLIWhichCitesTheNewestReachingRunNotAnOlderOneThatMatched(t *testing.T) {
	phase := 1
	srv := newPhasedBackend(&phase)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeRefusalChain(t)

	if err := runRun(context.Background(), []string{"cli-refusal-flow", "-quiet"}); err != nil {
		t.Fatalf("phase 1 refuses the outsider, so the run passes: %v", err)
	}
	phase = 2
	if err := runRun(context.Background(), []string{"cli-refusal-flow", "-quiet"}); err == nil {
		t.Fatal("phase 2 lets the outsider read, so the run must fail")
	}
	newest := latestRunID(t, "cli-refusal-flow")

	out := whichOut(t, "-code", "PERMISSION_DENIED")
	line := whichLine(t, out, "outsider_reads")
	if !strings.Contains(line, "run "+newest+" got OK, step FAILED") {
		t.Errorf("the newest run reached the step, got OK and failed; that is the evidence, not the older passing run:\n%s", out)
	}
	if !strings.Contains(out, "failed: error.code want=PERMISSION_DENIED got=OK") {
		t.Errorf("a FAILED observation must name the failing expectation:\n%s", out)
	}
	if !strings.Contains(out, "1 with a local run record that reached a matching step") {
		t.Errorf("the footer must count the chain whose run reached the step:\n%s", out)
	}

	var jerr error
	raw := captureStdout(t, func() { jerr = chainWhich([]string{"-code", "PERMISSION_DENIED", "-json"}) })
	if jerr != nil {
		t.Fatal(jerr)
	}
	if !strings.Contains(raw, `"observed": {`) || !strings.Contains(raw, `"holds": false`) {
		t.Errorf("-json must carry the contradicting observation:\n%s", raw)
	}
}

func TestCLIWhichSaysTheNewestRunDidNotReachTheStep(t *testing.T) {
	phase := 1
	srv := newPhasedBackend(&phase)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeRefusalChain(t)

	if err := runRun(context.Background(), []string{"cli-refusal-flow", "-quiet"}); err != nil {
		t.Fatalf("phase 1: %v", err)
	}
	reached := latestRunID(t, "cli-refusal-flow")
	phase = 3
	_ = runRun(context.Background(), []string{"cli-refusal-flow", "-quiet", "-keep-going"})
	newest := latestRunID(t, "cli-refusal-flow")
	if newest == reached {
		t.Fatal("phase 3 must leave a newer run record")
	}

	out := whichOut(t, "-code", "PERMISSION_DENIED")
	if !strings.Contains(whichLine(t, out, "outsider_reads"), "run "+reached+" got PERMISSION_DENIED, step passed") {
		t.Errorf("the only run that reached the step is the evidence:\n%s", out)
	}
	if !strings.Contains(out, "newest run "+newest+" did not reach it") {
		t.Errorf("the output must say the newest run did not reach the step:\n%s", out)
	}
}

func TestCLIWhichByRPCReadsGotFromTheAssertedPath(t *testing.T) {
	phase := 1
	srv := newPhasedBackend(&phase)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-app-code.yaml", `apiVersion: shrt/v1
name: cli-app-code
steps:
    - id: refused
      call: ThingService/Fetch
      body:
          id: x
      expect:
          - path: error.code
            equals: PERMISSION_DENIED
          - path: error.details.0.app_code
            equals: 1304
`)
	_ = runRun(context.Background(), []string{"cli-app-code", "-quiet"})
	line := whichLine(t, whichOut(t, "-rpc", "ThingService/Fetch"), "refused")
	if !strings.Contains(line, "asserts 1304") {
		t.Fatalf("the digits assertion is the one shown: %q", line)
	}
	if strings.Contains(line, "got PERMISSION_DENIED,") {
		t.Errorf("got must be read from the path of the shown assertion, not the first code path: %q", line)
	}
	if !strings.Contains(line, "nothing at error.details.0.app_code") {
		t.Errorf("the response has no app_code, and the line must say so: %q", line)
	}
}
