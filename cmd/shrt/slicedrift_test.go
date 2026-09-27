package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTotallingBackend(total *int) *httptest.Server {
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next)})
		case "/shrt.test.v1.ThingService/Fetch":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "widget", "total": *total})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestSliceVerifyOfADriftOnlyStepComparesWhatItChanged(t *testing.T) {
	total := 2
	srv := newTotallingBackend(&total)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	total = 1
	for range 2 {
		captureStdout(t, func() { _ = runVerify(ctx, []string{"cli-thing-flow", "-quiet"}) })
	}
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	ids, _ := e.store.ListRuns("cli-thing-flow")
	newest := ids[len(ids)-1]
	slice := func() (string, string, error) {
		var err error
		var out string
		note := captureStderr(t, func() {
			out = captureStdout(t, func() {
				err = chainSlice(ctx, []string{"cli-thing-flow", "-step", "fetch", "-run", "latest", "-verify", "-repeat", "1"})
			})
		})
		return out, note, err
	}
	out, note, err := slice()
	if err != nil || !strings.Contains(out, "source run "+newest+" (a shrt verify replay)") || !strings.Contains(out, "the slice changed what source run "+newest+" changed") {
		t.Fatalf("-run latest slices the newest replay, and the slice reproduces its drift: %v\n%s", err, out)
	}
	if !strings.Contains(note, "as shrt diff picks it") {
		t.Fatalf("the note says why the replay was picked: %q", note)
	}
	total = 2
	out, _, err = slice()
	if exitCodeOf(err) != 1 || !strings.Contains(out, "NOT REPRODUCED") || !strings.Contains(out, "total (") || !strings.Contains(out, "the slice run did not") {
		t.Fatalf("a slice that passes without the drift did not reproduce it: %v\n%s", err, out)
	}
}
