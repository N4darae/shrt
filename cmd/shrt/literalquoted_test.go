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

const quotedLiteralChain = `apiVersion: shrt/v1
name: cli-unique
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: n-${vars.tag}
          meta:
              source: fixed-source-ao3
      expect:
          - path: error.code
            equals: OK
`

func TestARefusalQuotingALiteralEarlierRunsHadAcceptedIsARegressionNotAVarCollision(t *testing.T) {
	var created atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if created.Add(1) > 3 {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "REJECTED", "message": "SourceTaken: source fixed-source-ao3 already exists"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(int(created.Load())), "name": "n"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", quotedLiteralChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	captureStdout(t, func() {
		for _, tag := range []string{"a2", "a3"} {
			if err := runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=" + tag}); err != nil {
				t.Fatalf("shrt run: %v", err)
			}
		}
	})
	for _, tag := range []string{"m4", "m5"} {
		var err error
		out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=" + tag}) })
		var coded *exitError
		if err == nil || (errors.As(err, &coded) && coded.code != 1) {
			t.Fatalf("tag=%s: three runs sent meta.source=fixed-source-ao3 and had it accepted, so a refusal quoting it is a backend change, exit 1: %v\n%s", tag, err, out)
		}
		text := out + err.Error()
		for _, wrong := range []string{"fixture collision", "built from tag", "collides with itself", "CHAIN DEFECT"} {
			if strings.Contains(text, wrong) {
				t.Fatalf("tag=%s: the refusal quotes the literal source, not the var-built name, so %q is wrong: %v\n%s", tag, wrong, err, out)
			}
		}
		if !strings.HasPrefix(err.Error(), "regression") {
			t.Fatalf("tag=%s: want a regression against the safe spot, got %v\n%s", tag, err, out)
		}
	}
}
