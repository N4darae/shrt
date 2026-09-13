package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func chdirToFreshCLIWorkspace(t *testing.T, baseURL string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".shrt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".shrt", "descriptor.binpb"), catalogtest.Descriptor(), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".shrt", "config.yaml"), `target:
    base_url: `+baseURL+`
descriptor:
    file: .shrt/descriptor.binpb
paths:
    chains: .shrt/chains
    runs: .shrt/runs
    safespots: .shrt/safespots
`)
	writeFile(t, filepath.Join(dir, ".shrt", "chains", "cli-thing-flow.yaml"), `apiVersion: shrt/v1
name: cli-thing-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
          - path: id
            not_empty: true
      export:
          thing_id: id
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: widget
`)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
}

func newFakeCLIBackend() *httptest.Server {
	nextID := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			nextID++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "OK"},
				"id":    "thing-" + itoa(nextID),
			})
		case "/shrt.test.v1.ThingService/Fetch":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "OK"},
				"id":    body["id"],
				"name":  "widget",
			})
		default:
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "unimplemented", "message": r.URL.Path})
		}
	}))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestCLIRunExecutesAChainAgainstAFakeBackendAndSavesARecord(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)

	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}

	entries, err := os.ReadDir(".shrt/runs/cli-thing-flow")
	if err != nil {
		t.Fatalf("expected a saved run record directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one run record, got %d", len(entries))
	}
	raw, err := os.ReadFile(filepath.Join(".shrt/runs/cli-thing-flow", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec["status"] != "passed" {
		t.Fatalf("saved record status = %v, want passed", rec["status"])
	}
}

func TestCLIRunDryRunSendsNothing(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1"})
	}))
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)

	if err := runRun(context.Background(), []string{"cli-thing-flow", "-dry-run", "-quiet"}); err != nil {
		t.Fatalf("shrt run -dry-run: %v", err)
	}
	if calls != 0 {
		t.Fatalf("a dry run must send nothing, the fake backend saw %d call(s)", calls)
	}
}

func TestCLIInitBootstrapsAFreshDirectoryWithNoBackendNeeded(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	if err := runInit(context.Background(), []string{"-build=false"}); err != nil {
		t.Fatalf("shrt init: %v", err)
	}

	for _, want := range []string{
		".shrt/config.yaml",
		".shrt/docs/README.md",
		".shrt/docs/GRAMMAR.md",
		".shrt/docs/PLAYBOOK.md",
		".shrt/docs/PITFALLS.md",
		".claude/skills/shrt/SKILL.md",
		".claude/agents/shrt-contract-author.md",
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("shrt init did not create %s: %v", want, err)
		}
	}
}

func TestCLIConfirmRefusesWithoutExplicitHumanAcknowledgement(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)

	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}

	if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-by", "alice"}); err == nil {
		t.Fatal("confirm without -i-verified must be refused")
	}
	if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-i-verified"}); err == nil {
		t.Fatal("confirm without -by must be refused")
	}
	if _, err := os.Stat(".shrt/safespots/cli-thing-flow.json"); err == nil {
		t.Fatal("a refused confirm must not have written a safe spot")
	}

	if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-by", "alice", "-i-verified"}); err != nil {
		t.Fatalf("a properly acknowledged confirm must succeed: %v", err)
	}
	if _, err := os.Stat(".shrt/safespots/cli-thing-flow.json"); err != nil {
		t.Fatalf("expected a safe spot file after a successful confirm: %v", err)
	}

	if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-by", "bob", "-i-verified"}); err == nil {
		t.Fatal("confirm must refuse to silently overwrite an existing safe spot without -supersede")
	}
}

func TestCLIVerifyReportsDriftAgainstAHandWrittenSafeSpot(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)

	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	entries, err := os.ReadDir(".shrt/runs/cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(".shrt/runs/cli-thing-flow", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	spot := map[string]any{
		"chain":        "cli-thing-flow",
		"run_id":       rec["run_id"],
		"target":       rec["target"],
		"confirmed_by": "test",
		"confirmed_at": "2026-01-01T00:00:00Z",
		"digest":       "d",
		"steps":        rec["steps"],
	}
	spotRaw, err := json.Marshal(spot)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/safespots/cli-thing-flow.json", string(spotRaw))

	if err := runVerify(context.Background(), []string{"cli-thing-flow", "-run", rec["run_id"].(string), "-quiet"}); err != nil {
		t.Fatalf("a fresh run replayed against its own safe spot must diff clean: %v", err)
	}
}
