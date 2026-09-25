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

func newRound2Backend(fetchCode *string) *httptest.Server {
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		refuse := func(msg string) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "invalid_argument", "message": msg})
		}
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			switch body["name"] {
			case nil, "":
				refuse("name required")
			case "filler":
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "INTERNAL"}})
			default:
				next++
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next)})
			}
		case "/shrt.test.v1.ThingService/Fetch":
			if body["id"] == nil || body["id"] == "" {
				refuse("id required")
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": *fetchCode}, "id": body["id"], "name": "widget"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

const round2Chain = `apiVersion: shrt/v1
name: cli-r2-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: w-${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: other
      call: ThingService/Create
      body:
          name: other
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fill
      call: ThingService/Create
      body:
          name: filler
          kind: KIND_A
      expect:
          - path: error.code
            equals: INTERNAL
    - id: blank
      call: ThingService/Create
      body:
          name: ""
          kind: KIND_A
      expect:
          - path: transport.code
            equals: invalid_argument
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: widget
`

func round2Workspace(t *testing.T) *string {
	t.Helper()
	code := "OK"
	srv := newRound2Backend(&code)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-r2-flow.yaml", round2Chain)
	if err := runRun(context.Background(), []string{"cli-r2-flow", "-quiet", "-var", "tag=T1"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	return &code
}

func round2Slice(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), append([]string{"cli-r2-flow"}, args...))
	})
	return out, err
}

func TestCLISliceVerifyDoesNotCountRefusedWritesAndRecordsTheVerdict(t *testing.T) {
	round2Workspace(t)
	out, err := round2Slice(t, "-step", "fetch", "-run", "latest", "-keep", "other", "-var", "tag=T2", "-verify", "-write")
	if err != nil {
		t.Fatalf("fill and blank were refused in the source run, so they wrote nothing; the slice must reproduce: %v\n%s", err, out)
	}
	if !strings.Contains(out, "verify reproduced") {
		t.Fatalf("want reproduced:\n%s", out)
	}
	if !strings.Contains(out, "2 dropped write step(s) wrote nothing in run") ||
		!strings.Contains(out, "refused: error.code = INTERNAL") || !strings.Contains(out, "refused: transport invalid_argument") {
		t.Fatalf("the output must say which dropped writes were refused and why:\n%s", out)
	}
	if strings.Contains(out, "WARNING possible under-inclusion") {
		t.Fatalf("refused writes are not under-inclusion:\n%s", out)
	}
	written, err := chain.LoadFile(".shrt/chains/cli-r2-flow-slice-fetch.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(written.Description, "HYPOTHESIS") || !strings.Contains(written.Description, "VERIFIED") {
		t.Fatalf("a reproduced slice's description must record the verdict, not the hypothesis:\n%s", written.Description)
	}
	if written.Vars["tag"] != "T2" {
		t.Fatalf("the -var value of an undeclared var belongs in the written slice, got %v", written.Vars)
	}
}

func TestCLISliceNotReproducedNamesTheCommandThatKeepsTheWrites(t *testing.T) {
	code := round2Workspace(t)
	*code = "PERMISSION_DENIED"
	out, err := round2Slice(t, "-step", "fetch", "-run", "latest", "-var", "tag=T2", "-verify")
	if exitCodeOf(err) != 1 || !strings.Contains(out, "NOT REPRODUCED") {
		t.Fatalf("the verdict differs, want NOT REPRODUCED exit 1, got %v:\n%s", err, out)
	}
	next := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "next: ") {
			next = l
		}
	}
	if !strings.Contains(next, "-keep writes") || !strings.Contains(next, "-var tag=<fresh>") {
		t.Fatalf("a NOT REPRODUCED slice that dropped a write must name the -keep command: %q\n%s", next, out)
	}
	if strings.Contains(next, "fill") || strings.Contains(next, "blank") {
		t.Fatalf("refused writes wrote nothing and are not worth keeping: %q", next)
	}
}

func TestCLISliceClosureVerifyRefusesAnUndeclaredVarWithTheFlagToPass(t *testing.T) {
	round2Workspace(t)
	_, err := round2Slice(t, "-step", "fetch", "-run", "latest", "-verify")
	if err == nil || !strings.Contains(err.Error(), "-var tag=<fresh>") {
		t.Fatalf("closure re-creates w-${vars.tag}, so the run's tag collides; want the -var to pass, got %v", err)
	}
}

func TestCLIPinSliceTakesAnUndeclaredVarFromTheSourceRun(t *testing.T) {
	round2Workspace(t)
	out, err := round2Slice(t, "-step", "create", "-mode", "pin", "-run", "latest", "-write")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "tag = T1  (the value run ") {
		t.Fatalf("pin mode must say tag is the value the run used:\n%s", out)
	}
	written, err := chain.LoadFile(".shrt/chains/cli-r2-flow-slice-create.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if written.Vars["tag"] != "T1" {
		t.Fatalf("the pinned slice must run on its own, got vars %v", written.Vars)
	}
}

func TestCLISliceWriteRefusesToOverwriteAnotherChain(t *testing.T) {
	round2Workspace(t)
	other := "apiVersion: shrt/v1\nname: probe\nsteps:\n    - id: only\n      call: ThingService/Fetch\n      body:\n          id: x\n"
	writeFile(t, ".shrt/chains/probe.yaml", other)
	_, err := round2Slice(t, "-step", "fetch", "-write", "probe")
	if err == nil || !strings.Contains(err.Error(), "-force") {
		t.Fatalf("an existing chain that is not this slice must not be overwritten silently, got %v", err)
	}
	if raw, _ := os.ReadFile(".shrt/chains/probe.yaml"); string(raw) != other {
		t.Fatal("the refused write must leave the file untouched")
	}
	if _, err := round2Slice(t, "-step", "fetch", "-write", "probe", "-force"); err != nil {
		t.Fatalf("-force overwrites: %v", err)
	}
	if _, err := round2Slice(t, "-step", "fetch", "-keep", "other", "-write", "probe"); err != nil {
		t.Fatalf("re-slicing the same step into this command's own slice needs no -force: %v", err)
	}
	if _, err := round2Slice(t, "-step", "blank", "-write", "probe"); err == nil {
		t.Fatal("a slice of another step is not the same slice and needs -force")
	}
}

func TestCLIWhichSeesTransportRefusedStepsAndPrintsEvidenceOnItsOwnLine(t *testing.T) {
	round2Workspace(t)
	out := whichOut(t, "-code", "invalid_argument")
	if !strings.Contains(whichLine(t, out, "blank"), "got invalid_argument, step passed") {
		t.Fatalf("-code must find transport.code and the refused run did reach the step:\n%s", out)
	}
	out = whichOut(t, "-rpc", "ThingService/Create")
	if strings.Contains(out, "no local run reached it") {
		t.Fatalf("a transport-refused step that passed was reached:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "OBSERVED") && strings.Contains(l, " got ") {
			t.Errorf("the run evidence goes on its own line, never on the OBSERVED line: %q", l)
		}
		if strings.Contains(l, " got ") && !strings.HasPrefix(l, "    run ") {
			t.Errorf("every evidence line reads the same way: %q", l)
		}
	}
	if strings.Contains(out, "-mode pin -run ") || !strings.Contains(out, "-var tag=<fresh>") {
		t.Fatalf("the reproduce line of a write is a closure slice and must ask for a fresh tag, which create interpolates into a name:\n%s", out)
	}
}

func TestObservedResponseCarriesTheTransportOutcome(t *testing.T) {
	sr := &runner.StepRecord{HTTPStatus: 400, Response: json.RawMessage(`{"code":"invalid_argument","message":"m"}`),
		Transport: &runner.TransportError{Code: "invalid_argument", Message: "m"}}
	got, ok := chain.Get(observedResponse(sr), "transport.code")
	if !ok || got != "invalid_argument" {
		t.Fatalf("transport.code must be readable from the observation, got %v %v", got, ok)
	}
	sr = &runner.StepRecord{HTTPStatus: 200, Response: json.RawMessage(`{"error":{"code":"OK"}}`)}
	resp := observedResponse(sr)
	if v, _ := chain.Get(resp, "transport.code"); v != chain.TransportOK {
		t.Fatalf("a 200 answer reads transport.code ok, got %v", v)
	}
	if v, _ := chain.Get(resp, "error.code"); v != "OK" {
		t.Fatalf("the recorded body must survive the merge, got %v", v)
	}
}
