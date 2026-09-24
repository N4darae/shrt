package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const twoCreatesChain = `apiVersion: shrt/v1
name: cli-two
steps:
    - id: first
      call: ThingService/Create
      body:
          name: one
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: second
      call: ThingService/Create
      body:
          name: two
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: third
      call: ThingService/Create
      body:
          name: three
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

func TestAGatewayAnswerIsNotAStepThatGotAnAnswer(t *testing.T) {
	var down atomic.Bool
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if down.Load() {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("<html>gateway</html>"))
			return
		}
		next++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": body["name"]})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-two.yaml", twoCreatesChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-two", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-two", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-two", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	down.Store(true)
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-two", "-quiet"}) })
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatalf("every call answered by a gateway is could-not-verify, exit 3, got %v\n%s", err, out)
	}
	if strings.Contains(err.Error(), "got an answer were compared") || !strings.Contains(err.Error(), "nothing after it got an answer") {
		t.Fatalf("a step a gateway answered for got no answer from the service, so nothing after the first one was compared: %v", err)
	}
}
