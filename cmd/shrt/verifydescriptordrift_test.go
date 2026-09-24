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

func TestVerifyGivesADescriptorVerdictWhenTheFirstFailingStepOnlyDrifted(t *testing.T) {
	extra := false
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
			out := map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "widget"}
			if extra {
				out["traceHint"] = "t-1"
			}
			_ = json.NewEncoder(w).Encode(out)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
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
	extra = true
	var verr error
	out := captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"})
	})
	if code := exitCodeOf(verr); code != 3 {
		t.Fatalf("a field the descriptor does not declare is descriptor drift, not a regression: want exit 3, got %d: %v\n%s", code, verr, out)
	}
	for _, want := range []string{
		"could not verify cli-thing-flow: the response at fetch does not match the descriptor",
		`unknown field "traceHint"`,
		"shrt catalog build",
		"validate_output",
	} {
		if !strings.Contains(verr.Error(), want) {
			t.Errorf("want %q in %v", want, verr)
		}
	}
	if strings.Contains(verr.Error(), "regression") {
		t.Errorf("descriptor drift is no regression verdict: %v", verr)
	}
}
