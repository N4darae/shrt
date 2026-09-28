package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/hollow"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestFixtureNamesAreVarsThatIsolateTheRun(t *testing.T) {
	c := &chain.Chain{Name: "fx", Vars: map[string]any{"tag": "t", "qty": 1, "both": "b", "h": "x"}, Steps: []*chain.Step{
		{ID: "cp", Call: "CreateProduct", Body: map[string]any{"sku": "fx-${vars.tag}", "name": "Widget ${vars.both}", "qty": "${vars.qty}"}},
		{ID: "find", Call: "S/Find", Body: map[string]any{"sku": "${vars.both}"}},
		{ID: "add", Call: "AddStock", Body: map[string]any{"id_product": "${cp.product.id_product}", "qty": "${vars.q}0"}},
		{ID: "get", Call: "GetCustomer", Body: map[string]any{"id_customer": "cus-${vars.n}"}},
		{ID: "order", Call: "CreateOrder", Body: map[string]any{"idempotency_key": "k1-${vars.tag}", "lines": []any{map[string]any{"note": "for ${vars.q}0"}}}},
		{ID: "list", Call: "ThingService/Fetch", Headers: map[string]string{"X-Tag": "t-${vars.h}"}, Body: map[string]any{"id": "thing-1"}},
	}}
	path := fixtureRequestPath(c)
	for _, tc := range []struct {
		step, path string
		want       bool
	}{
		{"cp", "sku", true}, {"order", "idempotency_key", true}, {"list", "headers.X-Tag", true},
		{"add", "qty", false}, {"get", "id_customer", false}, {"order", "lines.0.note", false},
	} {
		if got := path(tc.step, tc.path); got != tc.want {
			t.Errorf("fixtureRequestPath %s %s = %v, want %v", tc.step, tc.path, got, tc.want)
		}
	}
	only := fixtureOnlyVar(c)
	for name, want := range map[string]bool{"tag": true, "h": true, "qty": false, "both": false, "missing": false} {
		if got := only(name); got != want {
			t.Errorf("fixtureOnlyVar(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestTheTokenLifetimeNamesAnEarlierRunsStepAsThatRunRecordedIt(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	read := func(id string, i int) *runner.StepRecord {
		return &runner.StepRecord{Index: i, ID: id, Call: "ThingService/Fetch", Status: runner.StatusPassed}
	}
	start := time.Date(2026, 9, 27, 17, 0, 0, 0, time.UTC)
	was := &runner.Record{Chain: "cli-thing-flow", RunID: "20260927T170000Z-aaaaaaaa", StartedAt: start, Status: runner.StatusPassed,
		Steps: []*runner.StepRecord{read("read_10s", 1), read("read_25s", 2)}}
	if _, err := e.store.SaveRun(was); err != nil {
		t.Fatal(err)
	}
	now := &runner.Record{Chain: "cli-thing-flow", RunID: "20260927T171000Z-bbbbbbbb", StartedAt: start.Add(10 * time.Minute),
		Steps: []*runner.StepRecord{read("read_0", 1), read("read_1", 2), read("read_2", 3)}}
	if prev := previousRecord(e, now); prev == nil || prev.Steps[1].ID != "read_25s" {
		t.Fatalf("the earlier run's step keeps its recorded name, got %+v", prev)
	}
}

func TestHollowGateSaysWhatRecordsItCounted(t *testing.T) {
	line := recordsCounted(&hollow.Report{Records: 37, ReplayRecords: 9, KeptRedRecords: 10, FailedRecords: 15, FailedKeptRedRecords: 10})
	for _, want := range []string{"28 shrt run and 9 verify replay(s)", "22 passed and 15 did not pass", "of chains kept red: 0 passed, 10 did not pass"} {
		if !strings.Contains(line, want) || strings.Contains(line, "10 of chains kept red, 15 that did not pass") {
			t.Errorf("each split adds up to the record count and kept red is a named part of it: want %q in %s", want, line)
		}
	}
	baseline := filepath.Join(t.TempDir(), "hollow-baseline")
	writeFile(t, baseline, "0\n")
	rep := &hollow.Report{Records: 19, ReplayRecords: 7, KeptRedRecords: 4, FailedRecords: 5, FailedKeptRedRecords: 3}
	var err error
	out := captureStdout(t, func() { err = hollowGate(rep, baseline, ".shrt/hollow-allow", nil) })
	for _, want := range []string{"from 19 run record(s)", "12 shrt run", "7 verify replay", "14 passed and 5 did not pass", "of chains kept red: 1 passed, 3 did not pass"} {
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("want %q in: %v\n%s", want, err, out)
		}
	}
}

func TestChainHollowTellsOrphansFromRenamesAndScratch(t *testing.T) {
	renameThingFlow(t, nil)
	if out, err := confirmRename(t, "-by", "bob@example.test"); err != nil {
		t.Fatalf("rename: %v\n%s", err, out)
	}
	if _, err := fixCmd(t, "run", "cli-renamed", "-quiet"); err != nil {
		t.Fatalf("run: %v", err)
	}
	writeFile(t, ".shrt/scratch/probe.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-renamed.yaml")), "name: cli-renamed", "name: probe", 1))
	if _, err := fixCmd(t, "run", ".shrt/scratch/probe.yaml", "-quiet"); err != nil {
		t.Fatalf("shrt run by path: %v", err)
	}
	out := captureStdout(t, func() { _ = chainHollow(nil) })
	if !strings.Contains(out, "orphan  cli-thing-flow  (renamed to cli-renamed") || strings.Contains(out, "Deleting a chain leaves its runs behind") ||
		!strings.Contains(out, "Renaming a chain leaves its runs under the old name") {
		t.Fatalf("a renamed orphan names its new chain and gets rename wording only:\n%s", out)
	}
	if strings.Contains(out, "orphan  probe") || !strings.Contains(out, "scratch probe") {
		t.Fatalf("probe was run by path and its file exists, so its runs are scratch runs, not orphans:\n%s", out)
	}
	writeFile(t, ".shrt/runs/gone/20260101T000000Z-deadbeef.json", "{}")
	out = captureStdout(t, func() { _ = chainHollow(nil) })
	if !strings.Contains(out, "Deleting a chain leaves its runs behind") || !strings.Contains(out, "Renaming a chain") {
		t.Fatalf("a deleted and a renamed orphan each get their wording:\n%s", out)
	}
}

func fixSessionWorkspace(t *testing.T, life time.Duration, steps string) {
	t.Helper()
	var mu sync.Mutex
	issued := map[string]time.Time{}
	logins := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/Login") {
			logins++
			token := fmt.Sprintf("tok-%d", logins)
			issued[token] = time.Now()
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "access_token": token,
				"expires_at": fmt.Sprint(time.Now().Add(time.Hour).Unix())})
			return
		}
		at, known := issued[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !known || life > 0 && time.Since(at) > life {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "unauthenticated", "message": "invalid or expired token"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "widget"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	t.Setenv("LIFE_USER", "staff")
	t.Setenv("LIFE_PASSWORD", "secret")
	writeFile(t, ".shrt/config.yaml", `target:
    base_url: `+srv.URL+`
descriptor:
    file: .shrt/descriptor.binpb
auth:
    call: shrt.test.v1.AuthService/Login
    body:
        username: ${env.LIFE_USER}
        password: ${env.LIFE_PASSWORD}
    token_path: access_token
    expires_path: expires_at
paths:
    chains: .shrt/chains
    runs: .shrt/runs
    safespots: .shrt/safespots
`)
	writeFile(t, filepath.Join(".shrt", "chains", "cli-thing-flow.yaml"), "apiVersion: shrt/v1\nname: cli-thing-flow\nsteps:\n"+steps)
}

func fixHeldFetches(wait string) string {
	fetch := func(id, wait string) string {
		s := "    - id: " + id + "\n      call: ThingService/Fetch\n"
		if wait != "" {
			s += "      wait: " + wait + "\n"
		}
		return s + "      body: {id: thing-1}\n      expect: [{path: error.code, equals: OK}, {path: name, equals: widget}]\n"
	}
	return fetch("fetch", "") + fetch("fetch_held", wait) + fetch("fetch_held_again", wait)
}

func TestAStepWait(t *testing.T) {
	t.Run("in one short chain proves a backend ends sessions early", func(t *testing.T) {
		fixSessionWorkspace(t, 200*time.Millisecond, fixHeldFetches("400ms"))
		out, err := fixCmd(t, "run", "cli-thing-flow")
		var coded *exitError
		if err == nil || errors.As(err, &coded) || !strings.Contains(out, "FINDING: token refused 0s after issue") ||
			!strings.Contains(out, "the fresh token the re-login issued was refused") || !strings.Contains(out, "sent after waiting") {
			t.Fatalf("two sessions ended long before their stated expiry in one run are a finding, exit 1: %v\n%s", err, out)
		}
	})
	t.Run("against a backend that keeps sessions passes and is not latency", func(t *testing.T) {
		fixSessionWorkspace(t, 0, fixHeldFetches("150ms"))
		out, err := fixCmd(t, "run", "cli-thing-flow", "-quiet")
		if err != nil || strings.Contains(out, "WARNING") || strings.Contains(out, "FINDING") {
			t.Fatalf("a backend that keeps its sessions passes the held chain: %v\n%s", err, out)
		}
		rec, err := store.New(".shrt/runs", ".shrt/safespots").LatestRun("cli-thing-flow")
		if err != nil {
			t.Fatal(err)
		}
		if held, _ := rec.Step("fetch_held"); held.WaitedMS < 150 || held.LatencyMS >= 150 {
			t.Fatalf("the record keeps the wait apart from the call's latency: waited %dms, latency %dms", held.WaitedMS, held.LatencyMS)
		}
	})
	t.Run("a dry run does not wait", func(t *testing.T) {
		fixSessionWorkspace(t, 0, fixHeldFetches("10m"))
		start := time.Now()
		if _, err := fixCmd(t, "run", "cli-thing-flow", "-dry-run", "-quiet"); err != nil || time.Since(start) > 30*time.Second {
			t.Fatalf("a dry run sends nothing, so it waits for nothing: %v after %s", err, time.Since(start))
		}
	})
	t.Run("outside its bounds does not load", func(t *testing.T) {
		for _, wait := range []string{"11m", "0s", "-5s", "soon", "25", "10m"} {
			c := &chain.Chain{Name: "w", Steps: []*chain.Step{{ID: "s", Call: "ThingService/Fetch", Wait: wait}}}
			err := c.Normalize()
			if wait == "10m" && err != nil || wait != "10m" && (err == nil || !strings.Contains(err.Error(), "wait")) {
				t.Fatalf("wait %q: only 10m and below load, a refusal names the key: %v", wait, err)
			}
		}
	})
}
