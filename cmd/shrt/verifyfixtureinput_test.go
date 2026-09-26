package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fixtureFlowChain = `apiVersion: shrt/v1
name: cli-fixture-flow
vars:
    tag: first
    trace: t-1
    kind: KIND_A
    total: 300
volatile:
    - '**.trace_id'
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget ${vars.tag}
          kind: ${vars.kind}
          meta:
              trace_id: ${vars.trace}
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: total
            equals: ${vars.total}
`

func newTotalBackend(regressed *bool) *httptest.Server {
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			total := 300
			if body["kind"] == "KIND_B" {
				total = 500
			}
			if *regressed {
				total = 999
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": body["name"], "total": total})
		case "/shrt.test.v1.ThingService/Fetch":
			total := 300
			if *regressed {
				total = 999
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "total": total})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestVerifyDoesNotExcuseARegressionByAFreshTagOrAnExpectationVar(t *testing.T) {
	regressed := false
	srv := newTotalBackend(&regressed)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-fixture-flow.yaml", fixtureFlowChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-fixture-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-fixture-flow", "-note", "total 300"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-fixture-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	verify := func(args ...string) string {
		t.Helper()
		var err error
		out := captureStdout(t, func() { err = runVerify(ctx, append([]string{"cli-fixture-flow", "-quiet"}, args...)) })
		if err != nil {
			out += "\nERR: " + err.Error()
		}
		return out
	}
	if out := verify("-var", "tag=second", "-var", "trace=t-2"); !strings.Contains(out, "no drift") || strings.Contains(out, "ERR:") {
		t.Fatalf("a fresh tag and a volatile trace id are not a drift on a healthy backend:\n%s", out)
	}
	regressed = true
	for _, args := range [][]string{
		{"-var", "tag=third", "-var", "trace=t-3"},
		{"-var", "total=999"},
		{"-var", "tag=fourth", "-var", "total=999"},
	} {
		out := verify(args...)
		if strings.Contains(out, "with different input") || !strings.Contains(out, "ERR: regression") {
			t.Errorf("%v: only fixture names, a volatile field or an expectation var differ, so the total change is a regression:\n%s", args, out)
		}
		if !strings.Contains(out, "total") {
			t.Errorf("%v: the regression names the changed path:\n%s", args, out)
		}
	}
	regressed = false
	out := verify("-var", "kind=KIND_B", "-var", "tag=fifth")
	if !strings.Contains(out, "ERR: drift with different input") || !strings.Contains(out, "kind=KIND_B") {
		t.Errorf("a changed kind is genuinely different input and explains the total change:\n%s", out)
	}
	if strings.Contains(out, "tag=fifth") {
		t.Errorf("the tag only names fixtures, so it is not blamed for the drift:\n%s", out)
	}
}

func TestVerifyCountsFixtureNameInputsAndListsThemUnderMasked(t *testing.T) {
	regressed := false
	srv := newTotalBackend(&regressed)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-fixture-flow.yaml", fixtureFlowChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-fixture-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-fixture-flow", "-note", "total 300"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-fixture-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	regressed = true
	for _, flags := range [][]string{nil, {"-v"}} {
		out := captureStdout(t, func() { _ = runVerify(ctx, append([]string{"cli-fixture-flow", "-var", "tag=second"}, flags...)) })
		if !strings.Contains(out, "request value(s) differ from the confirmed run only in a fixture name") || !strings.Contains(out, "(-masked lists them)") ||
			strings.Contains(out, "create name") {
			t.Errorf("%v: the fixture-name inputs are counted, not listed:\n%s", flags, out)
		}
	}
	out := captureStdout(t, func() { _ = runVerify(ctx, []string{"cli-fixture-flow", "-var", "tag=third", "-masked"}) })
	if !strings.Contains(out, "not counted as different input: create name") {
		t.Errorf("-masked lists each fixture-name input:\n%s", out)
	}
}
