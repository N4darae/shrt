package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func nonEmptyLines(out string) []string {
	lines := []string{}
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func deadCLITarget(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return "http://" + addr
}

func TestRunOutputPieces(t *testing.T) {
	for _, c := range []struct {
		status string
		dry    bool
		want   string
	}{
		{runner.StatusPassed, false, "ok"},
		{runner.StatusFailed, false, "FAIL"},
		{runner.StatusError, false, "ERROR"},
		{runner.StatusError, true, "ERROR"},
		{runner.StatusSkipped, false, "SKIP"},
		{runner.StatusSkipped, true, "--"},
	} {
		line := progressLine(&runner.StepRecord{Index: 1, ID: "create_customer", Call: "X/Y", Status: c.status}, c.dry)
		if got := strings.Fields(line)[0]; got != c.want {
			t.Errorf("status %s dry=%v: progress mark %q, want %q", c.status, c.dry, got, c.want)
		}
	}
	warned := summary(&runner.Record{Chain: "green-with-warnings", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{Index: 1, ID: "login", Status: runner.StatusPassed},
		{Index: 2, ID: "probe", Status: runner.StatusPassed, Warning: "refused in-band (status.code = REJECTED, not SUCCESS)\n       and a second line"},
	}}, false)
	if !strings.Contains(warned, "probe") || !strings.Contains(warned, "a second line") || strings.Contains(warned, "login") {
		t.Errorf("a green run's step warnings reach the summary, and only those steps:\n%s", warned)
	}
	shared := summary(&runner.Record{Chain: "access", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{ID: "admin_create", Status: runner.StatusPassed, Warning: runner.CachedTokenResent},
		{ID: "clerk_create", Status: runner.StatusPassed, Warning: runner.CachedTokenResent},
	}}, false)
	if strings.Count(shared, runner.CachedTokenResent) != 1 || !strings.Contains(shared, "warning [admin_create, clerk_create]: ") {
		t.Errorf("a warning shared by steps is one line naming them all:\n%s", shared)
	}
	if strings.Contains(runner.CachedTokenResent, "\n") || len(runner.CachedTokenResent) > 160 {
		t.Errorf("a restart the run recovered from is one short line: %q", runner.CachedTokenResent)
	}
	why := `not sent: ${create.id} reads step "create", which was refused in-band (status.code = REJECTED): a refused call's response decodes to zero values, and the request would carry them as if they were real. A reference to its request (${steps.create.request...}) is still safe: that is what was sent, so a step reading only that is sent`
	sc := runner.NewSkipCondenser()
	if got := sc.Condense("fetch", why); got != why {
		t.Errorf("the first time a not-sent reason is printed whole, got %q", got)
	}
	if got := sc.Condense("list", strings.Replace(why, "${create.id}", "${create.name}", 1)); strings.Contains(got, "zero values") || !strings.Contains(got, "${create.name}") || !strings.Contains(got, "fetch") {
		t.Errorf("a repeat keeps its own reference and points at the step that gave the reason, got %q", got)
	}
	c := &chain.Chain{}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		c.Steps = append(c.Steps, &chain.Step{ID: id})
	}
	if got := neverRanLine(c, &runner.Record{Status: runner.StatusFailed, Steps: []*runner.StepRecord{{ID: "a"}}}); !strings.HasPrefix(got, "6 later step(s) were not run (b, c, d and 3 more): ") {
		t.Errorf("the steps a stopped run never ran are capped at three names, got %q", got)
	}
	added := &diff.Report{Changes: []diff.Change{
		{Step: "create_order", Path: "order.currency", Kind: diff.KindUnexpected, Got: "EUR"},
		{Step: "list_orders", Path: "orders.0.currency", Kind: diff.KindUnexpected, Got: "EUR"},
	}}
	if got := regressionShape(added); !strings.Contains(got, "field(s) the safe spot does not have: create_order order.currency, list_orders orders.0.currency") {
		t.Errorf("a regression of only added fields names them as added: %q", got)
	}
	added.Changes = append(added.Changes, diff.Change{Step: "x", Path: "total", Kind: diff.KindChanged, Want: 1, Got: 2})
	if got := regressionShape(added); got != "" {
		t.Errorf("a changed value is not an added field: %q", got)
	}
}

const threeStepFlow = `apiVersion: shrt/v1
name: cli-thing-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: name
            equals: not-the-name
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: widget
    - id: fetch_again
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: widget
`

const keepGoingFlow = `apiVersion: shrt/v1
name: cli-keep-going
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: id
            exists: true
    - id: create_bad
      call: ThingService/Create
      body:
          name: gadget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: id
            equals: not-the-id
    - id: fetch_bad
      call: ThingService/Fetch
      body:
          id: ${create_bad.id}
    - id: fetch_bad_again
      call: ThingService/Fetch
      body:
          id: ${create_bad.id}
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: widget
    - id: fetch_compared
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: id
            equals: ${create_bad.id}
`

const threeCreates = `apiVersion: shrt/v1
name: cli-three
steps:
    - id: one
      call: ThingService/Create
      body: {name: a, kind: KIND_A}
    - id: two
      call: ThingService/Create
      body: {name: b, kind: KIND_A}
    - id: three
      call: ThingService/Create
      body: {name: c, kind: KIND_A}
`

const varChain = `apiVersion: shrt/v1
name: tagged
steps:
    - id: first
      call: ThingService/Create
      body:
          name: first
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: create_customer
      call: ThingService/Create
      body:
          name: c-${vars.batch}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

func TestRunPrintsWhatItDidNotRun(t *testing.T) {
	type runCase struct {
		name      string
		target    func(t *testing.T) string
		chain     string
		args      []string
		code      int
		has, lack []string
		count     map[string]int
	}
	thing := func(t *testing.T) string {
		srv := newFakeCLIBackend()
		t.Cleanup(srv.Close)
		return srv.URL
	}
	for _, c := range []runCase{
		{"a run that stops counts the steps it never ran", thing, threeStepFlow, []string{"cli-thing-flow", "-quiet"}, 1,
			[]string{"2 later step(s) were not run (fetch, fetch_again)", "-keep-going"}, nil, nil},
		{"-keep-going leaves no step unrun", thing, threeStepFlow, []string{"cli-thing-flow", "-quiet", "-keep-going"}, 1, nil, []string{"were not run"}, nil},
		{"-keep-going prints only the failures, a group per cause and a passed count", thing, keepGoingFlow, []string{"cli-keep-going", "-keep-going"}, 1,
			[]string{"FAIL   2 create_bad", "3 step(s) unevaluated behind create_bad: fetch_compared answered id=", "2 step(s) passed (-v prints every step)\n", "-keep-going: 4 of 6 steps did not pass (above)"},
			[]string{"ok     1 create", "fetch_bad ", "not evaluated", "not sent:"}, nil},
		{"-keep-going -v prints every step instead of the counts", thing, keepGoingFlow, []string{"cli-keep-going", "-keep-going", "-v"}, 1,
			[]string{"ok     1 create", "SKIP   3 fetch_bad", "not evaluated: ${create_bad.id}"}, []string{"unevaluated behind", "step(s) passed"}, nil},
		{"-keep-going against a dead target prints one line for the unsent steps", deadCLITarget, threeCreates, []string{"cli-three", "-keep-going", "-save=false"}, 3,
			nil, []string{" 3 three"}, map[string]int{"every remaining step": 1}},
		{"an errored run exits 3", deadCLITarget, "", []string{"cli-thing-flow", "-quiet", "-save=false"}, 3, nil, nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			chdirToFreshCLIWorkspace(t, c.target(t))
			if c.chain != "" {
				_, rest, _ := strings.Cut(c.chain, "name: ")
				name, _, _ := strings.Cut(rest, "\n")
				writeFile(t, ".shrt/chains/"+name+".yaml", c.chain)
			}
			var err error
			out := captureStdout(t, func() { err = runRun(context.Background(), c.args) })
			if code := exitCodeOf(err); code != c.code {
				t.Errorf("exit %d, want %d: %v\n%s", code, c.code, err, out)
			}
			for _, s := range c.has {
				if !strings.Contains(out, s) {
					t.Errorf("want %q in:\n%s", s, out)
				}
			}
			for _, s := range c.lack {
				if strings.Contains(out, s) {
					t.Errorf("want no %q in:\n%s", s, out)
				}
			}
			for s, n := range c.count {
				if strings.Count(out, s) != n {
					t.Errorf("want %q %d time(s) in:\n%s", s, n, out)
				}
			}
			if strings.Count(out, "dial tcp") > 2 {
				t.Errorf("the dial error is not repeated for every unsent step:\n%s", out)
			}
		})
	}
}

func TestAnUnsuppliedVarIsRefusedBeforeAnythingIsSent(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, filepath.Join(".shrt", "chains", "tagged.yaml"), varChain)
	writeFile(t, ".shrt/safespots/tagged.json", `{"chain":"tagged","run_id":"r","target":"t","confirmed_by":"test","confirmed_at":"2026-01-01T00:00:00Z","digest":"d","steps":[]}`)
	resealSafeSpot(t, ".shrt/safespots/tagged.json")
	for _, c := range []struct {
		run  func(context.Context, []string) error
		args []string
		ref  string
	}{
		{runRun, []string{"tagged", "-quiet"}, "${vars.batch}"},
		{runRun, []string{"tagged", "-quiet", "-dry-run"}, "${vars.batch}"},
		{runVerify, []string{"tagged", "-quiet"}, ""},
	} {
		calls.Store(0)
		err := c.run(context.Background(), c.args)
		if err == nil || !strings.Contains(err.Error(), "-var batch=...") || !strings.Contains(err.Error(), c.ref) {
			t.Errorf("%v: want a refusal naming -var batch=..., got %v", c.args, err)
		}
		if n := calls.Load(); n != 0 {
			t.Errorf("%v: %d request(s) sent before the refusal", c.args, n)
		}
	}
	calls.Store(0)
	if err := runRun(context.Background(), []string{"tagged", "-quiet", "-var", "batch=x"}); err != nil || calls.Load() != 2 {
		t.Errorf("with the var supplied both steps are sent: %v, %d call(s)", err, calls.Load())
	}
}

func TestAConfigThatDoesNotParseIsNotReportedAsMissing(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	writeFile(t, ".shrt/config.yaml", "target:\n    base_url: http://127.0.0.1:1\nconventions:\n    envelope_path: status.code\nconventions:\n    envelope_ok: SUCCESS\n")
	err := runDoctor(context.Background(), nil)
	if err == nil || strings.Contains(err.Error(), "shrt init' first") || !strings.Contains(err.Error(), "does not parse") || !strings.Contains(err.Error(), "conventions") {
		t.Fatalf("the config exists: say it does not parse and quote the yaml error, got %v", err)
	}
}

func TestVerifyReplaysEveryReachableStep(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	flow := `apiVersion: shrt/v1
name: cli-three-flow
steps:
    - id: create
      call: ThingService/Create
      body: {name: widget, kind: KIND_A}
      expect:
          - path: id
            not_empty: true
    - id: fetch
      call: ThingService/Fetch
      body: {id: "${create.id}"}
    - id: other
      call: ThingService/Fetch
      body: {id: thing-9}
`
	writeFile(t, ".shrt/chains/cli-three-flow.yaml", flow)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-three-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		_ = runConfirm(ctx, []string{"cli-three-flow", "-note", "baseline"})
		if err := runConfirm(ctx, []string{"cli-three-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	writeFile(t, ".shrt/chains/cli-three-flow.yaml", strings.Replace(flow, "not_empty: true", "equals: nope", 1))
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-three-flow", "-quiet", "-save=false"}) })
	if err == nil || strings.Contains(out, "length") || !strings.Contains(out, "first failing step: step 1 create") ||
		!strings.Contains(out, "[fetch] not_reached") || strings.Contains(out, "[other] not_reached") {
		t.Fatalf("verify names the first failure, the step held behind it, and still compares the independent one: %v\n%s", err, out)
	}
}

func TestVerifyVerdictsOnTheUniqueChain(t *testing.T) {
	setup := func(t *testing.T) (context.Context, *httptest.Server) {
		srv := newUniqueNameBackend()
		t.Cleanup(srv.Close)
		chdirToFreshCLIWorkspace(t, srv.URL)
		writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
		ctx := context.Background()
		approveUniqueChain(t, ctx)
		return ctx, srv
	}
	t.Run("a non-backend verdict leads and skips the drift dump", func(t *testing.T) {
		ctx, srv := setup(t)
		createForeignThing(t, srv.URL, "widget lead1")
		var err error
		out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=lead1"}) })
		lines := nonEmptyLines(out)
		if err == nil || len(lines) == 0 || !strings.HasPrefix(lines[0], "could not verify cli-unique: fixture collision") ||
			strings.Contains(out, "[create] changed") || !strings.Contains(out, "create (failed)") {
			t.Fatalf("the collision verdict is the first line and lists the steps briefly: %v\n%s", err, out)
		}
	})
	unordered := strings.Replace(uniqueNameChain, "    - id: fetch\n", "    - id: fetch\n      unordered: [items]\n", 1)
	t.Run("verify notes an unordered path added after approval without failing", func(t *testing.T) {
		ctx, _ := setup(t)
		writeFile(t, ".shrt/chains/cli-unique.yaml", unordered)
		var err error
		out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=u1"}) })
		if err != nil || !strings.Contains(out, "unordered: [items]") || !strings.Contains(out, "fetch") {
			t.Fatalf("%v\n%s", err, out)
		}
	})
	t.Run("confirm -supersede lists an unordered path added after approval", func(t *testing.T) {
		ctx, _ := setup(t)
		writeFile(t, ".shrt/chains/cli-unique.yaml", unordered)
		var err error
		out := captureStdout(t, func() {
			if err = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=u2"}); err == nil {
				err = runConfirm(ctx, []string{"cli-unique", "-supersede", "-note", "items are a set"})
			}
		})
		if err != nil || !strings.Contains(out, "unordered: [items]") || !strings.Contains(string(mustRead(t, ".shrt/safespots/pending/cli-unique.md")), "unordered: [items]") {
			t.Fatalf("the summary and the pending report list the added unordered path: %v\n%s", err, out)
		}
	})
}

const twoUUIDFieldsChain = `apiVersion: shrt/v1
name: cli-unique
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: n-${uuid}
          meta:
              source: s-${uuid}
      expect:
          - path: error.code
            equals: OK
`

func TestARefusalOverAnotherFieldIsNotTheSameWay(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		name, _ := body["name"].(string)
		meta, _ := body["meta"].(map[string]any)
		source, _ := meta["source"].(string)
		switch calls.Add(1) {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "n"})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "SOURCE_TAKEN", "message": "source " + source + " already exists"}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "NAME_TAKEN", "message": "name " + name + " already exists"}})
		}
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", twoUUIDFieldsChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	for _, want := range []verifyOutcome{
		{code: 3, has: []string{"meta.source="}},
		{code: 3, lack: []string{"FINDING"}},
		{code: 1, has: []string{"FINDING: "}},
	} {
		var err error
		out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet"}) })
		want.check(t, "a conflict on source, then on name, then on name again", err, out)
	}
}

func TestCLIAHandEditedRunRecordIsNotEvidence(t *testing.T) {
	approvedThingFlow(t)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
	e, err := loadEnv(true)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := e.store.ListRuns("cli-thing-flow")
	if err != nil || len(ids) < 2 {
		t.Fatalf("runs: %v %v", ids, err)
	}
	id := ids[len(ids)-1]
	path := filepath.Join(".shrt", "runs", "cli-thing-flow", id+".json")
	raw := string(mustRead(t, path))
	if edited := strings.Replace(raw, `"name": "widget"`, `"name": "gadget"`, 1); edited != raw {
		writeFile(t, path, edited)
	} else {
		t.Fatal("fixture edit did not apply")
	}
	for _, c := range []struct {
		name string
		run  func() error
	}{
		{"verify -run", func() error { return runVerify(ctx, []string{"cli-thing-flow", "-run", id}) }},
		{"diff", func() error { return runDiff(ctx, []string{"cli-thing-flow"}) }},
		{"diff by run id", func() error { return runDiff(ctx, []string{ids[0], id}) }},
		{"chain slice -run", func() error { return chainSlice(ctx, []string{"cli-thing-flow", "-step", "fetch", "-run", id}) }},
	} {
		var err error
		captureStdout(t, func() { err = c.run() })
		if !errors.Is(err, store.ErrRunEdited) {
			t.Errorf("%s of an edited record must refuse it: %v", c.name, err)
		}
	}
	if out := captureStdout(t, func() { _ = chainHollow(nil) }); !strings.Contains(out, "changed after shrt wrote them") {
		t.Errorf("hollow says it did not count an edited record:\n%s", out)
	}
}
