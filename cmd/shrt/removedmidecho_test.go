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

const dupAfterMidChain = `apiVersion: shrt/v1
name: cli-dupmid
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: Widget-${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: mid
      call: ThingService/Fetch
      body:
          id: none
      expect:
          - path: error.code
            equals: OK
    - id: dup
      call: ThingService/Create
      body:
          name: widget-${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: ALREADY_EXISTS
`

func newFoldedUniqueBackend() *httptest.Server {
	seen := map[string]bool{}
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			name, _ := body["name"].(string)
			if seen[strings.ToLower(name)] {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "ALREADY_EXISTS", "message": "name " + name + " already exists"}})
				return
			}
			seen[strings.ToLower(name)] = true
			next++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next)})
		case "/shrt.test.v1.ThingService/Fetch":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestRemovingAMiddleStepKeepsTheFixtureEchoMaskedAtLaterSteps(t *testing.T) {
	srv := newFoldedUniqueBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-dupmid.yaml", dupAfterMidChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-dupmid", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-dupmid", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-dupmid", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	raw, err := os.ReadFile(".shrt/chains/cli-dupmid.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a, b := strings.Index(s, "    - id: mid\n"), strings.Index(s, "    - id: dup\n")
	writeFile(t, ".shrt/chains/cli-dupmid.yaml", s[:a]+s[b:])

	var verr error
	out := captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-dupmid", "-quiet", "-masked", "-var", "tag=fresh2"}) })
	if verr == nil || !strings.Contains(verr.Error(), "1 change(s)") {
		t.Fatalf("only the removed step differs: want one change after a chain change, got %v\n%s", verr, out)
	}
	if listed, _, _ := strings.Cut(out, "values echoing a fixture name, not compared:"); strings.Contains(listed, "error.message") {
		t.Errorf("the refusal message only echoes the fixture name, as it does without the removal:\n%s", out)
	}
	if !strings.Contains(out, "dup name") {
		t.Errorf("the later step's name is listed among the values differing only in a fixture name:\n%s", out)
	}

	out = captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-dupmid", "-quiet", "-var", "tag=fresh3"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-dupmid", "-supersede", "-note", "mid removed"}); err != nil {
			t.Fatalf("supersede: %v", err)
		}
	})
	if listed, _, _ := strings.Cut(out, "values echoing a fixture name, not compared:"); strings.Contains(listed, "error.message") {
		t.Errorf("the supersede review does not carry the echoed message as a difference:\n%s", out)
	}
}
