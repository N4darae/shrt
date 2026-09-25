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

const referencedRepeatChain = `apiVersion: shrt/v1
name: cli-repeat
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: w-${vars.tag}
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: create_2
      call: ThingService/Create
      body:
          name: w2-${vars.tag}
          idempotency_key: ${steps.create.request.idempotency_key}
      expect:
          - path: error.code
            equals: OK
`

const repeatedValueChain = `apiVersion: shrt/v1
name: cli-repeat
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: w-${vars.tag}
      expect:
          - path: error.code
            equals: OK
    - id: create_2
      call: ThingService/Create
      body:
          name: w-${vars.tag}
      expect:
          - path: error.code
            equals: OK
`

func TestAnInRunRepeatTheSafeSpotHadAcceptedIsARegressionNotAChainDefect(t *testing.T) {
	for _, tc := range []struct {
		name, chain, refusal string
	}{
		{"deliberate reference", referencedRepeatChain, "DuplicateKey: idempotency key already exists"},
		{"same value, accepted before", repeatedValueChain, "NameTaken: name already exists"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				n := calls.Add(1)
				if n > 2 && n%2 == 0 {
					_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "REJECTED", "message": tc.refusal}})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "n"})
			}))
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			writeFile(t, ".shrt/chains/cli-repeat.yaml", tc.chain)
			ctx := context.Background()
			captureStdout(t, func() {
				if err := runRun(ctx, []string{"cli-repeat", "-quiet"}); err != nil {
					t.Fatalf("shrt run: %v", err)
				}
				if err := runConfirm(ctx, []string{"cli-repeat", "-note", "repeat accepted"}); err != nil {
					t.Fatalf("propose: %v", err)
				}
				if err := runConfirm(ctx, []string{"cli-repeat", "-approve", "-by", "alice@example.test"}); err != nil {
					t.Fatalf("approve: %v", err)
				}
			})
			var err error
			out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-repeat", "-quiet", "-var", "tag=v2"}) })
			var coded *exitError
			if err == nil || (errors.As(err, &coded) && coded.code != 1) {
				t.Fatalf("the safe spot's run had the repeat accepted, so its refusal now is a regression, exit 1: %v\n%s", err, out)
			}
			text := out + err.Error()
			if strings.Contains(text, "CHAIN DEFECT") || strings.Contains(text, "collides with itself") {
				t.Fatalf("the approved safe spot proves this in-run repeat used to be accepted, so it is no chain defect: %v\n%s", err, out)
			}
			if !strings.HasPrefix(err.Error(), "regression") {
				t.Fatalf("want a regression against the safe spot, got %v\n%s", err, out)
			}
		})
	}
}
