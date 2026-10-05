package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestAChainWithASafeSpotIsSentOnceByTheGateAndFailsWithTheRunsHeadline(t *testing.T) {
	var mu sync.Mutex
	creates, name := 0, "widget"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			creates++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(creates)})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": name})
		}
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	fixApprove(t, "cli-thing-flow")
	mu.Lock()
	creates, name = 0, "gadget"
	mu.Unlock()
	inProcessGate(t)
	out, code := runGateOut(t, "cli-thing-flow")
	if code != 1 || !strings.Contains(out, "FAIL cli-thing-flow  regression: fetch (Fetch) name want=widget got=gadget; suspect write create (Create)\n") {
		t.Fatalf("a failing expectation still fails the gate with the expectation as its headline, got %d:\n%s", code, out)
	}
	if creates != 1 {
		t.Fatalf("a chain with a safe spot is sent once, by verify, not run and then replayed: %d create call(s)", creates)
	}
}

func TestTheGateAlsoRunsAChainWhenVerifyCouldNotJudgeWhatARunWouldReport(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  gateOutcome
	}{
		{"a finding verify set aside after a drift", gateOutcome{code: 1, side: gateSidecar{RunToo: true}}},
		{"no verdict from verify", gateOutcome{code: 3}},
	} {
		f := gateWorkspace(t, map[string][]gateOutcome{"verify cli-thing-flow": {tc.out}})
		runGateOut(t)
		if f.tries["run cli-thing-flow"] != 1 {
			t.Errorf("%s: the chain is run too, got %v", tc.name, f.tries)
		}
	}
	f := gateWorkspace(t, nil)
	runGateOut(t)
	if f.tries["run cli-thing-flow"] != 0 || f.tries["verify cli-thing-flow"] != 1 {
		t.Errorf("a clean verify is the chain's only execution, got %v", f.tries)
	}
}
