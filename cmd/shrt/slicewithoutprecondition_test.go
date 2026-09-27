package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type lifecycleBackend struct {
	mu        sync.Mutex
	cancelBug bool
	note      int
}

func (b *lifecycleBackend) serve() *httptest.Server {
	states := map[string]string{}
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		id, _ := body["id"].(string)
		op, _ := body["name"].(string)
		if meta, _ := body["meta"].(map[string]any); meta != nil {
			id, _ = meta["trace_id"].(string)
		}
		w.Header().Set("Content-Type", "application/json")
		out := map[string]any{"error": map[string]any{"code": "OK"}}
		switch {
		case r.URL.Path == "/shrt.test.v1.ThingService/Fetch":
		case r.URL.Path != "/shrt.test.v1.ThingService/Create":
			w.WriteHeader(404)
			return
		case id == "":
			next++
			id = "thing-" + itoa(next)
			states[id] = "PENDING"
		case op == "confirm":
			states[id] = "CONFIRMED"
			out["total"] = b.note
		case op == "cancel" && !(b.cancelBug && states[id] == "CONFIRMED"):
			states[id] = "CANCELLED"
		}
		out["id"], out["name"] = id, states[id]
		_ = json.NewEncoder(w).Encode(out)
	}))
}

const lifecycleChain = `apiVersion: shrt/v1
name: life
steps:
    - id: make
      call: ThingService/Create
      body:
          name: widget
      expect:
          - path: error.code
            equals: OK
    - id: confirm
      call: ThingService/Create
      body:
          name: confirm
          meta:
              trace_id: ${make.id}
      expect:
          - path: name
            equals: CONFIRMED
    - id: fetch_confirmed
      call: ThingService/Fetch
      body:
          id: ${make.id}
      expect:
          - path: name
            equals: CONFIRMED
    - id: cancel
      call: ThingService/Create
      body:
          name: cancel
          meta:
              trace_id: ${make.id}
      expect:
          - path: name
            equals: CANCELLED
`

func lifecycleWorkspace(t *testing.T) *lifecycleBackend {
	t.Helper()
	b := &lifecycleBackend{note: 1}
	srv := b.serve()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/life.yaml", lifecycleChain)
	return b
}

func lifecycleWithout(t *testing.T, b *lifecycleBackend, note int) (string, error) {
	t.Helper()
	b.mu.Lock()
	b.cancelBug, b.note = true, note
	b.mu.Unlock()
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"life", "-quiet", "-keep-going"}); err == nil {
			t.Fatalf("cancel of a confirmed thing must fail")
		}
	})
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(ctx, []string{"life", "-without", "confirm", "-verify"})
	})
	return out, err
}

func TestSliceWithoutVerifyCallsAnUnchangedLeftOutStepAPreconditionAndPointsAtTheFailingStep(t *testing.T) {
	b := lifecycleWorkspace(t)
	out, err := lifecycleWithout(t, b, 1)
	if exitCodeOf(err) != 0 {
		t.Fatalf("fetch_confirmed reads what confirm wrote, so it does not make the verdict inconclusive, got %d: %v\n%s", exitCodeOf(err), err, out)
	}
	for _, want := range []string{
		"1 of 1 step(s) that failed in source run",
		"need it, they pass in the run without it: cancel",
		"confirm passed in source run",
		"so it is a precondition: look first at cancel",
		"fail only without it, as expected: fetch_confirmed read what confirm wrote",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "cause confirmed") || strings.Contains(out, "INCONCLUSIVE") {
		t.Fatalf("confirm answered as always, so removing it proves only that cancel needs it:\n%s", out)
	}
}

func TestSliceWithoutVerifyKeepsCauseConfirmedWhenTheLeftOutStepChangedAgainstTheSafeSpot(t *testing.T) {
	b := lifecycleWorkspace(t)
	approveChain := func() {
		ctx := context.Background()
		captureStdout(t, func() {
			if err := runRun(ctx, []string{"life", "-quiet"}); err != nil {
				t.Fatalf("shrt run: %v", err)
			}
			if err := runConfirm(ctx, []string{"life", "-note", "baseline"}); err != nil {
				t.Fatalf("propose: %v", err)
			}
			if err := runConfirm(ctx, []string{"life", "-approve", "-by", "alice@example.test"}); err != nil {
				t.Fatalf("approve: %v", err)
			}
		})
	}
	approveChain()
	out, _ := lifecycleWithout(t, b, 1)
	if !strings.Contains(out, "confirm answered as in run") {
		t.Fatalf("confirm answered as in the safe spot's run:\n%s", out)
	}
	out, err := lifecycleWithout(t, b, 2)
	if exitCodeOf(err) != 0 || !strings.Contains(out, "cause confirmed for 1 of 1 step(s)") || strings.Contains(out, "precondition") {
		t.Fatalf("confirm's note changed against the safe spot, so it stays the confirmed cause, got %d: %v\n%s", exitCodeOf(err), err, out)
	}
}
