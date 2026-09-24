package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAnInvalidLaterRequestIsRefusedBeforeAnythingIsSent(t *testing.T) {
	cases := map[string]map[string]any{
		"unknown field": {"name": "widget", "kind": "KIND_A", "bogus": 1},
		"invalid enum":  {"name": "widget", "kind": "KIND_NOPE"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := newFakeServer()
			defer srv.Close()
			r := newRunner(t, srv)
			c := normalized(t, &chain.Chain{Name: "late-invalid", Steps: []*chain.Step{
				{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Export: map[string]string{"thing_id": "id"}, Expect: okExpect()},
				{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${thing_id}"}, Expect: okExpect()},
				{ID: "create_bad", Call: "ThingService/Create", Body: body, Expect: okExpect()},
			}})
			rec, err := r.Run(context.Background(), c, runner.Options{})
			if err == nil {
				t.Fatalf("an invalid request in step 3 must refuse the run before sending, got a record %s %s", rec.Status, rec.Failure)
			}
			if !strings.Contains(err.Error(), "nothing was sent") || !strings.Contains(err.Error(), `"create_bad"`) {
				t.Fatalf("the refusal must name the step and say nothing was sent, got %v", err)
			}
			if len(srv.calls) != 0 {
				t.Fatalf("nothing may reach the backend, got %v", srv.calls)
			}
		})
	}
}

func TestARequestBuiltFromAnEarlierResponseIsValidatedWithSyntheticValues(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	r := newRunner(t, srv)
	c := normalized(t, &chain.Chain{Name: "late-valid", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Export: map[string]string{"thing_id": "id"}, Expect: okExpect()},
		{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${thing_id}"}, Expect: okExpect()},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("a valid chain must run, got %v", err)
	}
	if rec.Status != runner.StatusPassed {
		t.Fatalf("want passed, got %s %s", rec.Status, rec.Failure)
	}
}
