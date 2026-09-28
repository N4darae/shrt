package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/store"
)

func editLatestRun(t *testing.T, chainName, from, to string) string {
	t.Helper()
	e, err := loadEnv(true)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := e.store.ListRuns(chainName)
	if err != nil || len(ids) == 0 {
		t.Fatalf("no runs: %v", err)
	}
	path := filepath.Join(".shrt", "runs", chainName, ids[len(ids)-1]+".json")
	raw := string(mustRead(t, path))
	edited := strings.Replace(raw, from, to, 1)
	if edited == raw {
		t.Fatalf("fixture edit %q did not apply", from)
	}
	writeFile(t, path, edited)
	return ids[len(ids)-1]
}

func TestCLIAHandEditedRunRecordIsNotEvidence(t *testing.T) {
	approvedThingFlow(t)
	captureStdout(t, func() {
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
	id := editLatestRun(t, "cli-thing-flow", `"name": "widget"`, `"name": "gadget"`)

	var err error
	captureStdout(t, func() { err = runVerify(context.Background(), []string{"cli-thing-flow", "-run", id}) })
	if !errors.Is(err, store.ErrRunEdited) {
		t.Fatalf("verify -run of an edited record must refuse it, not diff it: %v", err)
	}
	captureStdout(t, func() { err = runDiff(context.Background(), []string{"cli-thing-flow"}) })
	if !errors.Is(err, store.ErrRunEdited) {
		t.Fatalf("diff of an edited record must refuse it: %v", err)
	}
	e, lerr := loadEnv(true)
	if lerr != nil {
		t.Fatal(lerr)
	}
	ids, _ := e.store.ListRuns("cli-thing-flow")
	captureStdout(t, func() { err = runDiff(context.Background(), []string{ids[0], id}) })
	if !errors.Is(err, store.ErrRunEdited) {
		t.Fatalf("diff by run id of an edited record must refuse it: %v", err)
	}
	captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-thing-flow", "-step", "fetch", "-run", id})
	})
	if !errors.Is(err, store.ErrRunEdited) {
		t.Fatalf("slice from an edited record must refuse it: %v", err)
	}
	var out string
	out = captureStdout(t, func() { err = chainHollow(nil) })
	if !strings.Contains(out, "changed after shrt wrote them") {
		t.Fatalf("hollow must say it did not count an edited record:\n%s", out)
	}
}
