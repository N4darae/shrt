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

func newEchoNameBackend() *httptest.Server {
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": body["name"]})
		case "/shrt.test.v1.ThingService/Fetch":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "widget"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func confirmedThingFlow(t *testing.T) {
	t.Helper()
	srv := newEchoNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	captureStdout(t, func() {
		if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-note", "create echoes the name"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	path := ".shrt/chains/cli-thing-flow.yaml"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "name: widget\n          kind", "name: gadget\n          kind", 1)
	if edited == string(raw) {
		t.Fatal("fixture edit did not apply")
	}
	writeFile(t, path, edited)
}

func TestCLIVerifySaysTheRequestDiffersBeforeCallingItARegression(t *testing.T) {
	confirmedThingFlow(t)
	var err error
	out := captureStdout(t, func() {
		err = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"})
	})
	if err == nil {
		t.Fatal("the response changed, so verify must not pass")
	}
	want := "request differs from the confirmed run at create name (widget -> gadget)"
	if !strings.Contains(out, want) {
		t.Fatalf("verify must say the chain now sends different input, want %q:\n%s", want, out)
	}
	if strings.Index(out, want) > strings.Index(out, "[create]") {
		t.Fatalf("the request differences come before the response changes:\n%s", out)
	}
	if strings.Contains(out, "idempotency_key") {
		t.Fatalf("a ${uuid} field differs every run and is not an input change:\n%s", out)
	}
	if strings.Contains(err.Error(), "regression") {
		t.Fatalf("with different input the changes are not evidence of a backend regression: %v", err)
	}
}

func TestCLIConfirmSupersedeShowsTheDifferencesAgainstTheReplacedSafeSpot(t *testing.T) {
	confirmedThingFlow(t)
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	var err error
	out := captureStdout(t, func() {
		err = runConfirm(context.Background(), []string{"cli-thing-flow", "-supersede", "-note", "renamed to gadget"})
	})
	if err != nil {
		t.Fatalf("propose -supersede: %v", err)
	}
	for _, want := range []string{"request name widget -> gadget", "response name widget -> gadget"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the approver signs off on replacing the safe spot, so the summary must show %q:\n%s", want, out)
		}
	}
}
