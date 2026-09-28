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
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if !strings.Contains(l, " "+step+" ") || !strings.Contains(l, "OBSERVED") {
			continue
		}
		if !strings.Contains(l, "run ") && i+1 < len(lines) {
			return l + " " + strings.TrimSpace(lines[i+1])
		}
		return l
	}
	t.Fatalf("no OBSERVED line for step %s in:\n%s", step, out)
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
	if !strings.Contains(whichLine(t, out, "outsider_reads"), "run "+reached+" got PERMISSION_DENIED, step passed") || !strings.Contains(out, "newest run "+stopped+" did not reach it") {
		t.Errorf("the only run that reached the step is the evidence, and the newest did not reach it:\n%s", out)
	}
	f.set(func(f *fixThing) { f.createCode, f.fetchCode = "", "" })
	if _, err := fixCmd(t, "run", "cli-refusal-flow", "-quiet"); err == nil {
		t.Fatal("the backend lets the outsider read, so the run must fail")
	}
	newest := latestRunID(t, "cli-refusal-flow")
	out = whichOut(t, "-code", "PERMISSION_DENIED")
	if !strings.Contains(whichLine(t, out, "outsider_reads"), "run "+newest+" got OK, step FAILED") ||
		!strings.Contains(out, "failed: error.code want=PERMISSION_DENIED got=OK") || !strings.Contains(out, "1 with a local run record that reached a matching step") {
		t.Errorf("the newest reaching run got OK and failed, and that is the evidence:\n%s", out)
	}
	if raw := whichOut(t, "-code", "PERMISSION_DENIED", "-json"); !strings.Contains(raw, `"observed": {`) || !strings.Contains(raw, `"holds": false`) {
		t.Errorf("-json carries the contradicting observation:\n%s", raw)
	}
	line := whichLine(t, whichOut(t, "-rpc", "ThingService/Fetch"), "outsider_reads")
	if observed := line[strings.Index(line, "run "):]; strings.Contains(observed, "PERMISSION_DENIED") || !strings.Contains(observed, "OK") || !strings.Contains(observed, "FAILED") {
		t.Errorf("the observed part shows the recorded code, not the asserted one: %q", line)
	}
	if _, err := fixCmd(t, "run", "cli-thing-flow", "-quiet"); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	if line := whichLine(t, whichOut(t, "-rpc", "ThingService/Fetch"), "fetch"); strings.Contains(line, "FAILED") || !strings.Contains(line, "passed") {
		t.Errorf("a step that passed reads as passed: %q", line)
	}
	writeFile(t, ".shrt/chains/cli-app-code.yaml", `apiVersion: shrt/v1
name: cli-app-code
steps:
    - id: refused
      call: ThingService/Fetch
      body:
          id: x
      expect:
          - path: error.code
            equals: PERMISSION_DENIED
          - path: error.details.0.app_code
            equals: 1304
`)
	f.set(func(f *fixThing) { f.fetchCode = "PERMISSION_DENIED" })
	fixCmd(t, "run", "cli-app-code", "-quiet")
	if line := whichLine(t, whichOut(t, "-rpc", "ThingService/Fetch"), "refused"); !strings.Contains(line, "asserts 1304") ||
		strings.Contains(line, "got PERMISSION_DENIED,") || !strings.Contains(line, "nothing at error.details.0.app_code") {
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
	for _, want := range []string{
		"reproduce: shrt chain slice cli-writes -step create_c  (2 of 3 steps)",
		"if that does not reproduce: shrt chain slice cli-writes -step create_c -keep writes  (3 of 3 steps)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the plain closure comes first when it keeps every related write: missing %q in:\n%s", want, out)
		}
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
	if !strings.Contains(out, "denied  asserts NotYours") || !strings.Contains(out, "asserts 1603, or only the reason NotYours") {
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
	line := whichSeenCell(&chain.WhichEvidence{
		Run: "r1", Status: runner.StatusFailed, Code: "SUCCESS", Path: "status.code", Asserted: "status.code",
		Failures: []chain.ExpectResult{{Path: "customer.name", Rule: "equals", Want: "idem i4", Got: "changed"}},
	})
	if !strings.Contains(line, "got SUCCESS") || !strings.Contains(line, "FAILED") || !strings.Contains(line, "customer.name") {
		t.Fatalf("a step that got the asserted code yet FAILED says which expectation failed: %q", line)
	}
	if passed := whichSeenCell(&chain.WhichEvidence{Run: "r1", Status: runner.StatusPassed, Code: "SUCCESS", Path: "status.code", Asserted: "status.code"}); strings.Contains(passed, "on ") {
		t.Fatalf("a passing step names no failure: %q", passed)
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
