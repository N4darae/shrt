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

func newNamingBackend(name *string) *httptest.Server {
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
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "OK"}, "id": body["id"], "name": *name,
				"created_at": "2026-09-0" + itoa(next) + "T10:00:00Z",
			})
		default:
			w.WriteHeader(404)
		}
	}))
}

func runIDs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".shrt/runs/cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".json"))
	}
	return out
}

func TestCLIDiffComparesTwoRunsWithoutASafeSpot(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)

	for range 2 {
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	}
	var derr error
	out := captureStdout(t, func() { derr = runDiff(context.Background(), []string{"cli-thing-flow"}) })
	if derr != nil {
		t.Fatalf("two runs differing only in ids and timestamps must compare the same: %v\n%s", derr, out)
	}
	if !strings.Contains(out, "not a verdict against a confirmed safe spot") {
		t.Errorf("the output must say it compares two runs, not a baseline:\n%s", out)
	}
	if _, err := os.Stat(".shrt/safespots/cli-thing-flow.json"); err == nil {
		t.Fatal("diff must not create a safe spot")
	}

	name = "gadget"
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err == nil {
		t.Fatal("the fetch step asserts name widget, so this run must fail")
	}
	out = captureStdout(t, func() { derr = runDiff(context.Background(), []string{"cli-thing-flow", "latest~1", "latest"}) })
	if exitCodeOf(derr) != 1 {
		t.Fatalf("runs that differ exit 1, got %v\n%s", derr, out)
	}
	for _, want := range []string{"fetch", "passed -> failed", "first failing step moved", "a=widget b=gadget"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "thing-2") || strings.Contains(out, "thing-3") {
		t.Errorf("ids differ every run and must be masked:\n%s", out)
	}
}

func TestCLIDiffFindsTheChainFromTwoRunIds(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	for range 2 {
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	}
	ids := runIDs(t)
	var derr error
	out := captureStdout(t, func() { derr = runDiff(context.Background(), ids[:2]) })
	if derr != nil {
		t.Fatalf("shrt diff %s %s: %v\n%s", ids[0], ids[1], derr, out)
	}
	if !strings.Contains(out, "cli-thing-flow") {
		t.Errorf("the report must name the chain it found:\n%s", out)
	}
}
