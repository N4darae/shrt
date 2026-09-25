package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func newRenameTotalBackend(total *int) *httptest.Server {
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next)})
		case "/shrt.test.v1.ThingService/Fetch":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "widget", "total": *total})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestARenamedStepIsComparedWithItsOldSelf(t *testing.T) {
	total := 750
	srv := newRenameTotalBackend(&total)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	first := runIDs(t)[0]
	raw, err := os.ReadFile(".shrt/chains/cli-thing-flow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", strings.Replace(string(raw), "- id: fetch\n", "- id: fetch_thing\n", 1))
	total = 1

	var verr error
	out := captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if code := exitCodeOf(verr); code != 1 || !strings.Contains(verr.Error(), "regression") {
		t.Fatalf("the renamed step's total changed 750 -> 1: want a regression, got %d: %v\n%s", code, verr, out)
	}
	for _, want := range []string{"fetch -> fetch_thing", "total", "750", "1"} {
		if !strings.Contains(out, want) {
			t.Errorf("verify output lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"this run has no such step", "the safe spot has no such step", "chain change"} {
		if strings.Contains(out, not) {
			t.Errorf("a rename is not a missing and an unexpected step (%q):\n%s", not, out)
		}
	}

	out = captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-supersede", "-note", "renamed"}); err != nil {
			t.Fatalf("supersede: %v", err)
		}
	})
	if !strings.Contains(out, "renamed from fetch") || !strings.Contains(out, "750 -> 1") {
		t.Errorf("the supersede review names the rename and shows the renamed step's change:\n%s", out)
	}
	if strings.Contains(out, "-> absent") || strings.Contains(out, "absent ->") {
		t.Errorf("the supersede review shows the rename as a missing and an added step:\n%s", out)
	}

	var derr error
	out = captureStdout(t, func() { derr = runDiff(ctx, []string{"cli-thing-flow", first, "latest"}) })
	if exitCodeOf(derr) != 1 {
		t.Fatalf("the runs differ in total: want exit 1, got %v\n%s", derr, out)
	}
	if !strings.Contains(out, "fetch -> fetch_thing") || !strings.Contains(out, "total") || strings.Contains(out, "not reached in B") {
		t.Errorf("diff pairs the renamed step and shows its change:\n%s", out)
	}
}
