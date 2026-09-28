package diff_test

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestARemovedStepIsAChainChangeNotARegression(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"ok":true}`)},
		{ID: "get", Call: "S/Get", Status: runner.StatusPassed, Response: []byte(`{"ok":true}`)},
	}}
	now := &chain.Chain{Name: "c", Steps: []*chain.Step{{ID: "create", Call: "S/Create"}}}
	rec := runOf("run", stepAs("create", runner.StatusPassed, `{"ok":true}`))
	rec.Steps[0].Call = "S/Create"

	changes := diff.ChainChanges(spot, now)
	if len(changes) != 1 || changes[0].Step != "get" || changes[0].Kind != diff.KindMissing {
		t.Fatalf("the chain no longer has step get; that is a change of input: %+v", changes)
	}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = append(changes, rep.RequestChanges...)
	if rep.Clean() {
		t.Fatal("the step count still differs")
	}
	if !strings.Contains(rep.Text(), "after a chain change, so they are not evidence of a backend regression") {
		t.Fatalf("a removed step must be reported as a chain change:\n%s", rep.Text())
	}

	for name, c := range map[string]*chain.Chain{
		"added":     {Steps: []*chain.Step{{ID: "create", Call: "S/Create"}, {ID: "get", Call: "S/Get"}, {ID: "list", Call: "S/List"}}},
		"reordered": {Steps: []*chain.Step{{ID: "get", Call: "S/Get"}, {ID: "create", Call: "S/Create"}}},
		"recalled":  {Steps: []*chain.Step{{ID: "create", Call: "S/Create"}, {ID: "get", Call: "S/Fetch"}}},
	} {
		if got := diff.ChainChanges(spot, c); len(got) == 0 {
			t.Errorf("%s: the chain's steps differ from the confirmed run's, got no change", name)
		}
	}
	same := &chain.Chain{Steps: []*chain.Step{{ID: "create", Call: "S/Create"}, {ID: "get", Call: "S/Get"}}}
	if got := diff.ChainChanges(spot, same); len(got) != 0 {
		t.Errorf("an unchanged chain is no input change: %+v", got)
	}
}

func TestARewiredReferenceIsAChainChangeForAnOlderSafeSpot(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create_customer", Call: "S/Create", Status: runner.StatusPassed, Request: []byte(`{"name":"a"}`), Response: []byte(`{"customer":{"id_customer":"cus-1"}}`)},
		{ID: "create_customer_2", Call: "S/Create", Status: runner.StatusPassed, Request: []byte(`{"name":"b"}`), Response: []byte(`{"customer":{"id_customer":"cus-2"}}`)},
		{ID: "create_order", Call: "S/Order", Status: runner.StatusPassed, Request: []byte(`{"id_customer":"cus-1","lines":[{"id_product":"cus-2"}]}`), Response: []byte(`{}`)},
	}}
	body := func(customer string) map[string]any {
		return map[string]any{"id_customer": customer, "lines": []any{map[string]any{"id_product": "${create_customer_2.customer.id_customer}"}}}
	}
	steps := func(customer string) []*chain.Step {
		return []*chain.Step{{ID: "create_customer", Call: "S/Create"}, {ID: "create_customer_2", Call: "S/Create"}, {ID: "create_order", Call: "S/Order", Body: body(customer)}}
	}
	same := diff.ChainChanges(spot, &chain.Chain{Steps: steps("${create_customer.customer.id_customer}")})
	if len(same) != 0 {
		t.Fatalf("the chain reads what the confirmed run read, no change: %+v", same)
	}
	got := diff.ChainChanges(spot, &chain.Chain{Steps: steps("${create_customer_2.customer.id_customer}")})
	if len(got) != 1 || got[0].Step != "create_order" || got[0].Path != "body.id_customer" ||
		got[0].Transition() != "${create_customer.customer.id_customer} -> ${create_customer_2.customer.id_customer}" {
		t.Fatalf("the rewired reference is a chain change naming both references: %+v", got)
	}
	rep := &diff.Report{RequestChanges: got}
	if !rep.OnlyChainChanged() {
		t.Error("a rewired reference is a chain change, not different input")
	}
}

func TestAStoredReferenceChangeIsAChainChange(t *testing.T) {
	spot := &store.SafeSpot{Steps: []*runner.StepRecord{
		{ID: "a", Call: "S/A"},
		{ID: "b", Call: "S/B", Request: []byte(`{"id":"x-1"}`), BodyRefs: map[string]string{"id": "${a.id}"}},
	}}
	now := &chain.Chain{Steps: []*chain.Step{{ID: "a", Call: "S/A"}, {ID: "b", Call: "S/B", Body: map[string]any{"id": "${a.other_id}"}}}}
	got := diff.ChainChanges(spot, now)
	if len(got) != 1 || got[0].Transition() != "${a.id} -> ${a.other_id}" {
		t.Fatalf("the stored reference differs from the chain's: %+v", got)
	}
}

func TestRespelledReferenceIsNotAChainChange(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "order", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"order":{"id_order":"o-1"}}`)},
		{ID: "confirm", Call: "S/Confirm", Status: runner.StatusPassed, Request: []byte(`{"id_order":"o-1","note":"x o-1"}`),
			BodyRefs: map[string]string{"id_order": "${order.order.id_order}", "note": "x ${ order.order.id_order }"}},
	}}
	for _, spelled := range []map[string]any{
		{"id_order": "${steps.order.response.order.id_order}", "note": "x ${steps.order.order.id_order}"},
		{"id_order": "${order.response.order.id_order}", "note": "x ${order.order.id_order}"},
	} {
		now := &chain.Chain{Name: "c", Steps: []*chain.Step{
			{ID: "order", Call: "S/Create"},
			{ID: "confirm", Call: "S/Confirm", Body: spelled},
		}}
		if got := diff.ChainChanges(spot, now); len(got) != 0 {
			t.Errorf("%v reads the same field as the confirmed run, so it is no chain change: %+v", spelled, got)
		}
	}
	other := &chain.Chain{Name: "c", Steps: []*chain.Step{
		{ID: "order", Call: "S/Create"},
		{ID: "confirm", Call: "S/Confirm", Body: map[string]any{"id_order": "${steps.order.request.order.id_order}", "note": "x ${order.order.id_order}"}},
	}}
	if got := diff.ChainChanges(spot, other); len(got) != 1 {
		t.Errorf("reading the request instead of the response is a chain change: %+v", got)
	}
}

func TestARespeltReferenceOrExpectationPathIsNoChainChange(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create_customer", Call: "S/CreateCustomer", Status: runner.StatusPassed, Response: []byte(`{}`)},
		{ID: "create_order", Call: "S/CreateOrder", Status: runner.StatusPassed, Response: []byte(`{}`),
			BodyRefs: map[string]string{"id_customer": "${create_customer.customer.id_customer}"},
			Expect:   []chain.ExpectResult{{Path: "order.total_minor", Rule: "equals", Want: 750, Passed: true}}},
	}}
	now := &chain.Chain{Name: "c", Steps: []*chain.Step{
		{ID: "create_customer", Call: "S/CreateCustomer"},
		{ID: "create_order", Call: "S/CreateOrder",
			Body:   map[string]any{"id_customer": "${create_customer.customer.idCustomer}"},
			Expect: []chain.Expectation{{Path: "order.totalMinor", Equals: 750}}},
	}}
	if changes := diff.ChainChanges(spot, now); len(changes) != 0 {
		t.Fatalf("idCustomer is the JSON name of id_customer and totalMinor of total_minor: no chain change, got %+v", changes)
	}
	now.Steps[1].Body["id_customer"] = "${create_customer.customer.email}"
	if changes := diff.ChainChanges(spot, now); len(changes) != 1 || !strings.HasPrefix(changes[0].Path, diff.BodyPathPrefix) {
		t.Fatalf("a reference to another field is still a chain change: %+v", changes)
	}
}

func TestAnUnorderedPathRespeltInCaseIsNotAnAddition(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "list", Call: "S/List", Status: runner.StatusPassed, Response: []byte(`{}`), Unordered: []string{"line_items"}},
	}}
	rec := runOf("run", &runner.StepRecord{ID: "list", Call: "S/List", Status: runner.StatusPassed, Response: []byte(`{}`), Unordered: []string{"lineItems"}})
	if added := diff.UnorderedAdded(spot, rec); len(added) != 0 {
		t.Fatalf("lineItems is line_items respelt, not an added unordered path: %v", added)
	}
}

func TestARespelledHeaderReferenceIsNoChangeOfInput(t *testing.T) {
	spot := &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "get", Call: "ThingService/Fetch", Status: runner.StatusPassed,
			Headers: map[string]string{"X-Ref": "r-${steps.mk.response.product.IdProduct}", "X-Other": "o-${steps.mk.response.product.sku}"}},
	}}
	rec := runOf("run", stepAs("get", runner.StatusPassed, `{}`))
	rec.Steps[0].Headers = map[string]string{"X-Ref": "r-${steps.mk.response.product.id_product}", "X-Other": "o-${steps.mk.response.product.name}"}

	changes := diff.CompareRequests(spot, rec, nil)
	for _, c := range changes {
		if c.Path == "headers.X-Ref" {
			t.Fatalf("respelling the same field is no change (GRAMMAR section 7), got %+v", c)
		}
	}
	found := false
	for _, c := range changes {
		found = found || c.Path == "headers.X-Other"
	}
	if !found {
		t.Fatalf("a header reading another field is still a change: %+v", changes)
	}
}

func TestAHeaderReadingAnotherFieldIsAChainChangeLikeABodyReference(t *testing.T) {
	spot := &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "get", Call: "ThingService/Fetch", Status: runner.StatusPassed,
			Headers: map[string]string{"X-Ref": "r-${steps.cust.response.customer.id_customer}", "X-Lit": "one"}},
	}}
	rec := runOf("run", stepAs("get", runner.StatusPassed, `{}`))
	rec.Steps[0].Headers = map[string]string{"X-Ref": "r-${steps.cust.response.customer.name}", "X-Lit": "one"}

	rep := &diff.Report{RequestChanges: diff.CompareRequests(spot, rec, nil)}
	if len(rep.RequestChanges) != 1 || rep.RequestChanges[0].Detail != diff.HeaderRefDetail {
		t.Fatalf("the header template reads another field: one change naming it, got %+v", rep.RequestChanges)
	}
	if !rep.OnlyChainChanged() {
		t.Fatalf("a header reference edit is a chain change, as a body reference edit is: %+v", rep.RequestChanges)
	}

	rec.Steps[0].Headers = map[string]string{"X-Ref": "r-${steps.cust.response.customer.id_customer}", "X-Lit": "two"}
	rep = &diff.Report{RequestChanges: diff.CompareRequests(spot, rec, nil)}
	if len(rep.RequestChanges) != 1 || rep.OnlyChainChanged() {
		t.Fatalf("a literal header value changed is different input, as a literal body value is: %+v", rep.RequestChanges)
	}
}

func TestAStepHeaderEditIsAnInputChange(t *testing.T) {
	spot := orderSpot(`{"name":"Widget"}`)
	spot.Steps[0].Headers = map[string]string{}
	got := stepAs("fetch_order", runner.StatusPassed, `{"name":"Widget"}`)
	got.Headers = map[string]string{"X-Dry-Run": "1"}
	changes := diff.CompareRequests(spot, runOf("run", got), nil)
	if len(changes) != 1 || changes[0].Path != "headers.X-Dry-Run" || changes[0].Kind != diff.KindUnexpected {
		t.Fatalf("a header the confirmed run did not send is different input: %+v", changes)
	}
	spot.Steps[0].Headers = map[string]string{"X-Dry-Run": "0"}
	if changes := diff.CompareRequests(spot, runOf("run", got), nil); len(changes) != 1 || changes[0].Kind != diff.KindChanged {
		t.Fatalf("a header value that differs is different input: %+v", changes)
	}
	spot.Steps[0].Headers = nil
	if changes := diff.CompareRequests(spot, runOf("run", got), nil); len(changes) != 0 {
		t.Fatalf("a safe spot recorded before headers were recorded has nothing to compare: %+v", changes)
	}
}

func TestAPrincipalSwapIsAnInputChange(t *testing.T) {
	step := func(profile string) *runner.StepRecord {
		return &runner.StepRecord{ID: "create_customer", Call: "CustomerService/CreateCustomer", Status: runner.StatusPassed,
			AuthProfile: profile, Request: json.RawMessage(`{"name":"Carol"}`), Response: json.RawMessage(`{"status":{"code":"SUCCESS"}}`)}
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{step("default")}}
	rec := runOf("run", step("clerk"))
	changes := diff.CompareRequests(spot, rec, nil)
	if len(changes) != 1 || changes[0].Path != "auth_profile" || changes[0].Want != "default" || changes[0].Got != "clerk" {
		t.Fatalf("the step now runs as another principal, which is a change of input: %+v", changes)
	}
	rep := diff.CompareWithRequests(spot, rec, nil, nil)
	if !strings.Contains(rep.Text(), "auth_profile (default -> clerk)") {
		t.Fatalf("name the profile change:\n%s", rep.Text())
	}
	if !rep.PrincipalChanged() {
		t.Fatal("a principal swap must stop verify from reporting no drift")
	}
	if got := diff.CompareRequests(spot, runOf("run", step("default")), nil); len(got) != 0 {
		t.Fatalf("same principal, no change: %+v", got)
	}
	if got := diff.CompareRequests(spot, runOf("run", step("")), nil); len(got) != 0 {
		t.Fatalf("a record that does not say which profile ran is not evidence of a swap: %+v", got)
	}
}

func TestAnotherPrincipalBehindTheSameProfileIsAnInputChange(t *testing.T) {
	body := `{"error":{"code":"OK"}}`
	want := stepAs("create", runner.StatusPassed, body)
	want.AuthProfile, want.AuthPrincipal = "default", "aaaa1111bbbb2222"
	got := stepAs("create", runner.StatusPassed, body)
	got.AuthProfile, got.AuthPrincipal = "default", "cccc3333dddd4444"
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{want}}

	rep := diff.CompareWithRequests(spot, runOf("run", got), nil, nil)
	if !rep.PrincipalChanged() {
		t.Fatalf("the same profile logged in as someone else, so the safe spot does not vouch for this run:\n%s", rep.Text())
	}
	if !strings.Contains(rep.Text(), "principal") {
		t.Fatalf("the report must say the principal changed:\n%s", rep.Text())
	}

	same := stepAs("create", runner.StatusPassed, body)
	same.AuthProfile, same.AuthPrincipal = "default", "aaaa1111bbbb2222"
	if rep := diff.CompareWithRequests(spot, runOf("run", same), nil, nil); rep.PrincipalChanged() {
		t.Fatalf("the same principal is no input change:\n%s", rep.Text())
	}
	old := stepAs("create", runner.StatusPassed, body)
	old.AuthProfile = "default"
	if rep := diff.CompareWithRequests(spot, runOf("run", old), nil, nil); rep.PrincipalChanged() {
		t.Fatalf("a record that does not say which principal ran is not compared:\n%s", rep.Text())
	}
}

func TestASafeSpotWithoutAPrincipalSaysPrincipalCheckingIsOff(t *testing.T) {
	want := stepAs("create", runner.StatusPassed, `{"error":{"code":"OK"}}`)
	want.AuthProfile = "default"
	got := stepAs("create", runner.StatusPassed, `{"error":{"code":"REJECTED"}}`)
	got.AuthProfile, got.AuthPrincipal = "default", "cccc3333dddd4444"
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{want}}
	rep := diff.CompareWithRequests(spot, runOf("run", got), nil, nil)
	if len(rep.PrincipalUnchecked) != 1 || rep.PrincipalUnchecked[0] != "create" {
		t.Fatalf("the safe spot cannot say which principal ran create, got %v", rep.PrincipalUnchecked)
	}
	text := rep.Text()
	if !strings.Contains(text, "principal checking is off") || !strings.Contains(text, "shrt confirm thing-flow -supersede") {
		t.Fatalf("the report must say principal checking is off and how to turn it on:\n%s", text)
	}
	both := stepAs("create", runner.StatusPassed, `{"error":{"code":"OK"}}`)
	both.AuthProfile, both.AuthPrincipal = "default", "aaaa"
	spot.Steps[0] = both
	if rep := diff.CompareWithRequests(spot, runOf("run", got), nil, nil); len(rep.PrincipalUnchecked) != 0 {
		t.Fatalf("a safe spot with a principal is checked, got %v", rep.PrincipalUnchecked)
	}
}

func clockExpectCase(by any) []diff.Change {
	recorded := func(of int) []chain.ExpectResult {
		return []chain.ExpectResult{
			{Path: "expires_at", Rule: "within", Want: fmt.Sprintf("%d ± 10", of), Got: of, Passed: true},
			{Path: "created_at", Rule: "between", Want: fmt.Sprintf("[%d, %d]", of-60, of+60), Got: of, Passed: true},
			{Path: "qty", Rule: "gte", Want: "2", Got: 3, Passed: true},
			{Path: "request_id", Rule: "not_equal", Want: "0b3e", Got: "77aa", Passed: true},
		}
	}
	spot := &store.SafeSpot{Chain: "auth", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "login", Call: "S/Login", Status: runner.StatusPassed, Expect: recorded(1790314828)},
	}}
	rec := &runner.Record{RunID: "run", Chain: "auth", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{ID: "login", Call: "S/Login", Status: runner.StatusPassed, Expect: recorded(1790314900)},
	}}
	now := &chain.Chain{Name: "auth", Steps: []*chain.Step{{ID: "login", Call: "S/Login", Expect: []chain.Expectation{
		{Path: "expires_at", Within: &chain.Within{Of: "${nowunix+3600}", By: by}},
		{Path: "created_at", Between: []any{"${nowunix-60}", "${nowunix+60}"}},
		{Path: "request_id", NotEqual: "${uuid}"},
		{Path: "qty", Gte: 2},
	}}}}
	return diff.ChainChangesIn(spot, now, rec)
}

func TestAClockTemplateInAnExpectationIsNotAChainEdit(t *testing.T) {
	if got := clockExpectCase(10); len(got) != 0 {
		t.Fatalf("the chain is unchanged; only ${nowunix...} and ${uuid} resolved to other values, yet: %+v", got)
	}
}

func TestALiteralOperandBesideAClockTemplateIsStillCompared(t *testing.T) {
	got := clockExpectCase(20)
	if len(got) != 1 || got[0].Path != diff.ExpectPath || !strings.Contains(fmt.Sprint(got[0].Got), "expires_at within") {
		t.Fatalf("within.by changed from 10 to 20, which is a chain edit: %+v", got)
	}
}

func TestAnExpectationAddedInTheMiddleIsPairedByPath(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "confirm", Call: "S/Confirm", Status: runner.StatusPassed, Response: []byte(`{}`), Expect: []chain.ExpectResult{
			{Path: "status.code", Rule: "equals", Want: "SUCCESS", Passed: true},
			{Path: "order.status", Rule: "equals", Want: "CONFIRMED", Passed: true},
			{Path: "order.id_order", Rule: "equals", Want: "ord-d034498e2fa4", Passed: true},
		}},
	}}
	now := &chain.Chain{Name: "c", Steps: []*chain.Step{{ID: "confirm", Call: "S/Confirm", Expect: []chain.Expectation{
		{Path: "status.code", Equals: "SUCCESS"},
		{Path: "order.status", Equals: "CONFIRMED"},
		{Path: "order.lines.0.qty", Equals: "${vars.qty}"},
		{Path: "order.id_order", Equals: "${create_order.order.id_order}"},
	}}}}
	changes := diff.ChainChanges(spot, now)
	if len(changes) != 1 || changes[0].Kind != diff.KindUnexpected {
		t.Fatalf("one expectation was added; the others are unchanged: %+v", changes)
	}
	if !strings.Contains(changes[0].Transition(), "absent -> order.lines.0.qty equals") || strings.Contains(changes[0].Transition(), "order.id_order") {
		t.Fatalf("the change must be the added path alone: %s", changes[0].Transition())
	}

	rec := runOf("run", stepAs("confirm", runner.StatusPassed, `{}`))
	rec.Steps[0].Expect = []chain.ExpectResult{
		{Path: "status.code", Rule: "equals", Want: "SUCCESS", Passed: true},
		{Path: "order.status", Rule: "equals", Want: "CONFIRMED", Passed: true},
		{Path: "order.lines.0.qty", Rule: "equals", Want: 3, Passed: true},
		{Path: "order.id_order", Rule: "equals", Want: "ord-0f0f0f0f0f0f", Passed: true},
	}
	changes = diff.ChainChangesIn(spot, now, rec)
	if len(changes) != 1 || changes[0].Transition() != "absent -> order.lines.0.qty equals 3" {
		t.Fatalf("with the run at hand the added expectation shows its value as run, the form the safe spot records: %+v", changes)
	}

	rule := &chain.Chain{Name: "c", Steps: []*chain.Step{{ID: "confirm", Call: "S/Confirm", Expect: []chain.Expectation{
		{Path: "status.code", Equals: "SUCCESS"},
		{Path: "order.id_order", NotEmpty: true},
	}}}}
	changes = diff.ChainChanges(spot, rule)
	got := []string{}
	for _, c := range changes {
		got = append(got, c.Transition())
	}
	want := []string{"order.id_order equals ord-d034498e2fa4 -> order.id_order not_empty true", "order.status equals CONFIRMED -> absent"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("per path: a changed rule and a removed expectation\nwant %q\ngot  %q", want, got)
	}
}

func heldExpectCase(addedHeld bool) *diff.Report {
	total := chain.ExpectResult{Path: "total", Rule: "equals", Want: 2, Got: 2, Passed: true}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"n":1}`)},
		{ID: "fetch", Call: "S/Fetch", Status: runner.StatusPassed, Response: []byte(`{"total":2,"qty":3}`), Expect: []chain.ExpectResult{total}},
	}}
	failed := total
	failed.Got, failed.Passed = 9, false
	added := chain.ExpectResult{Path: "qty", Rule: "equals", Want: 3, Got: 3, Passed: true}
	fetchExpect := []chain.ExpectResult{failed, added}
	fetchBody := `{"total":9,"qty":3}`
	if !addedHeld {
		added.Got, added.Passed = 4, false
		fetchExpect = []chain.ExpectResult{total, added}
		fetchBody = `{"total":2,"qty":4}`
	}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusFailed, Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"n":7}`)},
		{ID: "fetch", Call: "S/Fetch", Status: runner.StatusFailed, Response: []byte(fetchBody), Expect: fetchExpect},
	}}
	now := &chain.Chain{Name: "c", Steps: []*chain.Step{
		{ID: "create", Call: "S/Create"},
		{ID: "fetch", Call: "S/Fetch", Expect: []chain.Expectation{
			{Path: "total", Equals: 2},
			{Path: "qty", Equals: 3},
		}},
	}}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = diff.ChainChanges(spot, now)
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{})
	return rep
}

func TestAnAddedExpectationThatHeldExplainsNothing(t *testing.T) {
	rep := heldExpectCase(true)
	text := rep.Text()
	if len(rep.RequestChanges) != 1 || rep.RequestChanges[0].Path != diff.ExpectPath {
		t.Fatalf("the only chain edit is the added expectation: %+v", rep.RequestChanges)
	}
	if got, all := len(rep.Unexplained()), len(rep.Changes); got != all || all == 0 {
		t.Fatalf("an added expectation that held explains none of the %d change(s), %d unexplained:\n%s", all, got, text)
	}
	for _, bad := range []string{"input differs", "after different input", "different input does not explain"} {
		if strings.Contains(text, bad) {
			t.Errorf("an expectation edit is not different input, the report must not say %q:\n%s", bad, text)
		}
	}
	if !strings.Contains(text, "evidence of a backend regression") {
		t.Errorf("the changes are evidence of a regression:\n%s", text)
	}
}

func TestAnAddedExpectationThatFailedExplainsItsOwnStatusChange(t *testing.T) {
	rep := heldExpectCase(false)
	text := rep.Text()
	for _, c := range rep.Changes {
		if c.Step == "fetch" && c.Kind == diff.KindStatus && !c.WithInput {
			t.Errorf("the added expectation failed, so it explains fetch's status change:\n%s", text)
		}
	}
	if strings.Contains(text, "input differs") || strings.Contains(text, "after different input") {
		t.Errorf("an expectation edit is not different input:\n%s", text)
	}
}

func causalRuns(orderWas, orderNow, confirmNow, stockNow string) (*store.SafeSpot, *runner.Record) {
	steps := func(order, confirm, stock string) []*runner.StepRecord {
		return []*runner.StepRecord{
			{ID: "product", Call: "S/CreateProduct", Status: runner.StatusPassed, Response: []byte(`{"id":"p1"}`)},
			{ID: "customer", Call: "S/CreateCustomer", Status: runner.StatusPassed, Response: []byte(`{"id":"c1"}`)},
			{ID: "order", Call: "S/CreateOrder", Status: runner.StatusPassed, Response: []byte(`{"total":` + order + `}`)},
			{ID: "confirm", Call: "S/Confirm", Status: runner.StatusPassed, Response: []byte(`{"total":` + confirm + `}`)},
			{ID: "stock", Call: "S/GetProduct", Status: runner.StatusPassed, Response: []byte(`{"qty":` + stock + `}`)},
		}
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: steps(orderWas, orderWas, "7")}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: steps(orderNow, confirmNow, stockNow)}
	return spot, rec
}

func causalReads() map[string][]diff.Read {
	return map[string][]diff.Read{
		"order":   {{Step: "customer"}, {Step: "product"}},
		"confirm": {{Step: "order"}},
		"stock":   {{Step: "product"}},
	}
}

func unexplainedSteps(rep *diff.Report) string {
	out := []string{}
	for _, c := range rep.Unexplained() {
		out = append(out, c.Step+":"+c.Path)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestAnInputChangeWhoseResponseDidNotChangeExplainsNothingDownstream(t *testing.T) {
	spot, rec := causalRuns("5348", "6250", "6250", "7")
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "customer", Path: "headers.X-Trace-Note", Kind: diff.KindUnexpected, Got: "lab"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Reads: causalReads()})
	if got := unexplainedSteps(rep); got != "confirm:total,order:total" {
		t.Fatalf("customer answered as before, so its header explains no change at the steps reading it: %s\n%s", got, rep.Text())
	}
}

func TestAnInputChangeExplainsItsOwnStepAndTheStepsReadingItsChangedResponse(t *testing.T) {
	spot, rec := causalRuns("5348", "6598", "6598", "6")
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "order", Path: "lines.0.qty", Kind: diff.KindChanged, Want: "3", Got: "4"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Reads: causalReads()})
	if got := unexplainedSteps(rep); got != "" {
		t.Fatalf("order is a write whose answer changed with its qty, so the server state after it may differ: the stock read is explained too: %s\n%s", got, rep.Text())
	}
}

func TestAnInputChangeAtAReadExplainsOnlyItsReaders(t *testing.T) {
	spot, rec := causalRuns("5348", "5348", "5348", "6")
	spot.Steps[1].Call, rec.Steps[1].Call = "S/GetCustomer", "S/GetCustomer"
	rec.Steps[1].Response = []byte(`{"id":"c2"}`)
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "customer", Path: "id", Kind: diff.KindChanged, Want: "c1", Got: "c2"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Reads: causalReads()})
	if got := unexplainedSteps(rep); got != "stock:qty" {
		t.Fatalf("a read changes no server state, so its different input explains nothing at stock, which does not read it: %s\n%s", got, rep.Text())
	}
}

func TestARequestValueReadDownstreamExplainsTheReader(t *testing.T) {
	spot, rec := causalRuns("5348", "5348", "6598", "7")
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "order", Path: "lines.0.qty", Kind: diff.KindChanged, Want: "3", Got: "4"}}
	reads := causalReads()
	reads["confirm"] = []diff.Read{{Step: "order", Request: true, Path: "lines"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Reads: reads})
	if got := unexplainedSteps(rep); got != "" {
		t.Fatalf("confirm reads order's changed request value, so its change is explained: %s\n%s", got, rep.Text())
	}
}

func TestAReaderIsExplainedOnlyWhenTheValueItReadChanged(t *testing.T) {
	product := func(id, price string) string {
		return `{"id_product":"` + id + `","price_minor":"` + price + `"}`
	}
	steps := func(a, b string, list []string, price string) []*runner.StepRecord {
		return []*runner.StepRecord{
			{ID: "pa", Call: "S/CreateProduct", Status: runner.StatusPassed, Response: []byte(`{"product":` + product(a, "100") + `}`)},
			{ID: "pb", Call: "S/CreateProduct", Status: runner.StatusPassed, Response: []byte(`{"product":` + product(b, "200") + `}`)},
			{ID: "list", Call: "S/ListProducts", Status: runner.StatusPassed, Response: []byte(`{"products":[` + strings.Join(list, ",") + `]}`)},
			{ID: "get_first", Call: "S/GetProduct", Status: runner.StatusPassed, Response: []byte(`{"product":` + product(a, price) + `}`)},
		}
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: steps("prd-aaa111", "prd-bbb222",
		[]string{product("prd-aaa111", "100"), product("prd-bbb222", "200")}, "100")}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: steps("prd-ccc333", "prd-ddd444",
		[]string{product("prd-ccc333", "100")}, "101")}
	reads := map[string][]diff.Read{"get_first": {{Step: "list", Path: "products.0.id_product"}}}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "list", Path: "sku_prefix", Kind: diff.KindChanged, Want: "sku-", Got: "sku-a"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Reads: reads})
	if got := unexplainedSteps(rep); got != "get_first:product.price_minor" {
		t.Fatalf("get_first read products.0.id_product, the same product after renaming, so its input did not differ: %s\n%s", got, rep.Text())
	}

	rec.Steps[2].Response = []byte(`{"products":[` + product("prd-ddd444", "200") + `]}`)
	rep = diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "list", Path: "sku_prefix", Kind: diff.KindChanged, Want: "sku-", Got: "sku-b"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Reads: reads})
	if got := unexplainedSteps(rep); got != "" {
		t.Fatalf("the first listed product is another one now, so get_first read another value: %s\n%s", got, rep.Text())
	}
}

func steps(spec string) []*runner.StepRecord {
	out := []*runner.StepRecord{}
	for _, part := range strings.Fields(spec) {
		id, call, _ := strings.Cut(part, ":")
		out = append(out, &runner.StepRecord{ID: id, Call: "S/" + call})
	}
	return out
}

func renameList(rs []diff.StepRename) string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.String())
	}
	return strings.Join(out, ",")
}

func TestARenameIsPairedAcrossAnInsertedOrDeletedStep(t *testing.T) {
	was := steps("mka:Create mkb:Create sta:Add stb:Add ga:Get gb:Get gc:Get")
	for _, tc := range []struct{ now, want string }{
		{"top:List mka:Create mkb:Create sta:Add stb:Add xa:Get gb:Get gc:Get", "ga -> xa"},
		{"mka:Create mkb:Create sta:Add stb:Add xa:Get gb:Get", "ga -> xa"},
		{"mka:Create mkb:Create sta:Add stb:Add ga:Get gb:Get", ""},
		{"mka:Create mkb:Create sta:Add stb:Add ga:Get xb:Get", "gb -> xb"},
	} {
		if got := renameList(diff.StepRenames(was, steps(tc.now))); got != tc.want {
			t.Errorf("now %s: want renames %q, got %q", tc.now, tc.want, got)
		}
	}
}

func TestTextPairsAMissingAndAnUnexpectedFieldHoldingOneValueAsARename(t *testing.T) {
	r := &diff.Report{SafeSpotID: "spot-1", Changes: []diff.Change{
		{Step: "create_order", Path: "order.amount_minor", Kind: diff.KindMissing, Want: float64(3400)},
		{Step: "create_order", Path: "order.total_cents", Kind: diff.KindUnexpected, Got: float64(3400)},
		{Step: "create_order", Path: "order.note", Kind: diff.KindUnexpected, Got: "x"},
		{Step: "list_orders", Path: "orders.0.amount_minor", Kind: diff.KindMissing, Want: float64(3400)},
		{Step: "list_orders", Path: "orders.0.total_cents", Kind: diff.KindUnexpected, Got: float64(9)},
	}}
	renames := r.RenamedFields()
	if len(renames) != 1 || renames[0].From != "order.amount_minor" || renames[0].To != "order.total_cents" {
		t.Fatalf("want one rename, amount_minor -> total_cents at create_order, got %+v", renames)
	}
	text := r.Text()
	for _, want := range []string{
		"[create_order] renamed    order.amount_minor -> order.total_cents (both 3400): likely a renamed field, still a change",
		"[create_order] unexpected order.note",
		"[list_orders] missing    orders.0.amount_minor",
		"[list_orders] unexpected orders.0.total_cents",
		"1 missing and unexpected field pair(s)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[create_order] missing") || strings.Contains(text, "unexpected order.total_cents") {
		t.Fatalf("the paired fields are one renamed line, not two:\n%s", text)
	}
}

func TestChainChangesNameAWholeRemovedBatchItem(t *testing.T) {
	spot := &store.SafeSpot{Chain: "batch", Steps: []*runner.StepRecord{{
		ID: "stock", Call: "StockService/AddStockBatch",
		Request: []byte(`{"lines":[{"id_product":"p-a","qty":"10"},{"id_product":"p-b","qty":"20"},{"id_product":"p-a","qty":"5"}]}`),
		BodyRefs: map[string]string{
			"lines.0.id_product": "${a.id}", "lines.1.id_product": "${b.id}", "lines.2.id_product": "${a.id}",
		},
	}}}
	c := &chain.Chain{Name: "batch", Steps: []*chain.Step{{
		ID: "stock", Call: "StockService/AddStockBatch",
		Body: map[string]any{"lines": []any{
			map[string]any{"id_product": "${a.id}", "qty": "10"},
			map[string]any{"id_product": "${b.id}", "qty": "20"},
		}},
	}}}
	changes := diff.ChainChanges(spot, c)
	if len(changes) != 1 {
		t.Fatalf("want one change, the removed item, got %+v", changes)
	}
	got := changes[0].Path + " " + changes[0].Transition()
	for _, want := range []string{"body.lines.2 ", `"id_product":"${a.id}"`, `"qty":"5"`, "-> absent"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the removed item is named whole, qty included, want %q in %q", want, got)
		}
	}
}

func listStep(call, profile, request string) *runner.StepRecord {
	return &runner.StepRecord{ID: "list", Call: call, Procedure: "/shop.catalog.v1.ProductService/ListProducts",
		AuthProfile: profile, Status: runner.StatusPassed, Request: json.RawMessage(request), Response: json.RawMessage(`{"n":1}`)}
}

func TestRunDiffReportsRequestAndPrincipalDifferences(t *testing.T) {
	a := runOf("run-a", listStep("ListProducts", "default", `{"sku_prefix":"zzz2"}`))
	b := runOf("run-b", listStep("ListProducts", "clerk", `{"sku_prefix":"zzz9"}`))
	rep := compareRuns(a, b)
	text := rep.Text()
	if rep.Same() || strings.Contains(text, "no differences") {
		t.Fatalf("the runs sent different requests as different principals:\n%s", text)
	}
	if !strings.Contains(text, "sku_prefix") || !strings.Contains(text, "zzz2") || !strings.Contains(text, "auth_profile") {
		t.Fatalf("name the request and principal differences:\n%s", text)
	}
}

func TestARespelledCallIsTheSameCall(t *testing.T) {
	a := runOf("run-a", listStep("ListProducts", "default", `{"sku_prefix":"zzz2"}`))
	b := runOf("run-b", listStep("shop.catalog.v1.ProductService/ListProducts", "default", `{"sku_prefix":"zzz2"}`))
	if rep := compareRuns(a, b); !rep.Same() {
		t.Fatalf("the same rpc spelled two ways is no difference:\n%s", rep.Text())
	}
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{listStep("ListProducts", "default", `{"sku_prefix":"zzz2"}`)}}
	now := &chain.Chain{Name: "thing-flow", Steps: []*chain.Step{{ID: "list", Call: "shop.catalog.v1.ProductService/ListProducts"}}}
	if changes := diff.ChainChanges(spot, now); len(changes) != 0 {
		t.Fatalf("respelling the call is not a chain change: %+v", changes)
	}
	rep := diff.CompareMasking(spot, b, nil)
	if !rep.Clean() {
		t.Fatalf("verify must not report the respelled call as an order change:\n%s", rep.Text())
	}
}
