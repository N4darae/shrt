package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func keepGoingChain(t *testing.T) *chain.Chain {
	t.Helper()
	return normalized(t, &chain.Chain{Name: "keep-going", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create",
			Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "id", Equals: "deliberately-wrong"}},
			Export: map[string]string{"thing_id": "id"}},
		{ID: "fetch_by_step", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${create.id}"}, Expect: okExpect()},
		{ID: "fetch_by_export", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${exports.thing_id}"}, Expect: okExpect()},
		{ID: "create_other", Call: "ThingService/Create",
			Body: map[string]any{"name": "gadget", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "fetch_other", Call: "ThingService/Fetch",
			Body:   map[string]any{"id": "${create_other.id}"},
			Expect: []chain.Expectation{{Path: "name", Equals: "not-a-widget"}}},
		{ID: "after_skipped", Call: "ThingService/Fetch",
			Body: map[string]any{"id": "${steps.fetch_by_step.response.id}"}, Expect: okExpect()},
	}})
}

func TestWithoutKeepGoingARunStillStopsAtTheFirstFailure(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newRunner(t, srv).Run(context.Background(), keepGoingChain(t), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rec.Steps) != 1 || rec.Status != runner.StatusFailed {
		t.Fatalf("want the run to stop failed after step 1, got %d steps, status %s", len(rec.Steps), rec.Status)
	}
	if rec.KeepGoing {
		t.Error("keep_going must be false when the option was not given")
	}
}

func TestKeepGoingRunsPastFailedExpectationsAndSkipsTheirDependents(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newRunner(t, srv).Run(context.Background(), keepGoingChain(t), runner.Options{KeepGoing: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Status != runner.StatusFailed {
		t.Fatalf("a keep-going run with failed steps must still be failed, got %s", rec.Status)
	}
	if !rec.KeepGoing {
		t.Error("the record must say it was a keep-going run, or a reader assumes it stopped at the first red")
	}
	if len(rec.Steps) != 6 {
		t.Fatalf("want every step recorded, got %d", len(rec.Steps))
	}
	want := map[string]string{
		"create":          runner.StatusFailed,
		"fetch_by_step":   runner.StatusSkipped,
		"fetch_by_export": runner.StatusSkipped,
		"create_other":    runner.StatusPassed,
		"fetch_other":     runner.StatusFailed,
		"after_skipped":   runner.StatusSkipped,
	}
	for id, status := range want {
		if got := stepByID(t, rec, id).Status; got != status {
			t.Errorf("step %s: status %s, want %s", id, got, status)
		}
	}
	for _, id := range []string{"fetch_by_step", "fetch_by_export", "after_skipped"} {
		sr := stepByID(t, rec, id)
		if !strings.Contains(sr.Error, "not sent") || sr.Request != nil {
			t.Errorf("step %s depends on a step that did not pass: it must not be sent, and must say so; error=%q request=%s", id, sr.Error, sr.Request)
		}
	}
	if !strings.Contains(stepByID(t, rec, "after_skipped").Error, "fetch_by_step") {
		t.Errorf("a step skipped behind a skipped step must name the step it waited on, got %q", stepByID(t, rec, "after_skipped").Error)
	}

	srv.mu.Lock()
	fetches := 0
	for _, p := range srv.calls {
		if p == "/shrt.test.v1.ThingService/Fetch" {
			fetches++
		}
	}
	srv.mu.Unlock()
	if fetches != 1 {
		t.Fatalf("only fetch_other may reach the server, got %d Fetch calls", fetches)
	}

	wantFailed := []string{"create", "fetch_by_step", "fetch_by_export", "fetch_other", "after_skipped"}
	if strings.Join(rec.FailedSteps, ",") != strings.Join(wantFailed, ",") {
		t.Errorf("failed_steps = %v, want %v", rec.FailedSteps, wantFailed)
	}
	for _, id := range wantFailed {
		if !strings.Contains(rec.Failure, `"`+id+`"`) {
			t.Errorf("the failure summary must name every step that did not pass, missing %s in %q", id, rec.Failure)
		}
	}
}

func TestKeepGoingContinuesPastAStepThatWasNeverSent(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := normalized(t, &chain.Chain{Name: "keep-going-error", Steps: []*chain.Step{
		{ID: "broken", Call: "ThingService/Create",
			Body: map[string]any{"name": "${vars.missing}", "kind": "KIND_A"}, Expect: okExpect()},
		{ID: "independent", Call: "ThingService/Create",
			Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{KeepGoing: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Status != runner.StatusError {
		t.Fatalf("the run's verdict must be the first failure's, as without -keep-going: want %s, got %s", runner.StatusError, rec.Status)
	}
	if got := stepByID(t, rec, "independent").Status; got != runner.StatusPassed {
		t.Fatalf("a step that reads nothing from the broken one should still run, got %s", got)
	}
}
