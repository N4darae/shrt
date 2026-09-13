package coredistillation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

const chainName = "library-mode"

const chainYAML = `apiVersion: shrt/v1
name: library-mode
description: create a thing and read it back, the smallest chain that proves the loop
vars:
  label: widget
steps:
  - id: create
    call: shrt.test.v1.ThingService/Create
    body:
      name: ${vars.label}
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
    call: shrt.test.v1.ThingService/Fetch
    body:
      id: ${exports.thing_id}
    expect:
      - path: error.code
        equals: OK
      - path: id
        equals: ${exports.thing_id}
`

type backend struct {
	*httptest.Server
	created int
	name    string
}

func newBackend() *backend {
	b := &backend{name: "widget"}
	b.Server = httptest.NewServer(http.HandlerFunc(b.handle))
	return b
}

func (b *backend) handle(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ok := map[string]any{"code": "OK", "message": ""}
	reply := func(v map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	switch r.URL.Path {
	case "/shrt.test.v1.AuthService/Login":
		reply(map[string]any{"error": ok, "accessToken": "token-1", "expiresAt": "0"})
	case "/shrt.test.v1.ThingService/Create":
		if r.Header.Get("Authorization") != "Bearer token-1" {
			w.WriteHeader(http.StatusUnauthorized)
			reply(map[string]any{"code": "unauthenticated", "message": "no token"})
			return
		}
		b.created++
		reply(map[string]any{"error": ok, "id": fmt.Sprintf("thing-%v", body["name"])})
	case "/shrt.test.v1.ThingService/Fetch":
		reply(map[string]any{
			"error":     ok,
			"id":        body["id"],
			"name":      b.name,
			"createdAt": time.Now().UTC().Format(time.RFC3339Nano),
		})
	default:
		w.WriteHeader(http.StatusNotFound)
		reply(map[string]any{"code": "unimplemented", "message": r.URL.Path})
	}
}

func adopt(t *testing.T, baseURL string) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := config.Default()
	cfg.Root = root
	cfg.Target.BaseURL = baseURL
	cfg.Descriptor.Source = ""
	cfg.Volatile = []string{"**.created_at"}
	cfg.Auth = &config.Auth{
		Call:      "shrt.test.v1.AuthService/Login",
		Body:      map[string]any{"username": "${env.SHRT_TEST_USER}", "password": "${env.SHRT_TEST_PASSWORD}"},
		TokenPath: "access_token",
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Abs(cfg.Descriptor.File), catalogtest.Descriptor(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.Abs(cfg.Paths.Chains), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Abs(cfg.Paths.Chains), chainName+".yaml"), []byte(chainYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHRT_TEST_USER", "someone")
	t.Setenv("SHRT_TEST_PASSWORD", "secret")
	loaded, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func record(t *testing.T, cfg *config.Config) (*runner.Record, *store.Store) {
	t.Helper()
	cat, err := catalog.Load(cfg.Abs(cfg.Descriptor.File))
	if err != nil {
		t.Fatal(err)
	}
	c, err := chain.Resolve(cfg.Abs(cfg.Paths.Chains), chainName)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r, opts, err := runner.NewFromConfig(ctx, cfg, cat)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := r.Run(ctx, c, opts)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(cfg.Abs(cfg.Paths.Runs), cfg.Abs(cfg.Paths.SafeSpots))
	if _, err := st.SaveRun(rec); err != nil {
		t.Fatal(err)
	}
	return rec, st
}

func confirmed(rec *runner.Record, volatile []string) *store.SafeSpot {
	return &store.SafeSpot{
		Chain: rec.Chain, RunID: rec.RunID, ConfirmedBy: "fixture",
		ConfirmedAt: time.Unix(1790035200, 0).UTC(), Volatile: volatile, Steps: clone(rec.Steps),
	}
}

func clone(steps []*runner.StepRecord) []*runner.StepRecord {
	raw, err := json.Marshal(steps)
	if err != nil {
		panic(err)
	}
	var out []*runner.StepRecord
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

func TestLibraryModeDrivesTheWholeLoopWithoutTheCLIOrThisRepository(t *testing.T) {
	b := newBackend()
	defer b.Close()
	cfg := adopt(t, b.URL)

	rec, st := record(t, cfg)

	if !rec.Passed() {
		t.Fatalf("chain %s: %s\n%s", chainName, rec.Status, rec.Failure)
	}
	if len(rec.Steps) != 2 {
		t.Fatalf("want both steps recorded, got %d", len(rec.Steps))
	}
	if b.created != 1 {
		t.Errorf("the create step has to have reached the backend exactly once, got %d", b.created)
	}
	runs, err := st.ListRuns(chainName)
	if err != nil || len(runs) != 1 {
		t.Fatalf("the run record is the receipt, and it has to survive on disk: %v %v", runs, err)
	}
}

func TestLibraryModeCarriesTheTokenTheLoginReturned(t *testing.T) {
	b := newBackend()
	defer b.Close()

	rec, _ := record(t, adopt(t, b.URL))

	if !rec.Passed() {
		t.Fatalf("the create step rejects anything but the minted token, so a failure here means the "+
			"auth middleware never attached it: %s\n%s", rec.Status, rec.Failure)
	}
}

func TestLibraryModeReplayDiffsCleanAgainstAConfirmedRun(t *testing.T) {
	b := newBackend()
	defer b.Close()
	cfg := adopt(t, b.URL)
	first, _ := record(t, cfg)
	spot := confirmed(first, cfg.Volatile)

	report := diff.Compare(spot, first)

	if !report.Clean() {
		t.Fatalf("a run diffed against itself must be clean:\n%s", report.Text())
	}
}

func TestLibraryModeReportsADriftedFieldTheAssertionsNeverMentioned(t *testing.T) {
	b := newBackend()
	defer b.Close()
	cfg := adopt(t, b.URL)
	first, _ := record(t, cfg)
	spot := confirmed(first, cfg.Volatile)

	b.name = "widget mk2"
	second, _ := record(t, cfg)

	if !second.Passed() {
		t.Fatalf("no assertion in this chain mentions name, so the second run has to stay green -- "+
			"that is the premise: %s\n%s", second.Status, second.Failure)
	}
	report := diff.Compare(spot, second)
	if report.Clean() {
		t.Fatalf("name changed between the confirmed run and this one, and verify is the check that "+
			"catches what nobody asserted:\n%s", report.Text())
	}
	if !strings.Contains(report.Text(), "widget mk2") {
		t.Errorf("the diff has to print the value that moved: %s", report.Text())
	}
}

func TestLibraryModeTreatsAVolatileFieldAsNoise(t *testing.T) {
	b := newBackend()
	defer b.Close()
	cfg := adopt(t, b.URL)
	first, _ := record(t, cfg)
	spot := confirmed(first, cfg.Volatile)

	time.Sleep(2 * time.Millisecond)
	second, _ := record(t, cfg)

	if report := diff.Compare(spot, second); !report.Clean() {
		t.Fatalf("created_at is declared volatile, and a timestamp that moves on every call would "+
			"otherwise make every replay dirty:\n%s", report.Text())
	}
}

func TestLibraryModeDryRunResolvesEverythingAndSendsNothing(t *testing.T) {
	b := newBackend()
	defer b.Close()
	cfg := adopt(t, b.URL)
	cat, err := catalog.Load(cfg.Abs(cfg.Descriptor.File))
	if err != nil {
		t.Fatal(err)
	}
	c, err := chain.Resolve(cfg.Abs(cfg.Paths.Chains), chainName)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r, opts, err := runner.NewFromConfig(ctx, cfg, cat)
	if err != nil {
		t.Fatal(err)
	}
	opts.DryRun = true

	rec, err := r.Run(ctx, c, opts)
	if err != nil {
		t.Fatal(err)
	}

	if rec.Status == runner.StatusFailed || rec.Status == runner.StatusError {
		t.Fatalf("every reference in this chain resolves, so the dry run must not fail: %s\n%s",
			rec.Status, rec.Failure)
	}
	if b.created != 0 {
		t.Errorf("a dry run that reaches the backend is not a dry run: %d create(s) arrived", b.created)
	}
}
