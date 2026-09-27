package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAFailedAbsenceNamesTheValueThatWasPresent(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	c := normalized(t, &chain.Chain{Name: "exists-holds", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create",
			Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "id", Exists: ptrBool(false)}}},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := chain.DescribeFailure(stepByID(t, rec, "create").Expect[0])
	if !strings.Contains(got, `want absent got present (holds "thing-`) {
		t.Errorf("a failed exists: false shows the value it found: %s", got)
	}
}
