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

func violationWorkspace(t *testing.T, name *any) {
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
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": *name})
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

func verifyWithRebuildMatching(t *testing.T, matches bool) (error, string) {
	t.Helper()
	was := descriptorMatchesRebuild
	descriptorMatchesRebuild = func(context.Context, *config.Config) (bool, error) { return matches, nil }
	defer func() { descriptorMatchesRebuild = was }()
	var verr error
	out := captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"})
	})
	return verr, out
}

func TestVerifyCallsABodyTheCurrentProtoCannotHoldARegression(t *testing.T) {
	var name any = "widget"
	violationWorkspace(t, &name)
	name = 7
	verr, out := verifyWithRebuildMatching(t, true)
	if code := exitCodeOf(verr); code != 1 {
		t.Fatalf("a wrong-typed field against a descriptor that matches a rebuild is a backend change: want exit 1, got %d: %v\n%s", code, verr, out)
	}
	for _, want := range []string{"regression", "fetch", "name", "7"} {
		if !strings.Contains(verr.Error(), want) {
			t.Errorf("want %q in %v", want, verr)
		}
	}
	if strings.Contains(verr.Error(), "not a verdict") || strings.Contains(out, "could not verify") {
		t.Errorf("a proto violation with a current descriptor is a verdict:\n%v\n%s", verr, out)
	}
}

func TestVerifyKeepsExit3ForABodyAStaleDescriptorCannotHold(t *testing.T) {
	var name any = "widget"
	violationWorkspace(t, &name)
	name = 7
	verr, out := verifyWithRebuildMatching(t, false)
	if code := exitCodeOf(verr); code != 3 {
		t.Fatalf("a stale descriptor is no verdict: want exit 3, got %d: %v\n%s", code, verr, out)
	}
	if !strings.Contains(verr.Error(), "shrt catalog build") {
		t.Errorf("want the rebuild remedy: %v", verr)
	}
}
