package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func stepAs(id, status, body string) *runner.StepRecord {
	return &runner.StepRecord{ID: id, Call: "ThingService/Fetch", Status: status, Response: json.RawMessage(body)}
}

func runOf(id string, steps ...*runner.StepRecord) *runner.Record {
	status := runner.StatusPassed
	for _, s := range steps {
		if s.Status != runner.StatusPassed {
			status = s.Status
			break
		}
	}
	return &runner.Record{Chain: "thing-flow", RunID: id, Status: status, Steps: steps}
}

func TestRunDiffMasksIdsAndTimestampsThatDifferEveryRun(t *testing.T) {
	a := runOf("run-a", stepAs("create", runner.StatusPassed,
		`{"id":"thing-1","owner_id":"u-7","created_at":"2026-09-01T10:00:00Z","name":"widget"}`))
	b := runOf("run-b", stepAs("create", runner.StatusPassed,
		`{"id":"thing-2","owner_id":"u-8","created_at":"2026-09-02T11:30:00Z","name":"widget"}`))
	rep := diff.CompareRuns(a, b)
	if !rep.Same() {
		t.Fatalf("only ids and timestamps differ, and those differ every run:\n%s", rep.Text())
	}
	if rep.Masked != 3 {
		t.Errorf("masked %d value(s), want 3, and the report must say how many it hid", rep.Masked)
	}
}

func TestRunDiffHonoursTheRecordsDeclaredVolatilePaths(t *testing.T) {
	a := runOf("run-a", stepAs("fetch", runner.StatusPassed, `{"name":"widget","tag":"T1"}`))
	b := runOf("run-b", stepAs("fetch", runner.StatusPassed, `{"name":"widget","tag":"T2"}`))
	b.Volatile = []string{"**.tag"}
	if rep := diff.CompareRuns(a, b); !rep.Same() {
		t.Fatalf("tag is declared volatile by the record:\n%s", rep.Text())
	}
}

func TestRunDiffReportsStatusFirstFailureUnreachedAndFieldChanges(t *testing.T) {
	a := runOf("run-a",
		stepAs("create", runner.StatusPassed, `{"error":{"code":"OK"},"name":"widget"}`),
		stepAs("fetch", runner.StatusFailed, `{"error":{"code":"NOT_FOUND"}}`),
		stepAs("close", runner.StatusPassed, `{"error":{"code":"OK"}}`),
	)
	b := runOf("run-b",
		stepAs("create", runner.StatusFailed, `{"error":{"code":"OK"},"name":"gadget"}`),
	)
	rep := diff.CompareRuns(a, b)
	if rep.Same() {
		t.Fatal("the two runs differ")
	}
	if rep.FirstFailureA != "fetch" || rep.FirstFailureB != "create" {
		t.Errorf("first failing step: a=%q b=%q, want fetch then create", rep.FirstFailureA, rep.FirstFailureB)
	}
	if strings.Join(rep.NoLongerReached, ",") != "fetch,close" {
		t.Errorf("no longer reached = %v, want fetch and close", rep.NoLongerReached)
	}
	text := rep.Text()
	for _, want := range []string{
		"create", "passed -> failed", "first failing step moved", "fetch", "close",
		"name", "widget", "gadget",
		"two recorded runs", "not a verdict against a confirmed safe spot",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report text lacks %q:\n%s", want, text)
		}
	}
}

func TestRunDiffSaysSoEvenWhenTheRunsMatch(t *testing.T) {
	body := `{"name":"widget"}`
	rep := diff.CompareRuns(runOf("run-a", stepAs("fetch", runner.StatusPassed, body)),
		runOf("run-b", stepAs("fetch", runner.StatusPassed, body)))
	if !rep.Same() {
		t.Fatal(rep.Text())
	}
	if !strings.Contains(rep.Text(), "not a verdict against a confirmed safe spot") {
		t.Errorf("a clean comparison of two runs is still not a verdict:\n%s", rep.Text())
	}
}
