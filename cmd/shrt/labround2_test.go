package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestProgressMarksAnErrorStepERRORNotFAIL(t *testing.T) {
	cases := []struct {
		status string
		dry    bool
		want   string
	}{
		{runner.StatusPassed, false, "ok"},
		{runner.StatusFailed, false, "FAIL"},
		{runner.StatusError, false, "ERROR"},
		{runner.StatusError, true, "ERROR"},
		{runner.StatusSkipped, false, "SKIP"},
		{runner.StatusSkipped, true, "--"},
	}
	for _, c := range cases {
		line := progressLine(&runner.StepRecord{Index: 1, ID: "create_customer", Call: "X/Y", Status: c.status}, c.dry)
		if got := strings.Fields(line)[0]; got != c.want {
			t.Errorf("status %s dry=%v: progress mark %q, want %q (line %q)", c.status, c.dry, got, c.want, line)
		}
	}
}

func chdirToVarWorkspace(t *testing.T) *int {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, filepath.Join(".shrt", "chains", "tagged.yaml"), `apiVersion: shrt/v1
name: tagged
steps:
    - id: first
      call: ThingService/Create
      body:
          name: first
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: create_customer
      call: ThingService/Create
      body:
          name: c-${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`)
	return &calls
}

func TestCLIRunRefusesAnUnsuppliedVarBeforeSendingAnything(t *testing.T) {
	for _, args := range [][]string{{"tagged", "-quiet"}, {"tagged", "-quiet", "-dry-run"}} {
		calls := chdirToVarWorkspace(t)
		err := runRun(context.Background(), args)
		if err == nil || !strings.Contains(err.Error(), "-var tag=...") || !strings.Contains(err.Error(), "${vars.tag}") {
			t.Fatalf("%v: want a refusal naming ${vars.tag} and -var tag=..., got %v", args, err)
		}
		if *calls != 0 {
			t.Fatalf("%v: %d request(s) sent before the run refused", args, *calls)
		}
	}
	calls := chdirToVarWorkspace(t)
	if err := runRun(context.Background(), []string{"tagged", "-quiet", "-var", "tag=x"}); err != nil {
		t.Fatalf("with -var tag=x: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("with the var supplied both steps should be sent, got %d", *calls)
	}
}

func TestCLIVerifyRefusesAnUnsuppliedVarBeforeSendingAnything(t *testing.T) {
	calls := chdirToVarWorkspace(t)
	writeFile(t, ".shrt/safespots/tagged.json", `{"chain":"tagged","run_id":"r","target":"t","confirmed_by":"test","confirmed_at":"2026-01-01T00:00:00Z","digest":"d","steps":[]}`)
	resealSafeSpot(t, ".shrt/safespots/tagged.json")
	err := runVerify(context.Background(), []string{"tagged", "-quiet"})
	if err == nil || !strings.Contains(err.Error(), "-var tag=...") {
		t.Fatalf("want a refusal naming -var tag=..., got %v", err)
	}
	if *calls != 0 {
		t.Fatalf("%d request(s) sent before verify refused", *calls)
	}
}
