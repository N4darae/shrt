package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const generatedFlowChain = `apiVersion: shrt/v1
name: cli-generated
steps:
    - id: create
      call: ThingService/Create
      body:
          name: m-${uuid}@example.test
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch_missing
      call: ThingService/Fetch
      body:
          id: ${uuid}
      expect:
          - path: error.code
            equals: NOT_FOUND
    - id: fetch_prefixed
      call: ThingService/Fetch
      body:
          id: thing-${uuid}
      expect:
          - path: error.code
            equals: NOT_FOUND
`

func newEchoingBackend(stale *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			name := body["name"]
			if *stale != "" {
				name = *stale
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": name})
		case "/shrt.test.v1.ThingService/Fetch":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "NOT_FOUND", "message": "no thing " + body["id"].(string)}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestVerifyMasksEchoesOfGeneratedValuesLikeFixtureNames(t *testing.T) {
	stale := ""
	srv := newEchoingBackend(&stale)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-generated.yaml", generatedFlowChain)
	ctx := context.Background()
	var first string
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-generated", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-generated", "-note", "refusals and echo checked"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-generated", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	spot, err := loadSpotName(t, "cli-generated")
	if err != nil {
		t.Fatal(err)
	}
	first = spot
	var verr error
	out := captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-generated", "-quiet"}) })
	if verr != nil || !strings.Contains(out, "no drift") {
		t.Fatalf("every difference only echoes a fresh ${uuid}, which is no more input than a fixture name; want no drift, got %v:\n%s", verr, out)
	}
	stale = first
	out = captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-generated", "-quiet"}) })
	if verr == nil || !strings.Contains(out, "create") || !strings.Contains(out, "name") {
		t.Fatalf("a response still carrying the confirmed run's generated value is a change, got %v:\n%s", verr, out)
	}
}

func loadSpotName(t *testing.T, chainName string) (string, error) {
	t.Helper()
	e, err := loadEnv(false)
	if err != nil {
		return "", err
	}
	spot, err := e.store.LoadSafeSpot(chainName)
	if err != nil {
		return "", err
	}
	for _, st := range spot.Steps {
		if st.ID == "create" {
			var resp map[string]any
			if err := json.Unmarshal(st.Response, &resp); err != nil {
				return "", err
			}
			name, _ := resp["name"].(string)
			return name, nil
		}
	}
	return "", nil
}
