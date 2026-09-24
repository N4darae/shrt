package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func driftWorkspace(t *testing.T, name *string, extra *bool) {
	t.Helper()
	nextID := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			nextID++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(nextID)})
		case "/shrt.test.v1.ThingService/Fetch":
			out := map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": *name}
			if *extra {
				out["traceHint"] = "t-1"
			}
			_ = json.NewEncoder(w).Encode(out)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	raw, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/config.yaml", string(raw)+"conventions:\n    validate_output: true\n")
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-note", "fetch returns the created name"}); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
		t.Fatalf("approve: %v", err)
	}
}

func TestVerifyCallsADeclaredFieldChangeInADriftedResponseARegression(t *testing.T) {
	name, extra := "widget", false
	driftWorkspace(t, &name, &extra)
	name, extra = "gadget", true
	var verr error
	out := captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"})
	})
	if code := exitCodeOf(verr); code != 1 {
		t.Fatalf("a declared field changed as well as the drift: want exit 1, got %d: %v\n%s", code, verr, out)
	}
	if !strings.Contains(verr.Error(), "regression") || !strings.Contains(verr.Error(), "fetch name") {
		t.Errorf("the verdict must be a regression naming the declared change: %v", verr)
	}
	if strings.Contains(out, "could not verify") {
		t.Errorf("a declared change is a verdict, not could-not-verify:\n%s", out)
	}
	for _, want := range []string{"widget", "gadget", `unknown field "traceHint"`} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in the human output:\n%s", want, out)
		}
	}
}

func TestVerifyDriftAdviceDoesNotSayRebuildWhenTheDescriptorMatchesARebuild(t *testing.T) {
	name, extra := "widget", false
	driftWorkspace(t, &name, &extra)
	was := descriptorMatchesRebuild
	descriptorMatchesRebuild = func(context.Context, *config.Config) (bool, error) { return true, nil }
	defer func() { descriptorMatchesRebuild = was }()
	extra = true
	var verr error
	captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"})
	})
	if code := exitCodeOf(verr); code != 3 {
		t.Fatalf("drift only: want exit 3, got %d: %v", code, verr)
	}
	if strings.Contains(verr.Error(), "catalog build") {
		t.Errorf("the descriptor matches a rebuild, so rebuilding is no remedy: %v", verr)
	}
	if !strings.Contains(verr.Error(), "the proto does not declare") {
		t.Errorf("want the backend-sends-undeclared-fields cause: %v", verr)
	}
}
