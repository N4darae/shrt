package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func renameInChainFile(t *testing.T, file string, pairs ...string) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, file, strings.NewReplacer(pairs...).Replace(string(raw)))
}

func TestADroppedRPCIsAFindingInTheFirstRunAfterItsStepWasRenamed(t *testing.T) {
	var drop atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if drop.Load() && r.URL.Path == "/shrt.test.v1.ThingService/Fetch" {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-9", "name": "widget"})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-drop.yaml", dropChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-drop", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-drop", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-drop", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	drop.Store(true)
	var err error
	var coded *exitError
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-drop", "-quiet"}) })
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatalf("a first dropped connection is could-not-verify, exit 3: %v\n%s", err, out)
	}
	renameInChainFile(t, ".shrt/chains/cli-drop.yaml", "- id: fetch\n", "- id: fetch_thing\n")
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-drop", "-quiet"}) })
	if err == nil || errors.As(err, &coded) || !strings.Contains(out, "FINDING: ") {
		t.Fatalf("the previous run sent the renamed step under its old name and was dropped the same way: a finding, exit 1: %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "fetch_thing") || !strings.Contains(err.Error(), "fails this rpc every time while answering others") {
		t.Fatalf("the finding names the step by its new name: %v", err)
	}
}

func TestAReusedFixtureIsFoundInARunThatNamedTheStepOtherwise(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-unique", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-unique", "-note", "unique names"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-unique", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	var err error
	captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=same1"}) })
	if err != nil {
		t.Fatalf("the first verify with a fresh tag is clean: %v", err)
	}
	renameInChainFile(t, ".shrt/chains/cli-unique.yaml", "- id: create\n", "- id: make_thing\n", "${create.id}", "${make_thing.id}")
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=same1"}) })
	if err == nil || !strings.Contains(err.Error(), "fixture reused") || strings.Contains(err.Error(), "no recorded run of this chain used that value") {
		t.Fatalf("the run before the rename sent tag=same1 at the same step: want fixture reused, got %v\n%s", err, out)
	}
}

func TestAFreshTokenRefusalRepeatsAcrossAStepRename(t *testing.T) {
	ctx, e, base := approvedThingFlowRun(t)
	saveResentAndRefusedAtFetch(t, e, base, "20990101T000000Z-resent1")
	renamed := copyRun(t, base, "tmp")
	for _, st := range renamed.Steps {
		if st.ID == "fetch" {
			st.ID = "fetch_thing"
		}
	}
	saveResentAndRefusedAtFetch(t, e, renamed, "20990101T000001Z-resent2")
	renameInChainFile(t, ".shrt/chains/cli-thing-flow.yaml", "- id: fetch\n", "- id: fetch_thing\n")
	var err error
	var coded *exitError
	out := captureStdout(t, func() {
		err = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-run", "20990101T000001Z-resent2"})
	})
	if err == nil || errors.As(err, &coded) || !strings.Contains(out, "FINDING: ") {
		t.Fatalf("the earlier run was refused the same way at the same step under its old name: a finding, exit 1: %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "20990101T000000Z-resent1") {
		t.Fatalf("the finding names the earlier run: %v", err)
	}
}
