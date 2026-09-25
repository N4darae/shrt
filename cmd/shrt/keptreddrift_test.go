package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAPinnedStepThatReturnsSomethingElseIsNotAsPinned(t *testing.T) {
	var total atomic.Int64
	total.Store(5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "gadget", "total": total.Load()})
	}))
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, filepath.Join(".shrt", "chains", "red.yaml"), `name: red
kept_red:
    - {step: fetch, path: name}
steps:
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-1}
      expect:
          - {path: name, equals: widget}
`)
	run := func() (string, error) {
		var err error
		out := captureStdout(t, func() { err = runRun(context.Background(), []string{"red"}) })
		return out, err
	}
	out, err := run()
	if err != nil || !strings.Contains(out, "FAILED AS PINNED") || !strings.Contains(out, "this run is the reference for the next one") {
		t.Fatalf("an old pin with no got still fails as pinned, and the first run says it had nothing to compare with: %v\n%s", err, out)
	}
	if out, err = run(); err != nil || !strings.Contains(out, "the pinned steps return what they returned in run") {
		t.Fatalf("an unchanged pinned step stays as pinned: %v\n%s", err, out)
	}
	total.Store(7)
	for i := 0; i < 2; i++ {
		out, err = run()
		if err == nil || !strings.Contains(out, "FAILED, NOT AS PINNED") || !strings.Contains(out, "fetch changed total a=5 b=7") {
			t.Fatalf("run %d: the pinned expectation still fails the same way, but the pinned step returns another total: %v\n%s", i+1, err, out)
		}
		if !strings.Contains(out, "re-pin") {
			t.Fatalf("the note says how to accept the change:\n%s", out)
		}
		if strings.Contains(out, "NEW FAILURE") || !strings.Contains(err.Error(), "a pinned step now returns something else: fetch changed total a=5 b=7") {
			t.Fatalf("a pinned step whose pins still fail as pinned is not a new failure outside the pinned defect: %v\n%s", err, out)
		}
	}
	writeFile(t, filepath.Join(".shrt", "chains", "red.yaml"), `name: red
kept_red:
    - {step: fetch, path: name, got: gadget}
steps:
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-1}
      expect:
          - {path: name, equals: widget}
`)
	if out, err = run(); err != nil || !strings.Contains(out, "FAILED AS PINNED") {
		t.Fatalf("a re-pinned chain file starts a new reference: %v\n%s", err, out)
	}
}

func TestKeptRedPinsRecordAStableGot(t *testing.T) {
	c := &chain.Chain{Name: "red", KeptRed: []chain.Pin{{Step: "list", Path: "orders.1"}}, Steps: []*chain.Step{{ID: "create"}, {ID: "list"}}}
	rec := &runner.Record{RunID: "r1", Vars: map[string]any{"tag": "t42"}, Steps: []*runner.StepRecord{
		{ID: "create", Status: runner.StatusPassed, Response: []byte(`{"order":{"id_order":"ord-ba9876543210"}}`)},
		{ID: "list", Status: runner.StatusFailed, Expect: []chain.ExpectResult{
			{Path: "orders.1", Rule: "exists", Want: false, Got: true},
			{Path: "orders.0.status", Rule: "equals", Want: "PENDING", Got: "CANCELLED"},
			{Path: "orders.0.id_order", Rule: "equals", Want: "ord-0123456789ab", Got: "ord-ba9876543210"},
			{Path: "orders.0.name", Rule: "equals", Want: "Widget", Got: "Widget t42"},
			{Path: "orders.0.total_minor", Rule: "equals", Want: 1250.0, Got: 500.0},
			{Path: "orders.0.created_at", Rule: "equals", Want: "2026-01-01T00:00:00Z", Got: "2026-01-02T00:00:00Z"},
			{Path: "status.details.0.reason", Rule: "equals", Want: "Cancelled"},
		}}}}
	pins, err := failurePins(c, rec, "list")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range pins {
		if p.Got != nil {
			got[p.Path] = *p.Got
		} else {
			got[p.Path] = "<none>"
		}
	}
	want := map[string]string{"orders.0.status": "CANCELLED", "orders.0.id_order": "${create.order.id_order}", "orders.0.name": "Widget ${vars.tag}",
		"orders.0.total_minor": "500", "orders.0.created_at": "<none>", "status.details.0.reason": ""}
	for path, w := range want {
		if got[path] != w {
			t.Errorf("%s: pinned got=%s, want %s (all: %v)", path, got[path], w, got)
		}
	}
	if c.KeptRed[0].Got == nil || *c.KeptRed[0].Got != "true" {
		t.Errorf("re-pinning an old pin with no got records the got it failed with: %+v", c.KeptRed[0])
	}
}

func TestAKeptRedRunReportsLatencyAgainstTheLastRunThatFailedAsPinned(t *testing.T) {
	var delay atomic.Int64
	srv := newSlowFetchBackend(&delay)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, filepath.Join(".shrt", "chains", "red.yaml"), `name: red
kept_red:
    - {step: fetch, path: name, got: widget}
steps:
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-1}
      expect:
          - {path: name, equals: gadget}
`)
	cfg, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/config.yaml", string(cfg)+"latency:\n    floor_ms: 100\n")
	ctx := context.Background()
	if err := runRun(ctx, []string{"red", "-quiet"}); err != nil {
		t.Fatalf("baseline run: %v", err)
	}
	delay.Store(150)
	var rerr error
	out := captureStdout(t, func() { rerr = runRun(ctx, []string{"red", "-quiet"}) })
	if rerr != nil {
		t.Fatalf("without latency.fail a slowdown is a warning: %v\n%s", rerr, out)
	}
	if !strings.Contains(out, "LATENCY: Fetch at step fetch") || !strings.Contains(out, "(the last run that failed as pinned)") {
		t.Fatalf("a kept-red chain has no safe spot, so latency is measured against its last as-pinned run:\n%s", out)
	}
	writeFile(t, ".shrt/config.yaml", string(cfg)+"latency:\n    floor_ms: 100\n    fail: true\n")
	out = captureStdout(t, func() { rerr = runRun(ctx, []string{"red", "-quiet"}) })
	if rerr == nil || !strings.Contains(rerr.Error(), "latency regression in red") {
		t.Fatalf("with latency.fail a confirmed slowdown fails the kept-red run: %v\n%s", rerr, out)
	}
}
