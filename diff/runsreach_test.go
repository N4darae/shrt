package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestRunDiffMasksIdsNamedWithAnIDPrefix(t *testing.T) {
	a := runOf("run-a", stepAs("create", runner.StatusPassed, `{"product":{"id_product":"prd-1","idOrder":"o-1","qty":1}}`))
	b := runOf("run-b", stepAs("create", runner.StatusPassed, `{"product":{"id_product":"prd-2","idOrder":"o-2","qty":1}}`))
	if rep := diff.CompareRuns(a, b); !rep.Same() || rep.Masked != 2 {
		t.Fatalf("id_product and idOrder differ every run and must be masked, got %s", rep.Text())
	}
}

func TestRunDiffTreatsAStepHeldBackByKeepGoingAsNotReached(t *testing.T) {
	a := runOf("run-a",
		stepAs("create", runner.StatusPassed, `{"total":5}`),
		stepAs("confirm", runner.StatusPassed, `{"order":{"total":5}}`))
	b := runOf("run-b",
		stepAs("create", runner.StatusFailed, `{"total":4}`),
		&runner.StepRecord{ID: "confirm", Status: runner.StatusSkipped})
	rep := diff.CompareRuns(a, b)
	if len(rep.NoLongerReached) != 1 || rep.NoLongerReached[0] != "confirm" {
		t.Fatalf("a skipped step was not reached, want it listed as such, got %s", rep.Text())
	}
	for _, c := range rep.Changes {
		if c.Step == "confirm" {
			t.Fatalf("a step that was never sent has no response to compare, got %+v", c)
		}
	}
}

func TestRunDiffAppliesTheCurrentVolatilePatterns(t *testing.T) {
	a := runOf("run-a", stepAs("create", runner.StatusPassed, `{"sku":"PROBE-1","qty":1}`))
	b := runOf("run-b", stepAs("create", runner.StatusPassed, `{"sku":"PROBE-2","qty":1}`))
	if rep := diff.CompareRuns(a, b); rep.Same() {
		t.Fatal("sku differs and nothing declares it volatile")
	}
	rep := diff.CompareRunsMasking(a, b, []string{"**.sku"})
	if !rep.Same() || !strings.Contains(rep.Text(), "no differences") {
		t.Fatalf("a pattern added to the config after the runs were recorded must still mask, got %s", rep.Text())
	}
}

func TestRunDiffNamesBuildsAndVarsThatDifferWithoutCallingThemDifferences(t *testing.T) {
	a := runOf("run-a", stepAs("create", runner.StatusPassed, `{"qty":1}`))
	b := runOf("run-b", stepAs("create", runner.StatusPassed, `{"qty":1}`))
	a.Build, b.Build = "baseline", "regressed"
	a.Vars = map[string]any{"tag": "t1", "total": 4100}
	b.Vars = map[string]any{"tag": "t2", "total": 4100}
	rep := diff.CompareRuns(a, b)
	if !rep.Same() {
		t.Fatalf("builds and vars are context, not response differences: %s", rep.Text())
	}
	text := rep.Text()
	if !strings.Contains(text, "builds differ: A baseline, B regressed") || !strings.Contains(text, "tag a=t1 b=t2") || strings.Contains(text, "total a=") {
		t.Fatalf("want the builds and only the changed var named, got %s", text)
	}
}

func TestRunDiffSaysWhichSideRanWithKeepGoing(t *testing.T) {
	a := &runner.Record{RunID: "a", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
		{ID: "one", Status: runner.StatusFailed},
	}}
	b := &runner.Record{RunID: "b", Chain: "c", Status: runner.StatusFailed, KeepGoing: true, Steps: []*runner.StepRecord{
		{ID: "one", Status: runner.StatusFailed},
		{ID: "two", Status: runner.StatusPassed},
	}}
	text := diff.CompareRuns(a, b).Text()
	if !strings.Contains(text, "run B used -keep-going and run A did not") {
		t.Fatalf("a reach difference caused by -keep-going must say so:\n%s", text)
	}
}

func TestVerifyDoesNotCountIdsThatDifferEveryRun(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "a", Steps: []*runner.StepRecord{
		{ID: "create", Call: "X/Create", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"product":{"id_product":"prd-1","sku":"sku-1","qty":4}}`)},
	}}
	rec := &runner.Record{RunID: "b", Chain: "c", Steps: []*runner.StepRecord{
		{ID: "create", Call: "X/Create", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"product":{"id_product":"prd-2","sku":"sku-2","qty":5}}`)},
	}}
	rep := diff.CompareMasking(spot, rec, []string{"product.sku"})
	if len(rep.Changes) != 1 || rep.Changes[0].Path != "product.qty" {
		t.Fatalf("only the qty is a real change: the id is id-shaped and the sku is declared volatile, got %+v", rep.Changes)
	}
	if rep.Masked != 1 || !strings.Contains(rep.Text(), "1 id- or timestamp-shaped value(s)") {
		t.Fatalf("the report must say how many id-shaped values it did not count:\n%s", rep.Text())
	}
}
