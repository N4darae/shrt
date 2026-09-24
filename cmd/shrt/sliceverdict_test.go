package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

type sliceBackend struct {
	fetch func(id string) (int, map[string]any)
	next  int
}

func newSliceBackend(t *testing.T, b *sliceBackend) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			b.next++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(b.next)})
		case "/shrt.test.v1.ThingService/Fetch":
			id, _ := body["id"].(string)
			code, out := b.fetch(id)
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(out)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-pair.yaml", `apiVersion: shrt/v1
name: cli-pair
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
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
          - path: id
            equals: thing-0
`)
}

func slicePairVerify(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), append([]string{"cli-pair", "-step", "fetch", "-verify", "-run", "latest"}, args...))
	})
	if err != nil {
		out += "\n" + err.Error()
	}
	return out, err
}

func TestCLISliceVerifyMasksAFreshIdInAFailureBothRunsShare(t *testing.T) {
	b := &sliceBackend{fetch: func(id string) (int, map[string]any) {
		return 200, map[string]any{"error": map[string]any{"code": "OK"}, "id": id, "name": "widget"}
	}}
	newSliceBackend(t, b)
	captureStdout(t, func() { _ = runRun(context.Background(), []string{"cli-pair", "-quiet"}) })
	out, err := slicePairVerify(t)
	if exitCodeOf(err) != 0 || !strings.Contains(out, "verify reproduced") {
		t.Fatalf("the same failure with a fresh id in got is reproduced:\n%s", out)
	}
}

func TestCLISliceVerifyComparesTheRefusalMessage(t *testing.T) {
	message := "qty must be greater than zero"
	b := &sliceBackend{fetch: func(string) (int, map[string]any) {
		return 200, map[string]any{"error": map[string]any{"code": "REJECTED", "message": message}, "id": "thing-0"}
	}}
	newSliceBackend(t, b)
	captureStdout(t, func() { _ = runRun(context.Background(), []string{"cli-pair", "-quiet"}) })
	message = "caller must hold role ADMIN"
	out, err := slicePairVerify(t)
	if exitCodeOf(err) != 1 || !strings.Contains(out, "NOT REPRODUCED") || !strings.Contains(out, "caller must hold role ADMIN") {
		t.Fatalf("a refusal for another reason is not the same failure:\n%s", out)
	}
}

func TestCLISliceVerifyCallsATransportRefusalNotReproduced(t *testing.T) {
	refuse := false
	b := &sliceBackend{fetch: func(id string) (int, map[string]any) {
		if refuse {
			return http.StatusForbidden, map[string]any{"code": "permission_denied", "message": "no"}
		}
		return 200, map[string]any{"error": map[string]any{"code": "OK"}, "id": id, "name": "widget"}
	}}
	newSliceBackend(t, b)
	captureStdout(t, func() { _ = runRun(context.Background(), []string{"cli-pair", "-quiet"}) })
	refuse = true
	out, err := slicePairVerify(t)
	if strings.Contains(out, "DID NOT RUN") || strings.Contains(out, "never reached the backend") {
		t.Fatalf("a 403 was sent and refused, it ran:\n%s", out)
	}
	if exitCodeOf(err) != 1 || !strings.Contains(out, "NOT REPRODUCED") || !strings.Contains(out, "403") {
		t.Fatalf("the source got an answer and the slice a 403: not reproduced:\n%s", out)
	}
	captureStdout(t, func() { _ = runRun(context.Background(), []string{"cli-pair", "-quiet"}) })
	out, err = slicePairVerify(t)
	if exitCodeOf(err) != 0 || !strings.Contains(out, "verify reproduced") {
		t.Fatalf("a source that got the same 403 is reproduced:\n%s", out)
	}
}

func TestVerdictCarriesTheRefusalBesideTheEnvelope(t *testing.T) {
	source := verdictOf(&runner.StepRecord{ID: "s", Status: runner.StatusPassed,
		Response: json.RawMessage(`{"error":{"code":"REJECTED","message":"qty must be greater than zero","details":[{"app_code":1203,"reason":"InvalidQty"}]}}`)})
	replay := verdictOf(&runner.StepRecord{ID: "s", Status: runner.StatusPassed,
		Response: json.RawMessage(`{"error":{"code":"REJECTED","message":"qty must be greater than zero","details":[{"app_code":1603,"reason":"PermissionDenied"}]}}`)})
	diffs := strings.Join(chain.CompareVerdicts(source, replay), "\n")
	if !strings.Contains(diffs, "1203") || !strings.Contains(diffs, "PermissionDenied") {
		t.Fatalf("app_code and reason must be compared, got %q", diffs)
	}
}
