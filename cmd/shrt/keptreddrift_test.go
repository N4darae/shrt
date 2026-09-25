package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	c := &chain.Chain{Name: "red", KeptRed: []chain.Pin{{Step: "list", Path: "orders.1"}}}
	rec := &runner.Record{RunID: "r1", Vars: map[string]any{"tag": "t42"}, Steps: []*runner.StepRecord{{ID: "list", Status: runner.StatusFailed, Expect: []chain.ExpectResult{
		{Path: "orders.1", Rule: "exists", Want: false, Got: true},
		{Path: "orders.0.status", Rule: "equals", Want: "PENDING", Got: "CANCELLED"},
		{Path: "orders.0.id_order", Rule: "equals", Want: "ord-0123456789ab", Got: "ord-ba9876543210"},
		{Path: "orders.0.name", Rule: "equals", Want: "Widget", Got: "Widget t42"},
		{Path: "orders.0.total_minor", Rule: "equals", Want: 1250.0, Got: 500.0},
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
	want := map[string]string{"orders.0.status": "CANCELLED", "orders.0.id_order": "<none>", "orders.0.name": "<none>", "orders.0.total_minor": "500"}
	for path, w := range want {
		if got[path] != w {
			t.Errorf("%s: pinned got=%s, want %s (all: %v)", path, got[path], w, got)
		}
	}
	if c.KeptRed[0].Got == nil || *c.KeptRed[0].Got != "true" {
		t.Errorf("re-pinning an old pin with no got records the got it failed with: %+v", c.KeptRed[0])
	}
}
