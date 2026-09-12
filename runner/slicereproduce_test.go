package runner_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func sliceableChain() *chain.Chain {
	c := &chain.Chain{
		Name: "slice-repro",
		Steps: []*chain.Step{
			{
				ID:   "create_subject",
				Call: "ThingService/Create",
				Body: map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"},
				Expect: []chain.Expectation{
					{Path: "error.code", Equals: "OK"},
					{Path: "id", NotEmpty: true},
				},
			},
			{
				ID:   "create_noise",
				Call: "ThingService/Create",
				Body: map[string]any{"name": "unrelated", "kind": "KIND_A", "idempotency_key": "${uuid}"},
				Expect: []chain.Expectation{
					{Path: "error.code", Equals: "OK"},
				},
			},
			{
				ID:   "fetch_subject",
				Call: "ThingService/Fetch",
				Body: map[string]any{"id": "${create_subject.id}"},
				Expect: []chain.Expectation{
					{Path: "error.code", Equals: "OK"},
					{Path: "name", Equals: "widget"},
				},
			},
		},
	}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func verdictFor(t *testing.T, rec *runner.Record, id string) chain.Verdict {
	t.Helper()
	sr, ok := rec.Step(id)
	if !ok {
		t.Fatalf("run %s has no step %q", rec.RunID, id)
	}
	var response any
	if len(sr.Response) > 0 {
		if err := json.Unmarshal(sr.Response, &response); err != nil {
			t.Fatal(err)
		}
	}
	code := ""
	if v, ok := chain.Get(response, "error.code"); ok {
		code = fmt.Sprint(v)
	}
	return chain.Verdict{Step: sr.ID, Status: sr.Status, ErrorCode: code, Expect: sr.Expect}
}

func TestSliceReproducesTheTargetStepVerdictAgainstARealRunner(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	r := newRunner(t, srv)
	c := sliceableChain()

	source, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !source.Passed() {
		t.Fatalf("the source run must pass before a slice of it means anything: %s", source.Failure)
	}

	res, err := chain.Slice(c, "fetch_subject", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Kept) != 2 {
		t.Fatalf("kept %d steps, want create_subject and fetch_subject", len(res.Kept))
	}

	replay, err := r.Run(context.Background(), res.Chain, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	diffs := chain.CompareVerdicts(verdictFor(t, source, "fetch_subject"), verdictFor(t, replay, "fetch_subject"))
	if len(diffs) != 0 {
		t.Fatalf("the slice must reproduce the target step's verdict, got %v", diffs)
	}
	if len(replay.Steps) >= len(source.Steps) {
		t.Fatalf("the slice ran %d steps and the source ran %d: a slice that saves no calls saves nothing",
			len(replay.Steps), len(source.Steps))
	}
}

func TestSliceVerifyReportsADivergentTargetVerdict(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	r := newRunner(t, srv)
	c := sliceableChain()

	source, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := chain.Slice(c, "fetch_subject", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	srv.setDrift("something-else")
	replay, err := r.Run(context.Background(), res.Chain, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	diffs := chain.CompareVerdicts(verdictFor(t, source, "fetch_subject"), verdictFor(t, replay, "fetch_subject"))
	if len(diffs) == 0 {
		t.Fatal("the backend answered differently and the comparison called it reproduced")
	}
}
