package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunAdvisesRaisingTheTimeoutWhenAStepGotNoAnswerInTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Fetch") {
			time.Sleep(400 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "widget"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	raw, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/config.yaml", strings.Replace(string(raw), "base_url: "+srv.URL+"\n", "base_url: "+srv.URL+"\n    timeout: 100ms\n", 1))
	var rerr error
	out := captureStdout(t, func() { rerr = runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
	if exitCodeOf(rerr) != 3 {
		t.Fatalf("a call with no answer is an error run, exit 3: %v\n%s", rerr, out)
	}
	if !strings.Contains(out, "raise target.timeout") {
		t.Fatalf("run must give the advice verify gives for a timeout:\n%s", out)
	}
}
