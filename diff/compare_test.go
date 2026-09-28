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

func step(id, body string) *runner.StepRecord {
	return &runner.StepRecord{
		Index: 1, ID: id, Call: "ThingService/Fetch", Status: runner.StatusPassed,
		Response: json.RawMessage(body),
	}
}

func spotOf(volatile []string, steps ...*runner.StepRecord) *store.SafeSpot {
	return &store.SafeSpot{Chain: "thing-flow", RunID: "safe-1", Volatile: volatile, Steps: steps}
}

func recOf(steps ...*runner.StepRecord) *runner.Record {
	return &runner.Record{Chain: "thing-flow", RunID: "run-9", Status: runner.StatusPassed, Steps: steps}
}

func changeKeys(changes []diff.Change) string {
	out := []string{}
	for _, c := range changes {
		out = append(out, c.Step+":"+c.Kind+":"+c.Path)
	}
	return strings.Join(out, ",")
}

func TestCompareClassifiesEachChangeByKind(t *testing.T) {
	a, b := step("create", `{"id":"a"}`), step("fetch", `{"id":"a"}`)
	for _, c := range []struct {
		name      string
		volatile  []string
		spot, run []*runner.StepRecord
		want      string
	}{
		{"identical", nil, []*runner.StepRecord{step("fetch", `{"id":"thing-1","name":"widget"}`)}, []*runner.StepRecord{step("fetch", `{"id":"thing-1","name":"widget"}`)}, ""},
		{"changed field", nil, []*runner.StepRecord{step("fetch", `{"name":"widget"}`)}, []*runner.StepRecord{step("fetch", `{"name":"gadget"}`)}, "fetch:changed:name"},
		{"volatile path masked", []string{"**.created_at"}, []*runner.StepRecord{step("fetch", `{"id":"a","created_at":"t1"}`)}, []*runner.StepRecord{step("fetch", `{"id":"a","created_at":"t2"}`)}, ""},
		{"missing and unexpected", nil, []*runner.StepRecord{step("fetch", `{"id":"a","name":"widget"}`)}, []*runner.StepRecord{step("fetch", `{"id":"a","label":"widget"}`)}, "fetch:unexpected:label,fetch:missing:name"},
		{"number to string", nil, []*runner.StepRecord{step("r", `{"quantity":3}`)}, []*runner.StepRecord{step("r", `{"quantity":"3"}`)}, "r:type:quantity"},
		{"boolean to string", nil, []*runner.StepRecord{step("r", `{"active":true}`)}, []*runner.StepRecord{step("r", `{"active":"true"}`)}, "r:type:active"},
		{"string to null", nil, []*runner.StepRecord{step("r", `{"note":"x"}`)}, []*runner.StepRecord{step("r", `{"note":null}`)}, "r:type:note"},
		{"scalar to object", nil, []*runner.StepRecord{step("r", `{"owner":"alice"}`)}, []*runner.StepRecord{step("r", `{"owner":{"name":"alice"}}`)}, "r:type:owner"},
		{"1.0 and 1 are one number", nil, []*runner.StepRecord{step("r", `{"rate":1.0}`)}, []*runner.StepRecord{step("r", `{"rate":1}`)}, ""},
		{"unchanged nested body", nil, []*runner.StepRecord{step("r", `{"status":{"code":"SUCCESS"},"lines":[{"quantity":3}]}`)}, []*runner.StepRecord{step("r", `{"status":{"code":"SUCCESS"},"lines":[{"quantity":3}]}`)}, ""},
		{"steps reordered", nil, []*runner.StepRecord{a, b}, []*runner.StepRecord{b, a}, "-:order:steps"},
	} {
		rep := diff.Compare(spotOf(c.volatile, c.spot...), recOf(c.run...))
		if got := changeKeys(rep.Changes); got != c.want || rep.Clean() != (c.want == "") {
			t.Errorf("%s: got %q clean=%v, want %q", c.name, got, rep.Clean(), c.want)
		}
	}
	text := diff.Compare(spotOf(nil, step("r", `{"quantity":3}`)), recOf(step("r", `{"quantity":"3"}`))).Text()
	if !strings.Contains(text, "number 3") || !strings.Contains(text, `string "3"`) {
		t.Fatalf("a type change names both types:\n%s", text)
	}
}

func TestAStepsEnvelopeChangeIsListedFirst(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	spot := spotOf(nil, step("get", `{"customer":null,"status":{"code":"REJECTED"}}`))
	rec := recOf(step("get", `{"customer":{"name":""},"status":{"code":"SUCCESS"}}`))
	rep := diff.Compare(spot, rec)
	if len(rep.Changes) < 2 || rep.Changes[0].Path != "status.code" {
		t.Fatalf("the envelope verdict flip leads the step's changes, got %+v", rep.Changes)
	}
}

func TestAListWhoseLengthChangedIsReportedAsLengthNotOrder(t *testing.T) {
	long := `{"products":[{"n":1},{"n":2},{"n":3}]}`
	short := `{"products":[{"n":1}]}`
	rep := diff.Compare(spotOf(nil, step("list_all_products", long)), recOf(step("list_all_products", short)))
	if !strings.Contains(rep.Text(), "[list_all_products] length     products want=3 item(s) got=1 item(s)") {
		t.Fatalf("a length change must say length, not order:\n%s", rep.Text())
	}
	runs := compareRuns(runOf("run-a", stepAs("list_all_products", runner.StatusPassed, long)),
		runOf("run-b", stepAs("list_all_products", runner.StatusPassed, short)))
	if !strings.Contains(runs.Text(), "[list_all_products] length     products a=3 item(s) b=1 item(s)") {
		t.Fatalf("a length change between runs must say length, not order:\n%s", runs.Text())
	}
}

func TestADriftDumpRendersObjectsAsJSONNotGoMaps(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		stepAs("create", runner.StatusPassed, `{"product":{"sku":"sku-a","price_minor":"250"},"status":{"code":"OK"}}`),
	}}
	rec := runOf("run", stepAs("create", runner.StatusFailed, `{"status":{"code":"REJECTED"}}`))
	text := diff.Compare(spot, rec).Text()
	if strings.Contains(text, "map[") {
		t.Fatalf("a changed object must render as JSON, not as a Go map:\n%s", text)
	}
	if !strings.Contains(text, `{"price_minor":"250","sku":"sku-a"}`) {
		t.Fatalf("the object must read as JSON:\n%s", text)
	}
}

func TestVerifyReportsStepsSkippedBehindAFailureAsNotReachedNotAsALengthChange(t *testing.T) {
	spot := spotOf(nil, step("create", `{"id":"t-1"}`), step("fetch", `{"name":"w"}`), step("list", `{"n":1}`))
	failed := step("create", `{"id":"t-1","error":{"code":"REJECTED"}}`)
	failed.Status, failed.Error = runner.StatusFailed, "expectation failed"
	skipped := &runner.StepRecord{Index: 2, ID: "fetch", Call: "ThingService/Fetch", Status: runner.StatusSkipped, Error: `not sent: ${create.id} reads step "create"`}
	rec := recOf(failed, skipped, step("list", `{"n":1}`))
	rec.Status, rec.KeepGoing = runner.StatusFailed, true

	rep := diff.Compare(spot, rec)
	text := rep.Text()
	if strings.Contains(text, "length") {
		t.Fatalf("a keep-going run records every step, so nothing changed length:\n%s", text)
	}
	if !strings.Contains(text, "[fetch] not_reached") || !strings.Contains(text, "first failing step: step 1 create") {
		t.Fatalf("the skipped step is not reached and the first red is named:\n%s", text)
	}
	for _, c := range rep.Changes {
		if c.Step == "fetch" && c.Kind != diff.KindNotReached {
			t.Fatalf("an unsent step has no response to compare, got %+v", c)
		}
	}
}

func TestVerifyOfARecordedRunThatStoppedEarlyReportsTheRestAsNotReached(t *testing.T) {
	spot := spotOf(nil, step("create", `{"id":"t-1"}`), step("fetch", `{"name":"w"}`), step("list", `{"n":1}`))
	failed := step("create", `{"id":"t-1"}`)
	failed.Status, failed.Error = runner.StatusError, "POST http://x: connection refused"
	failed.Response = nil
	rec := recOf(failed)
	rec.Status = runner.StatusError

	text := diff.Compare(spot, rec).Text()
	if strings.Contains(text, "length") {
		t.Fatalf("a run that stopped at its first red did not change the chain's length:\n%s", text)
	}
	if !strings.Contains(text, "[fetch..list] not_reached 2 step(s)") {
		t.Fatalf("every step past the stop is not reached:\n%s", text)
	}
	if !strings.Contains(text, "connection refused") || strings.Contains(text, "type ") {
		t.Fatalf("a step that errored sending nothing shows its error, not a type change:\n%s", text)
	}
}

func TestRunDiffCountsAnErroredStepThatSentNothingAsNotReachedAndShowsItsError(t *testing.T) {
	a := runOf("run-a", stepAs("create", runner.StatusPassed, `{"id":"t-1"}`))
	b := runOf("run-b", &runner.StepRecord{ID: "create", Call: "ThingService/Fetch", Status: runner.StatusError,
		Error: "POST http://127.0.0.1:1/x: dial tcp: connection refused"})
	rep := compareRuns(a, b)
	if len(rep.NoLongerReached) != 1 || len(rep.Changes) != 0 {
		t.Fatalf("an errored step that sent nothing is not reached and has no response to compare, got %s", rep.Text())
	}
	if !strings.Contains(rep.Text(), "connection refused") {
		t.Fatalf("the error text of the step must be shown:\n%s", rep.Text())
	}
}

func TestRunDiffDoesNotBlameKeepGoingWhenTheOtherRunHadNoRedStep(t *testing.T) {
	a := runOf("run-a", stepAs("create", runner.StatusPassed, `{"id":"t-1"}`))
	b := runOf("run-b", stepAs("create", runner.StatusPassed, `{"id":"t-1"}`))
	b.KeepGoing = true
	if text := compareRuns(a, b).Text(); strings.Contains(text, "past the first red") || strings.Contains(text, "first red") {
		t.Fatalf("run A had no red step, so -keep-going reached nothing extra:\n%s", text)
	}
	c := runOf("run-c", stepAs("create", runner.StatusFailed, `{"id":"t-1"}`))
	if text := compareRuns(c, b).Text(); !strings.Contains(text, "first red") {
		t.Fatalf("with a red step in the run without -keep-going, the note applies:\n%s", text)
	}
}

func TestStepsAStoppedRunNeverReachedAreNotCountedAsChanges(t *testing.T) {
	spot := spotOf(nil, step("create", `{"id":"t-1","total":3148}`), step("fetch", `{"name":"w"}`), step("list", `{"n":1}`))
	failed := step("create", `{"id":"t-1","total":1250}`)
	failed.Status, failed.Error = runner.StatusFailed, "expectation failed"
	rec := recOf(failed)
	rec.Status = runner.StatusFailed

	rep := diff.Compare(spot, rec)
	if got := rep.Counted(); got != 1 {
		t.Fatalf("the total changed and failed the step, whose status is shown on the first failing step line; the two steps never reached are not changes, counted %d:\n%s", got, rep.Text())
	}
	text := rep.Text()
	if !strings.Contains(text, "[fetch..list] not_reached 2 step(s)") {
		t.Fatalf("the steps not reached are still listed:\n%s", text)
	}
}

const (
	timedOutError = "POST http://127.0.0.1:1/shrt.test.v1.ThingService/Fetch: sent, no answer before target.timeout (700ms): context deadline exceeded"
	droppedError  = "POST http://127.0.0.1:1/shrt.test.v1.ThingService/Fetch: sent, no answer: the backend closed the connection " +
		"before a response arrived (EOF): it most likely stopped or crashed while this request was in flight, so whether the call " +
		"took effect is unknown. This is not a verdict about the rpc: check the backend is up and run again"
)

func TestAStepSentWithoutAnAnswerIsSentNotNotReached(t *testing.T) {
	run := func(id, create, createBody, err string, erred ...int) *runner.Record {
		rec := runOf(id, stepAs("create", create, createBody), stepAs("fetch", runner.StatusError, ""), stepAs("list", runner.StatusPassed, `{"n":1}`))
		for _, i := range erred {
			rec.Steps[i].Error = err
		}
		return rec
	}
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: run("spot", runner.StatusPassed, `{"n":1}`, "").Steps}
	spot.Steps[1] = stepAs("fetch", runner.StatusPassed, `{"n":1}`)
	a := runOf("a", spot.Steps...)
	for _, c := range []struct {
		name          string
		verify, runs  *runner.Record
		changes, line string
	}{
		{"timed out", run("run", runner.StatusError, "", timedOutError, 0, 1), run("run", runner.StatusError, "", timedOutError, 0, 1),
			"create:status:status,fetch:status:status", "sent in B, no answer before target.timeout: create, fetch"},
		{"connection dropped", run("run", runner.StatusFailed, `{"n":2}`, droppedError, 1), run("run", runner.StatusPassed, `{"n":1}`, droppedError, 1),
			"create:status:status,create:changed:n,fetch:status:status", "sent in B, no answer (the connection closed): fetch"},
	} {
		rep := diff.Compare(spot, c.verify)
		if got := changeKeys(rep.Changes); got != c.changes || strings.Contains(rep.Text(), "not sent") {
			t.Errorf("%s: verify changes %q, want %q:\n%s", c.name, got, c.changes, rep.Text())
		}
		if text := compareRuns(a, c.runs).Text(); !strings.Contains(text, c.line) || strings.Contains(text, "not reached in B") {
			t.Errorf("%s: diff must say %q:\n%s", c.name, c.line, text)
		}
	}
}

func TestAReadBackHeldBehindAFailedStepShowsWhatItAnswered(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "batch", Call: "S/Batch", Status: runner.StatusPassed, Response: []byte(`{"qty":12}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusPassed, Response: []byte(`{"qty":12}`),
			Expect: []chain.ExpectResult{{Path: "qty", Rule: "equals", Want: 12, Got: 12, Passed: true}}},
	}}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
		{ID: "batch", Call: "S/Batch", Status: runner.StatusFailed, Response: []byte(`{"qty":6}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusFailed, Response: []byte(`{"qty":12}`),
			Expect: []chain.ExpectResult{{Path: "qty", Rule: "unevaluated", Want: 6, Got: 12,
				Detail: `not evaluated: ${batch.qty} reads step "batch", which did not pass, so its value is not evidence (-keep-going)`}}},
	}}
	text := diff.CompareMasking(spot, rec, nil).Text()
	if !strings.Contains(text, "answered qty=12, not judged: it reads step batch, which did not pass") {
		t.Fatalf("the held read-back was sent, so its status change shows what it answered:\n%s", text)
	}
}

func TestAHeldReadBackNamesTheExpectationNotJudged(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"at":"t"}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusPassed, Response: []byte(`{"qty":1}`)},
	}}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusFailed, Response: []byte(`{}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusFailed, Response: []byte(`{"qty":1}`),
			Expect: []chain.ExpectResult{
				{Path: "qty", Rule: "equals", Want: 1, Got: 1, Passed: true},
				{Path: "at", Rule: "unevaluated", Detail: `not evaluated: ${create.at} reads step "create", which did not pass, so its value is not evidence (-keep-going)`}}},
	}}
	text := diff.CompareMasking(spot, rec, nil).Text()
	if !strings.Contains(text, "(at not judged: it reads step create, which did not pass)") {
		t.Fatalf("a sent read-back names the held expectation, not the whole step, as not judged:\n%s", text)
	}
}

func TestVerifyPrintsARepeatedNotSentReasonOnce(t *testing.T) {
	reason := `reads step "create", which was refused in-band (status.code = REJECTED): a refused call's response decodes to zero values, and the request would carry them as if they were real.`
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot"}
	rec := runOf("run", stepAs("create", runner.StatusFailed, `{"n":2}`))
	spot.Steps = append(spot.Steps, &runner.StepRecord{ID: "create", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)})
	for i, id := range []string{"a", "b", "c"} {
		spot.Steps = append(spot.Steps, &runner.StepRecord{ID: id, Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)})
		skipped := stepAs(id, runner.StatusSkipped, "")
		skipped.Error = "not sent: ${create.f" + string(rune('0'+i)) + "} " + reason
		rec.Steps = append(rec.Steps, skipped)
	}
	text := diff.Compare(spot, rec).Text()
	if n := strings.Count(text, "decodes to zero values"); n != 1 {
		t.Fatalf("the reason is printed once and referred to after, printed %d times:\n%s", n, text)
	}
	if !strings.Contains(text, "${create.f2}") {
		t.Fatalf("each skipped step keeps its own reference:\n%s", text)
	}
}

func TestAChangedExpectationNeverExplainsATransportError(t *testing.T) {
	for _, rule := range []string{"equals", "unevaluated"} {
		spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
			{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"n":1}`)},
			{ID: "fetch", Call: "S/Fetch", Status: runner.StatusPassed, Response: []byte(`{"qty":3}`),
				Expect: []chain.ExpectResult{{Path: "qty", Rule: "equals", Want: 3, Got: 3, Passed: true}}},
		}}
		rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
			{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"n":7}`)},
			{ID: "fetch", Call: "S/Fetch", Status: runner.StatusFailed, Response: []byte(`{"code":"internal","message":"pool exhausted"}`),
				Transport: &runner.TransportError{Code: "internal", Message: "pool exhausted"},
				Expect:    []chain.ExpectResult{{Path: "qty", Rule: rule, Want: 4, Passed: false, Detail: "path not present in response"}}},
		}}
		now := &chain.Chain{Name: "c", Steps: []*chain.Step{
			{ID: "create", Call: "S/Create"},
			{ID: "fetch", Call: "S/Fetch", Expect: []chain.Expectation{{Path: "qty", Equals: 4}}},
		}}
		rep := diff.CompareMasking(spot, rec, nil)
		rep.RequestChanges = diff.ChainChangesIn(spot, now, rec)
		rep.SeparateInput(spot, rec, nil, diff.Fixtures{})
		text := rep.Text()
		for _, c := range rep.Changes {
			if c.Step == "fetch" && c.Kind == diff.KindStatus && c.WithInput {
				t.Errorf("rule %s: the step was refused at transport, which no expectation edit explains:\n%s", rule, text)
			}
		}
		if strings.Contains(text, "explained by the failed changed expectation") {
			t.Errorf("rule %s: a transport error is not explained by an expectation edit:\n%s", rule, text)
		}
	}
}

func TestARemovedMiddleStepIsOneChangeAndTheRestAreStillCompared(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"n":1}`)},
		{ID: "fetch", Call: "S/Fetch", Status: runner.StatusPassed, Response: []byte(`{"n":2}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusPassed, Response: []byte(`{"n":3}`)},
		{ID: "list", Call: "S/List", Status: runner.StatusPassed, Response: []byte(`{"n":4}`)},
	}}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"n":1}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusPassed, Response: []byte(`{"n":3}`)},
		{ID: "list", Call: "S/List", Status: runner.StatusPassed, Response: []byte(`{"n":5}`)},
	}}
	now := &chain.Chain{Name: "c", Steps: []*chain.Step{{ID: "create", Call: "S/Create"}, {ID: "get", Call: "S/Get"}, {ID: "list", Call: "S/List"}}}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = diff.ChainChanges(spot, now)
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{})
	kinds := []string{}
	for _, c := range rep.Changes {
		kinds = append(kinds, c.Step+":"+c.Kind+":"+c.Path)
	}
	got := strings.Join(kinds, ",")
	if got != "fetch:missing:step,list:changed:n" {
		t.Fatalf("the removed step is one change, get did not move, and list is still compared at its own record: %s\n%s", got, rep.Text())
	}
	text := rep.Text()
	if strings.Contains(text, "request value(s)") {
		t.Fatalf("a removed step is a chain change, not a request value:\n%s", text)
	}
	if !strings.Contains(text, "1 chain change(s)") {
		t.Fatalf("count the chain change as one:\n%s", text)
	}
}

func TestVerifyReportSaysTheReplayRanAgainstAnotherTarget(t *testing.T) {
	body := `{"order":{"total":"500"}}`
	spot := orderSpot(body)
	spot.Target = "http://127.0.0.1:18199"
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, body))
	rec.Target = "http://prod.example"
	rep := diff.Compare(spot, rec)
	if rep.SafeSpotTarget != "http://127.0.0.1:18199" || rep.RunTarget != "http://prod.example" {
		t.Fatalf("targets not reported: %+v", rep)
	}
	text := rep.Text()
	if list := rep.MaskedList(); !strings.HasPrefix(list, "targets differ: safe spot http://127.0.0.1:18199, this run http://prod.example") || strings.Contains(text, "targets differ") {
		t.Fatalf("a different target leads the -masked list, not the report:\n%s\n%s", list, text)
	}
	rec.Target = spot.Target
	if text := diff.Compare(spot, rec).MaskedList(); strings.Contains(text, "targets differ") {
		t.Fatalf("same target, nothing to say:\n%s", text)
	}
}

func TestATargetThatDiffersOnlyInCaseIsNotReportedAsAnotherTarget(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "a", Target: "HTTP://127.0.0.1:18121"}
	rec := &runner.Record{Chain: "c", RunID: "b", Target: "http://127.0.0.1:18121/"}
	if rep := diff.CompareMasking(spot, rec, nil); rep.SafeSpotTarget != "" || rep.RunTarget != "" {
		t.Fatalf("HTTP:// and http:// name one target, got %q vs %q", rep.SafeSpotTarget, rep.RunTarget)
	}
	a := &runner.Record{Chain: "c", RunID: "a", Target: "http://API.example.test"}
	b := &runner.Record{Chain: "c", RunID: "b", Target: "http://api.example.test"}
	if rep := compareRuns(a, b); rep.TargetA != "" || rep.TargetB != "" {
		t.Fatalf("host case does not change the target, got %q vs %q", rep.TargetA, rep.TargetB)
	}
}
