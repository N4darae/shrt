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

const whichBaselinedChain = `apiVersion: shrt/v1
name: cli-which-baselined
steps:
    - id: denied
      call: ThingService/Fetch
      body:
          id: thing-1
      expect:
          - path: error.message
            equals: NotYours
`

func TestWhichCodeCountsASiblingAssertionAndTheSafeSpotBaseline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "1603", "message": "NotYours"}, "name": "widget"})
	}))
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	raw, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/config.yaml", string(raw)+"conventions:\n    envelope_path: name\n    envelope_ok: widget\n    code_fields: [code, message]\n")
	writeFile(t, ".shrt/chains/cli-which-baselined.yaml", whichBaselinedChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-which-baselined", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
	out := whichOut(t, "-code", "1603")
	if !strings.Contains(out, "error.message equals NotYours") {
		t.Errorf("the step asserts the sibling detail of the same refusal, and which must say so:\n%s", out)
	}
	captureStdout(t, func() {
		if err := runConfirm(ctx, []string{"cli-which-baselined", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-which-baselined", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	out = whichOut(t, "-code", "1603")
	if strings.Contains(out, "goes unnoticed") {
		t.Errorf("the safe spot baselines error.code, so shrt verify would catch a change:\n%s", out)
	}
	if !strings.Contains(out, "safe spot") || !strings.Contains(out, "shrt verify") {
		t.Errorf("which must say the safe spot baselines the code and verify compares it:\n%s", out)
	}
}
