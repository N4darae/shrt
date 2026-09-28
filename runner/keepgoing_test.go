package runner_test

import (
	"net"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestKeepGoingRunsPastFailuresAndHoldsBackOnlyWhatReadsThem(t *testing.T) {
	const (
		passed  = runner.StatusPassed
		failed  = runner.StatusFailed
		skipped = runner.StatusSkipped
	)
	wrong := chain.Expectation{Path: "id", Equals: "deliberately-wrong"}
	create := func(id string, export map[string]string, expect ...chain.Expectation) *chain.Step {
		return &chain.Step{ID: id, Call: "ThingService/Create", Body: thing("widget"), Export: export, Expect: expect}
	}
	fetch := func(id, ref string) *chain.Step { return step(id, "ThingService/Fetch", byID(ref), okExpect()...) }
	for _, tc := range []struct {
		name   string
		setup  func(*fakeServer)
		c      *chain.Chain
		status map[string]string
		errs   map[string]string
		check  func(*testing.T, *runner.Record, *fakeServer)
	}{
		{name: "dependents of failed steps are skipped unsent", c: flow(
			create("create", map[string]string{"thing_id": "id"}, wrong),
			fetch("fetch_by_step", "${create.id}"),
			fetch("fetch_by_export", "${exports.thing_id}"),
			step("create_other", "ThingService/Create", thing("gadget"), okExpect()...),
			step("fetch_other", "ThingService/Fetch", byID("${create_other.id}"), chain.Expectation{Path: "name", Equals: "not-a-widget"}),
			fetch("after_skipped", "${steps.fetch_by_step.response.id}")),
			status: map[string]string{"create": failed, "fetch_by_step": skipped, "fetch_by_export": skipped, "create_other": passed, "fetch_other": failed, "after_skipped": skipped},
			errs:   map[string]string{"fetch_by_step": "not sent", "fetch_by_export": "not sent", "after_skipped": "fetch_by_step"},
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				fetches := 0
				for _, p := range f.calls {
					if p == "/shrt.test.v1.ThingService/Fetch" {
						fetches++
					}
				}
				wantFailed := "create,fetch_by_step,fetch_by_export,fetch_other,after_skipped"
				if fetches != 1 || !rec.KeepGoing || strings.Join(rec.FailedSteps, ",") != wantFailed || stepByID(t, rec, "fetch_by_step").Request != nil {
					t.Fatalf("only fetch_other is sent, the record says keep_going and lists every failed step: %d %v %v", fetches, rec.KeepGoing, rec.FailedSteps)
				}
				for _, id := range rec.FailedSteps {
					if !strings.Contains(rec.Failure, `"`+id+`"`) {
						t.Errorf("the failure summary names %s: %q", id, rec.Failure)
					}
				}
			}},
		{name: "a step never sent does not stop an independent one", c: flow(
			step("seed", "ThingService/Create", thing("widget"), okExpect()...),
			step("broken", "ThingService/Create", map[string]any{"name": "widget", "kind": "${seed.response.id}"}, okExpect()...),
			step("independent", "ThingService/Create", thing("widget"), okExpect()...)),
			status: map[string]string{"independent": passed}},
		{name: "a step reading a failed step only in an expectation is sent", c: flow(
			create("create", nil, wrong),
			create("create_other", nil, chain.Expectation{Path: "id", NotEqual: "${create.id}"}, chain.Expectation{Path: "id", NotEmpty: true})),
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				other := rec.Steps[1]
				if other.Status == skipped || len(other.Expect) != 2 {
					t.Fatalf("create_other's body does not read create: %s %+v", other.Status, other.Expect)
				}
				held, own := other.Expect[0], other.Expect[1]
				if held.Rule != "unevaluated" || held.Passed || !strings.Contains(held.Detail, `"create"`) || held.Got == nil || held.Got != own.Got || !own.Passed {
					t.Fatalf("the held expectation is unevaluated naming create and shows what was answered; its own still runs: %+v %+v", held, own)
				}
			}},
		{name: "a step reading a field the failed step did not fail on is sent", c: flow(
			create("create", map[string]string{"thing_id": "id", "thing_name": "name"}, chain.Expectation{Path: "id", NotEmpty: true}, chain.Expectation{Path: "name", Equals: "deliberately-wrong"}),
			fetch("fetch_by_id", "${create.id}"),
			fetch("fetch_by_bare_export", "${thing_id}"),
			fetch("fetch_by_export", "${exports.thing_id}"),
			fetch("fetch_by_name", "${create.name}"),
			fetch("fetch_by_failed_export", "${thing_name}")),
			status: map[string]string{"fetch_by_id": passed, "fetch_by_bare_export": passed, "fetch_by_export": passed, "fetch_by_name": skipped, "fetch_by_failed_export": skipped}},
		{name: "a step reading only a failed step's request is sent", c: flow(
			&chain.Step{ID: "create_order", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "k-1"},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}, wrong}},
			step("create_order_again", "ThingService/Create", map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${steps.create_order.request.idempotency_key}"}, okExpect()...),
			fetch("fetch", "${create_order.id}")),
			status: map[string]string{"create_order_again": passed, "fetch": skipped},
			errs:   map[string]string{"fetch": "failed its assertion on id"},
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if strings.Contains(stepByID(t, rec, "fetch").Error, "refused call") {
					t.Fatalf("create_order was answered, not refused")
				}
			}},
		{name: "a refusal is named as a refusal", c: flow(skipAuth(fetch("probe", "x")), fetch("next", "${probe.id}")),
			errs: map[string]string{"next": "was refused before a response body existed (transport unauthenticated)"}},
		{name: "a skip reason shared by steps is given once", setup: refusedCreate("out_of_stock"), c: flow(
			step("create", "ThingService/Create", thing("w"), okExpect()...), fetch("fetch_one", "${create.id}"), fetch("fetch_two", "${create.id}")),
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if strings.Count(rec.Failure, "was refused in-band") != 1 || !strings.Contains(rec.Failure, `"fetch_two"`) {
					t.Fatalf("the reason is said once and every skipped step listed:\n%s", rec.Failure)
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServer()
			t.Cleanup(f.Close)
			if tc.setup != nil {
				tc.setup(f)
			}
			rec := run(t, newRunner(t, f), normalized(t, tc.c), runner.Options{KeepGoing: true})
			if rec.Status != failed {
				t.Fatalf("the run's verdict is the first failure's, got %s", rec.Status)
			}
			for id, want := range tc.status {
				if got := stepByID(t, rec, id); got.Status != want {
					t.Errorf("step %s: status %s, want %s (%s)", id, got.Status, want, got.Error)
				}
			}
			for id, want := range tc.errs {
				if got := stepByID(t, rec, id).Error; !strings.Contains(got, want) {
					t.Errorf("step %s: error %q lacks %q", id, got, want)
				}
			}
			if tc.check != nil {
				tc.check(t, rec, f)
			}
		})
	}
}

func TestAFailedStepLeadsWithItsFailedExpectationNotTheExportItCouldNotRead(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	c := flow(&chain.Step{ID: "create", Call: "ThingService/Create", Body: thing("widget"),
		Expect: []chain.Expectation{{Path: "id", Equals: "deliberately-wrong"}}, Export: map[string]string{"thing_ref": "no_such_field"}})
	got := run(t, newRunner(t, srv), normalized(t, c), runner.Options{}).Steps[0].Error
	if !strings.HasPrefix(got, "expectation failed: id want=deliberately-wrong") || !strings.Contains(got, `export "thing_ref"`) {
		t.Fatalf("the cause is the failed expectation and must come first, got %q", got)
	}
}

func TestAReferenceToAPathTheAnsweredStepLacksFailsTheReferencingStep(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	login := skipAuth(step("login", "AuthService/Login", map[string]any{"username": "alice", "password": "hunter2"}))
	c := flow(login, step("preview", "BatchService/Preview", map[string]any{"lines": []any{}}),
		step("use", "BatchService/Preview", map[string]any{"lines": []any{"${preview.results.0.amount}"}}))
	rec := run(t, newBatchRunner(t, srv), normalized(t, c), runner.Options{})
	use := stepByID(t, rec, "use")
	if rec.Status != runner.StatusFailed || use.Status != runner.StatusFailed || !strings.Contains(use.Error, `step "preview" answered without results.0.amount (results is [])`) {
		t.Fatalf("a path the recorded response lacks will be lacking on every re-run: %s %s %q", rec.Status, use.Status, use.Error)
	}
}

func TestKeepGoingStopsSendingOnceTheTargetIsUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target := "http://" + ln.Addr().String()
	ln.Close()
	r := buildRunner(t, testConfig(target), catalogtest.New())
	seen := 0
	r.OnStep = func(*runner.StepRecord) { seen++ }
	c := flow(step("a", "ThingService/Create", thing("x"), okExpect()...), step("b", "ThingService/Create", thing("y"), okExpect()...), step("c", "ThingService/Create", thing("z"), okExpect()...))
	rec := run(t, r, normalized(t, c), runner.Options{KeepGoing: true})
	if rec.Steps[0].Status != runner.StatusError || rec.Status != runner.StatusError {
		t.Fatalf("the first step dies on the dial, got %s / run %s", rec.Steps[0].Status, rec.Status)
	}
	for _, sr := range rec.Steps[1:] {
		if sr.Status != runner.StatusSkipped || sr.Request != nil || !sr.NotSentUnreachable() || !strings.Contains(sr.Error, target) || !strings.Contains(sr.Error, "unreachable") {
			t.Fatalf("step %s must be skipped unsent naming the unreachable target, got %s %q", sr.ID, sr.Status, sr.Error)
		}
	}
	if seen != 3 || len(rec.FailedSteps) != 3 || strings.Count(rec.Failure, "unreachable") != 1 {
		t.Fatalf("every step is recorded and the summary names the dead target once: seen=%d failed=%v %q", seen, rec.FailedSteps, rec.Failure)
	}
}
