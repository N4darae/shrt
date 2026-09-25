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

const sameCallChain = `apiVersion: shrt/v1
name: pair-flow
steps:
    - id: c1
      call: ThingService/Create
      body:
          name: alpha
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: c2
      call: ThingService/Create
      body:
          name: beta
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: f1
      call: ThingService/Fetch
      body:
          id: ${c1.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: alpha
    - id: f2
      call: ThingService/Fetch
      body:
          id: ${c2.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: beta
`

func newSameCallBackend(total *int) *httptest.Server {
	next := 0
	names := map[string]any{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			id := "thing-" + itoa(next)
			names[id] = body["name"]
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": id})
		case "/shrt.test.v1.ThingService/Fetch":
			id, _ := body["id"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": id, "name": names[id], "total": *total})
		default:
			w.WriteHeader(404)
		}
	}))
}

func confirmSameCallChain(t *testing.T, total *int) {
	t.Helper()
	srv := newSameCallBackend(total)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/pair-flow.yaml", sameCallChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"pair-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"pair-flow", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"pair-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
}

func editPairFlow(t *testing.T, pairs ...string) {
	t.Helper()
	raw, err := os.ReadFile(".shrt/chains/pair-flow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/chains/pair-flow.yaml", strings.NewReplacer(pairs...).Replace(string(raw)))
}

func TestRenamingTwoStepsThatShareACallIsARename(t *testing.T) {
	total := 750
	confirmSameCallChain(t, &total)
	editPairFlow(t, "- id: f1\n", "- id: fa\n", "- id: f2\n", "- id: fb\n")
	ctx := context.Background()

	var verr error
	out := captureStdout(t, func() { verr = runVerify(ctx, []string{"pair-flow", "-quiet"}) })
	if verr != nil {
		t.Fatalf("only the names of two Fetch steps changed, at the same positions: want no drift, got %v\n%s", verr, out)
	}
	if !strings.Contains(out, "f1 -> fa") || !strings.Contains(out, "f2 -> fb") {
		t.Errorf("verify names both renames:\n%s", out)
	}

	total = 751
	out = captureStdout(t, func() { verr = runVerify(ctx, []string{"pair-flow", "-quiet"}) })
	if code := exitCodeOf(verr); code != 1 || !strings.Contains(verr.Error(), "regression") {
		t.Fatalf("total changed 750 -> 751 at both renamed steps: want a regression, got %d: %v\n%s", code, verr, out)
	}
	for _, not := range []string{"missing", "unexpected step", "chain change"} {
		if strings.Contains(out, not) {
			t.Errorf("a rename of two same-call steps is not a missing and an unexpected step (%q):\n%s", not, out)
		}
	}
	if !strings.Contains(out, "[fa] changed    total want=750 got=751") {
		t.Errorf("the renamed step's change is shown under its new name:\n%s", out)
	}
}

func TestSwappingTheIDsOfTwoSameCallStepsIsTwoRenames(t *testing.T) {
	total := 750
	confirmSameCallChain(t, &total)
	editPairFlow(t, "- id: c1\n", "- id: c2\n", "- id: c2\n", "- id: c1\n", "${c1.id}", "${c2.id}", "${c2.id}", "${c1.id}")
	ctx := context.Background()

	var verr error
	out := captureStdout(t, func() { verr = runVerify(ctx, []string{"pair-flow", "-quiet"}) })
	if verr != nil {
		t.Fatalf("the ids of two Create steps were swapped with their bodies in place: want no drift, got %v\n%s", verr, out)
	}
	for _, not := range []string{"moved", "regression:", "not renamed consistently"} {
		if strings.Contains(out, not) {
			t.Errorf("a swap of two same-call step ids is not a move (%q):\n%s", not, out)
		}
	}
	if !strings.Contains(out, "c1 -> c2") || !strings.Contains(out, "c2 -> c1") {
		t.Errorf("verify names the swap as two renames:\n%s", out)
	}
}

func TestAStepOrderMoveIsAChainChangeEvenBesideAnExpectationEdit(t *testing.T) {
	total := 750
	confirmSameCallChain(t, &total)
	raw, err := os.ReadFile(".shrt/chains/pair-flow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	c2Start := strings.Index(s, "    - id: c2\n")
	f1Start := strings.Index(s, "    - id: f1\n")
	f2Start := strings.Index(s, "    - id: f2\n")
	moved := s[:c2Start] + s[f1Start:f2Start] + s[c2Start:f1Start] + s[f2Start:]
	moved = strings.Replace(moved, "            equals: beta\n", "            equals: beta\n          - path: id\n            not_empty: true\n", 1)
	writeFile(t, ".shrt/chains/pair-flow.yaml", moved)

	var verr error
	out := captureStdout(t, func() { verr = runVerify(context.Background(), []string{"pair-flow", "-quiet"}) })
	if verr == nil || strings.Contains(verr.Error(), "regression") {
		t.Fatalf("c2 moved after f1 and an expectation was added: want drift after a chain change, got %v\n%s", verr, out)
	}
	if !strings.Contains(verr.Error(), "chain change") {
		t.Errorf("the move is a chain change: %v\n%s", verr, out)
	}
}
