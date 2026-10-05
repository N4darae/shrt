package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
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

func compareRuns(a, b *runner.Record, extra ...string) *diff.RunReport {
	return diff.CompareRunsSkipping(a, b, extra, diff.Fixtures{})
}

func TestRunDiffMasksWhatDiffersEveryRunAndNothingElse(t *testing.T) {
	errored := func(msg string) *runner.StepRecord {
		return &runner.StepRecord{ID: "create", Call: "ThingService/Create", Status: runner.StatusError, Error: msg}
	}
	for _, c := range []struct {
		name     string
		a, b     *runner.StepRecord
		volatile []string
		extra    []string
		same     bool
		masked   int
	}{
		{"ids and timestamps", stepAs("create", runner.StatusPassed, `{"id":"thing-1","owner_id":"u-7","created_at":"2026-09-01T10:00:00Z","name":"widget"}`),
			stepAs("create", runner.StatusPassed, `{"id":"thing-2","owner_id":"u-8","created_at":"2026-09-02T11:30:00Z","name":"widget"}`), nil, nil, true, 3},
		{"id-prefixed names", stepAs("create", runner.StatusPassed, `{"product":{"id_product":"prd-1","idOrder":"o-1","qty":1}}`),
			stepAs("create", runner.StatusPassed, `{"product":{"id_product":"prd-2","idOrder":"o-2","qty":1}}`), nil, nil, true, 2},
		{"record's volatile path", stepAs("fetch", runner.StatusPassed, `{"name":"widget","tag":"T1"}`), stepAs("fetch", runner.StatusPassed, `{"name":"widget","tag":"T2"}`), []string{"**.tag"}, nil, true, 0},
		{"undeclared sku", stepAs("create", runner.StatusPassed, `{"sku":"PROBE-1","qty":1}`), stepAs("create", runner.StatusPassed, `{"sku":"PROBE-2","qty":1}`), nil, nil, false, 0},
		{"sku under a pattern added since", stepAs("create", runner.StatusPassed, `{"sku":"PROBE-1","qty":1}`), stepAs("create", runner.StatusPassed, `{"sku":"PROBE-2","qty":1}`), nil, []string{"**.sku"}, true, 0},
		{"same error text", errored("dial tcp: connection refused"), errored("dial tcp: connection refused"), nil, nil, true, 0},
		{"other error text", errored(`auth login response has no token at "access_token"`), errored("auth login: dial tcp: connection refused"), nil, nil, false, 0},
	} {
		a, b := runOf("a", c.a), runOf("b", c.b)
		b.Volatile = c.volatile
		rep := compareRuns(a, b, c.extra...)
		if rep.Same() != c.same || (c.masked > 0 && rep.Masked != c.masked) {
			t.Errorf("%s: same=%v masked=%d, want %v %d:\n%s", c.name, rep.Same(), rep.Masked, c.same, c.masked, rep.Text())
		}
	}
	text := compareRuns(runOf("a", errored(`no token at "access_token"`)), runOf("b", errored("connection refused"))).Text()
	if !strings.Contains(text, "no token at") || !strings.Contains(text, "connection refused") {
		t.Fatalf("the diff shows both error texts:\n%s", text)
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
	rep := compareRuns(a, b)
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
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report text lacks %q:\n%s", want, text)
		}
	}
}

func TestRunDiffNamesAHeaderAndFoldsAFixtureReference(t *testing.T) {
	run := func(id, tag string, headers map[string]string) *runner.Record {
		return runOf(id,
			&runner.StepRecord{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Headers: map[string]string{},
				Request: json.RawMessage(`{"sku":"sku-` + tag + `-a"}`), Response: json.RawMessage(`{"ok":true}`)},
			&runner.StepRecord{ID: "dup", Call: "S/Create", Status: runner.StatusPassed, Headers: headers,
				Request: json.RawMessage(`{"sku":"sku-` + tag + `-a"}`), Response: json.RawMessage(`{"ok":false}`)})
	}
	a := run("a", "t1", map[string]string{})
	b := run("b", "t2", map[string]string{"X-Trace-Note": "lab"})
	fx := diff.Fixtures{Named: func(step, path string) bool { return step == "create" && path == "sku" }}
	rep := diff.CompareRunsSkipping(a, b, nil, fx)
	text := rep.Text()
	if !strings.Contains(text, "[dup] unexpected headers.X-Trace-Note") {
		t.Fatalf("a header sent in one run only is a request difference:\n%s", text)
	}
	if strings.Contains(text, "[dup] changed    sku") {
		t.Fatalf("a reference that differs only by the fixture name it copies is not a changed request value:\n%s", text)
	}
}

func TestRunDiffTreatsAStepHeldBackByKeepGoingAsNotReached(t *testing.T) {
	a := runOf("run-a",
		stepAs("create", runner.StatusPassed, `{"total":5}`),
		stepAs("confirm", runner.StatusPassed, `{"order":{"total":5}}`))
	b := runOf("run-b",
		stepAs("create", runner.StatusFailed, `{"total":4}`),
		&runner.StepRecord{ID: "confirm", Status: runner.StatusSkipped})
	rep := compareRuns(a, b)
	if len(rep.NoLongerReached) != 1 || rep.NoLongerReached[0] != "confirm" {
		t.Fatalf("a skipped step was not reached, want it listed as such, got %s", rep.Text())
	}
	for _, c := range rep.Changes {
		if c.Step == "confirm" {
			t.Fatalf("a step that was never sent has no response to compare, got %+v", c)
		}
	}
}

func TestRunDiffNamesBuildsAndVarsThatDifferWithoutCallingThemDifferences(t *testing.T) {
	a := runOf("run-a", stepAs("create", runner.StatusPassed, `{"qty":1}`))
	b := runOf("run-b", stepAs("create", runner.StatusPassed, `{"qty":1}`))
	a.Build, b.Build = "baseline", "regressed"
	a.Vars = map[string]any{"tag": "t1", "total": 4100}
	b.Vars = map[string]any{"tag": "t2", "total": 4100}
	rep := compareRuns(a, b)
	if !rep.Same() {
		t.Fatalf("builds and vars are context, not response differences: %s", rep.Text())
	}
	text := rep.Text()
	if !strings.Contains(text, "builds differ: A baseline, B regressed") || !strings.Contains(text, "tag a=t1 b=t2") || strings.Contains(text, "total a=") {
		t.Fatalf("want the builds and only the changed var named, got %s", text)
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
	if rep.Masked != 1 || rep.VolatileMasked != 1 {
		t.Fatalf("the report must say how many id-shaped values it did not count:\n%s", rep.Text())
	}
}

func TestRunDiffDoesNotCallAStepSkippedUnderKeepGoingReached(t *testing.T) {
	a := &runner.Record{RunID: "a", Chain: "c", Status: runner.StatusFailed, KeepGoing: true, Steps: []*runner.StepRecord{
		{ID: "create_product", Status: runner.StatusFailed},
		{ID: "add_stock", Status: runner.StatusSkipped, Error: `not sent: ${create_product.product.id_product} reads step "create_product"`},
		{ID: "create_customer", Status: runner.StatusPassed},
	}}
	b := &runner.Record{RunID: "b", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
		{ID: "create_product", Status: runner.StatusFailed},
	}}
	text := compareRuns(a, b).Text()
	if strings.Contains(text, "are reached in A only") {
		t.Fatalf("add_stock was skipped in A, so not every step past the red is reached in A:\n%s", text)
	}
	if !strings.Contains(text, "run A used -keep-going and run B did not") || !strings.Contains(text, "create_customer") {
		t.Fatalf("the note must still name the side and what it reached:\n%s", text)
	}
	if !strings.Contains(text, "skipped in A as well") || !strings.Contains(text, "add_stock") {
		t.Fatalf("the note must name the step A skipped:\n%s", text)
	}
}

func TestRunDiffNamesTheStepsOnlyOneRunReachedOnce(t *testing.T) {
	a := &runner.Record{RunID: "a", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
		{ID: "create_product", Status: runner.StatusFailed},
	}}
	b := &runner.Record{RunID: "b", Chain: "c", Status: runner.StatusFailed, KeepGoing: true, Steps: []*runner.StepRecord{
		{ID: "create_product", Status: runner.StatusFailed},
		{ID: "add_stock", Status: runner.StatusPassed},
		{ID: "create_customer", Status: runner.StatusPassed},
	}}
	text := compareRuns(a, b).Text()
	if n := strings.Count(text, "add_stock, create_customer"); n != 1 {
		t.Fatalf("the steps only B reached are listed %d times, want once:\n%s", n, text)
	}
	if !strings.Contains(text, "run B used -keep-going and run A did not") {
		t.Fatalf("the keep-going note must stay:\n%s", text)
	}
	if !strings.Contains(text, "reached in B, not reached in A: add_stock, create_customer") {
		t.Fatalf("the reached line must stay:\n%s", text)
	}
}

func echoFailureRun(id, tag, got string) *runner.Record {
	return &runner.Record{RunID: id, Chain: "customers", Status: runner.StatusFailed, Vars: map[string]any{"tag": tag}, Steps: []*runner.StepRecord{
		{ID: "create_customer", Call: "S/CreateCustomer", Status: runner.StatusPassed,
			Request:  []byte(`{"email":"cust-` + tag + `@example.test","name":"Customer ` + tag + `"}`),
			Response: []byte(`{"customer":{"email":"cust-` + tag + `@example.test","name":"Customer ` + tag + `"}}`)},
		{ID: "get_customer", Call: "S/GetCustomer", Status: runner.StatusFailed,
			Response: []byte(`{"customer":{"email":"cust-` + tag + `@example.test","name":"` + got + `"}}`),
			Expect: []chain.ExpectResult{
				{Path: "customer.email", Rule: "equals", Want: "cust-" + tag + "@example.test", Got: "cust-" + tag + "@example.test", Passed: true},
				{Path: "customer.name", Rule: "equals", Want: "Customer " + tag, Got: got, Passed: false},
			}},
	}}
}

func TestDiffShowsTheFailingValuesWhenBothRunsFailedAtTheSameStep(t *testing.T) {
	fixture := func(step, path string) bool { return step == "create_customer" }
	a := echoFailureRun("a", "b6a", "cust-b6a@example.test")
	b := echoFailureRun("b", "b6b", "cust-b6b@example.test")
	text := diff.CompareRunsSkipping(a, b, nil, diff.Fixtures{Named: fixture}).Text()
	for _, want := range []string{
		"first failing step unchanged: get_customer, failing the same way in both: the values differ only by the ids and fixture names each run generated",
		"A: customer.name want=Customer b6a got=cust-b6a@example.test",
		"B: customer.name want=Customer b6b got=cust-b6b@example.test",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("both runs failed at get_customer; the diff shows the failing values of each, masking the echo only in the comparison, want %q:\n%s", want, text)
		}
	}
	c := echoFailureRun("c", "b6c", "Somebody else")
	text = diff.CompareRunsSkipping(a, c, nil, diff.Fixtures{Named: fixture}).Text()
	if !strings.Contains(text, "first failing step unchanged: get_customer, failing differently") {
		t.Fatalf("a got that is not the other run's echo is a different failure:\n%s", text)
	}
}

func TestAFailureDifferingOnlyByTheIdsEachRunGeneratedIsTheSame(t *testing.T) {
	run := func(id, made, other string) *runner.Record {
		return &runner.Record{RunID: id, Chain: "orders", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
			{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"order":{"id_order":"` + made + `"}}`)},
			{ID: "create_other", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"order":{"id_order":"` + other + `"}}`)},
			{ID: "list", Call: "S/List", Status: runner.StatusFailed, Response: []byte(`{"orders":[{"id_order":"` + other + `"}]}`),
				Expect: []chain.ExpectResult{{Path: "orders.0.id_order", Rule: "equals", Want: made, Got: other}}},
		}}
	}
	a := run("a", "ord-c1c11917aa24", "ord-60f9aa4b6db6")
	b := run("b", "ord-fb15f508130a", "ord-d0d8a86db64f")
	c := run("c", "ord-fb15f508130a", "ord-fb15f508130a")
	if !compareRuns(a, b).FailingAlike || compareRuns(a, c).FailingAlike {
		t.Fatal("the same failure with each run's own ids fails alike; a got that is not the other run's renamed id does not")
	}
}
