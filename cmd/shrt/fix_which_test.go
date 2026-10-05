package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func whichOut(t *testing.T, args ...string) string {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = chainWhich(args) })
	if err != nil {
		t.Fatalf("chain which %v: %v", args, err)
	}
	return out
}

func latestRunID(t *testing.T, chainName string) string {
	t.Helper()
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := e.store.LatestRun(chainName)
	if err != nil {
		t.Fatal(err)
	}
	return rec.RunID
}

func whichLine(t *testing.T, out, step string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if l == "  "+step || strings.HasPrefix(l, "  "+step+"  ") {
			return l
		}
	}
	t.Fatalf("no row for step %s in:\n%s", step, out)
	return ""
}

const fixRefusalChain = `apiVersion: shrt/v1
name: cli-refusal-flow
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
      export:
          thing_id: id
    - id: outsider_reads
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: PERMISSION_DENIED
`

func TestCLIWhichCitesTheRunsThatReachedTheStep(t *testing.T) {
	f := &fixThing{fetchCode: "PERMISSION_DENIED"}
	fixWorkspace(t, f, fixRefusalChain)
	if _, err := fixCmd(t, "run", "cli-refusal-flow", "-quiet"); err != nil {
		t.Fatalf("the backend refuses the outsider, so the run passes: %v", err)
	}
	reached := latestRunID(t, "cli-refusal-flow")
	f.set(func(f *fixThing) { f.createCode = "INTERNAL" })
	fixCmd(t, "run", "cli-refusal-flow", "-quiet", "-keep-going")
	stopped := latestRunID(t, "cli-refusal-flow")
	out := whichOut(t, "-code", "PERMISSION_DENIED")
	if line := whichLine(t, out, "outsider_reads"); line != "  outsider_reads  newest run: step skipped" || reached == stopped {
		t.Errorf("the only run that reached the step passed it, so only the newest run, which did not reach it, is named:\n%s", out)
	}
	f.set(func(f *fixThing) { f.createCode, f.fetchCode = "", "" })
	if _, err := fixCmd(t, "run", "cli-refusal-flow", "-quiet"); err == nil {
		t.Fatal("the backend lets the outsider read, so the run must fail")
	}
	newest := latestRunID(t, "cli-refusal-flow")
	out = whichOut(t, "-code", "PERMISSION_DENIED")
	if line := whichLine(t, out, "outsider_reads"); line != "  outsider_reads  FAILED error.code want=PERMISSION_DENIED got=OK" || newest == "" {
		t.Errorf("the newest reaching run got OK and failed, and that is the evidence:\n%s", out)
	}
	if raw := whichOut(t, "-code", "PERMISSION_DENIED", "-json"); !strings.Contains(raw, `"observed": {`) || !strings.Contains(raw, `"holds": false`) {
		t.Errorf("-json carries the contradicting observation:\n%s", raw)
	}
	line := whichLine(t, whichOut(t, "-rpc", "ThingService/Fetch"), "outsider_reads")
	if observed := line[strings.Index(line, "FAILED"):]; !strings.Contains(observed, "got=OK") {
		t.Errorf("the observed part shows the recorded code: %q", line)
	}
	if _, err := fixCmd(t, "run", "cli-thing-flow", "-quiet"); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	if line := whichLine(t, whichOut(t, "-rpc", "ThingService/Fetch"), "fetch"); line != "  fetch  asserts OK" {
		t.Errorf("a step that passed says no verdict: %q", line)
	}
	appCode := `apiVersion: shrt/v1
name: cli-app-code
steps:
    - id: refused
      call: ThingService/Fetch
      body:
          id: x
      expect:
          - path: error.code
            equals: PERMISSION_DENIED
`
	writeFile(t, ".shrt/chains/cli-app-code.yaml", appCode)
	f.set(func(f *fixThing) { f.fetchCode = "PERMISSION_DENIED" })
	fixCmd(t, "run", "cli-app-code", "-quiet")
	writeFile(t, ".shrt/chains/cli-app-code.yaml", appCode+"          - path: error.details.0.app_code\n            equals: 1304\n")
	if line := whichLine(t, whichOut(t, "-rpc", "ThingService/Fetch"), "refused"); line != "  refused  asserts 1304  got PERMISSION_DENIED at error.code" {
		t.Errorf("got is read from the path of the shown assertion: %q", line)
	}
	writeFile(t, ".shrt/chains/cli-writes.yaml", `apiVersion: shrt/v1
name: cli-writes
steps:
    - id: create_a
      call: ThingService/Create
      body:
          name: a
    - id: create_b
      call: ThingService/Create
      body:
          name: b
    - id: create_c
      call: ThingService/Create
      body:
          name: c-${create_a.id}
      expect:
          - path: error.code
            equals: OK
`)
	fixCmd(t, "run", "cli-writes", "-quiet")
	out = whichOut(t, "-code", "OK")
	if want := "\nshrt chain slice cli-writes -step create_c  (2 of 3 steps; if it does not reproduce, add -keep writes)\n"; !strings.Contains(out, want) {
		t.Errorf("the plain closure comes first when it keeps every related write: missing %q in:\n%s", want, out)
	}
}

func TestWhichCodeCountsASiblingAssertionAndTheSafeSpotBaseline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "1603", "message": "NotYours"}, "name": "widget"})
	}))
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	raw, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/config.yaml", string(raw)+"conventions:\n    envelope_path: name\n    envelope_ok: widget\n    code_fields: [code, message]\n")
	const denied = `apiVersion: shrt/v1
name: cli-which-baselined
steps:
    - id: denied
      call: ThingService/Fetch
      body:
          id: thing-1
      expect:
          - path: error.message
            equals: NotYours
`
	writeFile(t, ".shrt/chains/cli-which-baselined.yaml", denied)
	if _, err := fixCmd(t, "run", "cli-which-baselined", "-quiet"); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	out := whichOut(t, "-code", "1603")
	if !strings.Contains(out, "  denied  asserts only reason NotYours\n") || !strings.Contains(out, "asserting 1603 or NotYours") {
		t.Errorf("the step asserts the sibling detail of the same refusal, so which lists it:\n%s", out)
	}
	writeFile(t, ".shrt/chains/cli-which-baselined.yaml", strings.Replace(denied,
		"- path: error.message\n            equals: NotYours", "- path: error\n            exists: true", 1))
	fixApprove(t, "cli-which-baselined")
	out = whichOut(t, "-code", "1603")
	if strings.Contains(out, "goes unnoticed") || !strings.Contains(out, "safe spot") || !strings.Contains(out, "shrt verify") {
		t.Errorf("which says the safe spot baselines the code and verify compares it:\n%s", out)
	}
}

func TestChainWhichFindsTransportCodesAndHTTPStatuses(t *testing.T) {
	fixWorkspace(t, &fixThing{})
	if _, err := fixCmd(t, "run", "cli-thing-flow", "-quiet"); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	base, err := e.store.LatestRun("cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	saveRefusedAtFetch(t, e, base, "20990101T000000Z-refused1")
	if out := whichOut(t, "-code", "unauthenticated"); !strings.Contains(out, "cli-thing-flow") || !strings.Contains(out, "fetch") || strings.Contains(out, "no local run record observed it") {
		t.Fatalf("a run record holds a 401 unauthenticated at fetch, so which cites it:\n%s", out)
	}
	writeFile(t, ".shrt/chains/cli-noauth.yaml", `apiVersion: shrt/v1
name: cli-noauth
steps:
    - id: fetch
      call: ThingService/Fetch
      skip_auth: true
      body:
          id: x
      expect:
          - path: transport.http_status
            equals: 401
`)
	if out := whichOut(t, "-code", "401"); !strings.Contains(out, "cli-noauth") {
		t.Fatalf("cli-noauth asserts transport.http_status equals 401, so which -code 401 lists it:\n%s", out)
	}
}

func TestWhichNamesTheFailingExpectationOnTheObservedLine(t *testing.T) {
	none := func(string, string) string { return "" }
	h := chain.WhichChain{Chain: "c", Runs: 1}
	m := chain.WhichStep{Step: "s", Asserts: []chain.CodeAssertion{{Path: "status.code", Value: "SUCCESS"}}, Observed: &chain.WhichEvidence{
		Run: "r1", Status: runner.StatusFailed, Code: "SUCCESS", Path: "status.code", Asserted: "status.code",
		Failures: []chain.ExpectResult{{Path: "customer.name", Rule: "equals", Want: "idem i4", Got: "changed"}},
	}}
	if line := whichRow(h, m, chain.WhichQuery{}, "SUCCESS", none); line != "s  FAILED customer.name want=\"idem i4\" got=changed" && line != "s  FAILED customer.name want=idem i4 got=changed" {
		t.Fatalf("a step that got the asserted code yet FAILED says which expectation failed: %q", line)
	}
	m.Observed = &chain.WhichEvidence{Run: "r1", Status: runner.StatusPassed, Code: "SUCCESS", Path: "status.code", Asserted: "status.code"}
	if line := whichRow(h, m, chain.WhichQuery{}, "SUCCESS", none); line != "s" {
		t.Fatalf("a passing step asserting the usual code is its step id alone: %q", line)
	}
	m.Observed = nil
	if line := whichRow(h, m, chain.WhichQuery{}, "OK", none); line != "s  asserts SUCCESS  no run reached it" {
		t.Fatalf("a step no run reached says so: %q", line)
	}
}

func newMintingBackend(t *testing.T, prefix string, refuseAll bool) *httptest.Server {
	t.Helper()
	minted := map[string]bool{}
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			id := prefix + "-" + itoa(next)
			minted[id] = true
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": id})
		case "/shrt.test.v1.ThingService/Fetch":
			id, _ := body["id"].(string)
			if !minted[id] || refuseAll {
				_ = json.NewEncoder(w).Encode(fixErr("REJECTED", "no thing "+id))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": id})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestARunFromAnotherTargetIsNoEvidenceHere(t *testing.T) {
	for _, refuseAll := range []bool{false, true} {
		first := newMintingBackend(t, "old", false)
		chdirToFreshCLIWorkspace(t, first.URL)
		writeFile(t, ".shrt/chains/cli-pair.yaml", `apiVersion: shrt/v1
name: cli-pair
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
`)
		if _, err := fixCmd(t, "run", "cli-pair", "-quiet"); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		second := newMintingBackend(t, "new", refuseAll)
		writeFile(t, ".shrt/config.yaml", strings.Replace(string(mustRead(t, ".shrt/config.yaml")), first.URL, second.URL, 1))
		if !refuseAll {
			if out := whichOut(t, "-rpc", "ThingService/Fetch"); strings.Contains(out, "-mode pin -run") {
				t.Fatalf("the only run was recorded against another target, so which must not offer to pin from it:\n%s", out)
			}
			continue
		}
		var err error
		out := captureStdout(t, func() {
			err = chainSlice(context.Background(), []string{"cli-pair", "-step", "fetch", "-run", "latest", "-verify"})
		})
		if !strings.Contains(out, "the source run was recorded against "+first.URL+", this target is "+second.URL) || exitCodeOf(err) != 3 || strings.Contains(out, "NOT REPRODUCED") {
			t.Fatalf("a different verdict on another target is inconclusive and names both targets (exit %d):\n%s", exitCodeOf(err), out)
		}
	}
}
