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

const twoUUIDFieldsChain = `apiVersion: shrt/v1
name: cli-unique
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: n-${uuid}
          meta:
              source: s-${uuid}
      expect:
          - path: error.code
            equals: OK
`

func TestARefusalOverAnotherFieldIsNotTheSameWay(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		name, _ := body["name"].(string)
		meta, _ := body["meta"].(map[string]any)
		source, _ := meta["source"].(string)
		switch calls.Add(1) {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "n"})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "SOURCE_TAKEN", "message": "source " + source + " already exists"}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "NAME_TAKEN", "message": "name " + name + " already exists"}})
		}
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", twoUUIDFieldsChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	var err error
	var coded *exitError
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet"}) })
	if !errors.As(err, &coded) || coded.code != 3 || !strings.Contains(err.Error(), "meta.source=") {
		t.Fatalf("a first conflict on source is a fixture collision, exit 3: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet"}) })
	if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out, "FINDING") {
		t.Fatalf("the previous run was refused over source (SOURCE_TAKEN), this one over name (NAME_TAKEN): not the same way, so no finding: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet"}) })
	if err == nil || errors.As(err, &coded) || !strings.Contains(out, "FINDING: ") {
		t.Fatalf("two runs refused over name with NAME_TAKEN are the same way, a finding, exit 1: %v\n%s", err, out)
	}
}
