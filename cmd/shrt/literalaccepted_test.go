package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestALiteralEarlierRunsSentAndHadAcceptedIsNotASelfCollision(t *testing.T) {
	var created atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if created.Add(1) > 3 {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "REJECTED", "message": "duplicate record"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "n"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", unnamedConflictChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	captureStdout(t, func() {
		for range 2 {
			if err := runRun(ctx, []string{"cli-unique", "-quiet"}); err != nil {
				t.Fatalf("shrt run: %v", err)
			}
		}
	})
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet"}) })
	if err == nil {
		t.Fatalf("a refused create does not verify clean\n%s", out)
	}
	text := out + err.Error()
	if strings.Contains(text, "CHAIN DEFECT") || strings.Contains(text, "collides with itself") {
		t.Fatalf("three recorded runs sent meta.source=fixed-source-ao3 and were accepted, so that literal does not collide: %v\n%s", err, out)
	}
	if !strings.HasPrefix(err.Error(), "regression") {
		t.Fatalf("with no self-collision to blame, the refusal is a change against the safe spot: %v\n%s", err, out)
	}
}
