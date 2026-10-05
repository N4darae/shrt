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

func TestRedactedValuesAreCountedAsNeverCompared(t *testing.T) {
	body := `{"product":{"qty_on_hand":"<redacted>","name":"Widget"}}`
	rep := diff.Compare(orderSpot(body), runOf("run", stepAs("fetch_order", runner.StatusPassed, body)))
	if rep.Redacted != 1 || len(rep.RedactedPaths) != 1 || rep.RedactedPaths[0] != "fetch_order product.qty_on_hand" {
		t.Fatalf("a redacted value is blanked on both sides, so it is never compared and must be counted: %+v", rep)
	}
	if !strings.Contains(rep.MaskedList(), "redact paths, blanked in the records, not compared:\n  fetch_order product.qty_on_hand") {
		t.Fatalf("the report must say which redacted values were not compared:\n%s\n%s", rep.Text(), rep.MaskedList())
	}
}

func TestAValueScrubbedForHoldingASecretIsNotSaidToBeUnderARedactPath(t *testing.T) {
	body := `{"customer":{"name":"<redacted>","token":"<redacted>"}}`
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, body))
	rec.Redacted = []string{"**.token", "**.*password"}
	text := diff.Compare(orderSpot(body), rec).MaskedList()
	if !strings.Contains(text, "under redact paths, blanked in the records, not compared:\n  fetch_order customer.token") {
		t.Fatalf("customer.token is under a redact path:\n%s", text)
	}
	if !strings.Contains(text, "scrubbed by value for holding a secret the run sent, not compared:\n  fetch_order customer.name") {
		t.Fatalf("customer.name is under no redact path; it was blanked because its value held a secret, and must be named so:\n%s", text)
	}
}

func TestVerifyWarnsWhenAVolatilePatternMasksAWholeResponse(t *testing.T) {
	spot := orderSpot(`{"order":{"total":"500"}}`)
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"order":{"total":"999"}}`))
	rep := diff.CompareMasking(spot, rec, []string{"**"})
	text := rep.Text()
	if len(rep.FullyMasked) != 1 || rep.FullyMasked[0] != "fetch_order" {
		t.Fatalf("every field of fetch_order is volatile: %+v", rep.FullyMasked)
	}
	if !strings.Contains(text, "WARNING: every response field of step(s) fetch_order") || !strings.Contains(text, "no drift") {
		t.Fatalf("a no-drift verdict over a fully masked response must carry the warning diff and confirm give:\n%s", text)
	}
}

func TestDiffWarnsWhenAVolatilePatternMasksEveryField(t *testing.T) {
	a := runOf("a", stepAs("get", runner.StatusPassed, `{"product":{"price_minor":"250","name":"Widget"}}`))
	b := runOf("b", stepAs("get", runner.StatusPassed, `{"product":{"price_minor":"99999","name":"Widget"}}`))
	a.Volatile = []string{"**"}
	b.Volatile = []string{"**"}
	rep := compareRuns(a, b)
	if len(rep.FullyMasked) != 1 || rep.FullyMasked[0] != "get" {
		t.Fatalf("every field of step get is volatile, so nothing of it was compared: %+v", rep.FullyMasked)
	}
	text := rep.Text()
	if !strings.Contains(text, "WARNING") || !strings.Contains(text, "compared nothing") {
		t.Fatalf("\"no differences\" with only a not-shown count reads as a clean result; warn loudly:\n%s", text)
	}

	a.Volatile, b.Volatile = []string{"**.created_at"}, []string{"**.created_at"}
	if rep := compareRuns(a, b); len(rep.FullyMasked) != 0 || strings.Contains(rep.Text(), "WARNING") {
		t.Fatalf("a narrow pattern masks nothing wholesale: %+v\n%s", rep.FullyMasked, rep.Text())
	}
}

func TestANewRedactPatternNamesTheRequestValueItBlankedToo(t *testing.T) {
	step := func(email string) *runner.StepRecord {
		return &runner.StepRecord{ID: "cust", Call: "S/CreateCustomer", Status: runner.StatusPassed,
			Request: []byte(`{"email":"` + email + `"}`), Response: []byte(`{"customer":{"email":"` + email + `"}}`)}
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{step("w@example.test")}}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Redacted: []string{"**.email"},
		Steps: []*runner.StepRecord{step("<redacted>")}}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.NoteApprovedRedact(nil, rec)
	rep.NoteRedactedRequests(spot, rec)
	text := rep.Text()
	for _, want := range []string{"cust customer.email", "cust request email"} {
		if !strings.Contains(text, want) {
			t.Errorf("want %q among the values the new pattern blanked:\n%s", want, text)
		}
	}
}

func TestVolatileAddedAfterApprovalIsReportedAndCounted(t *testing.T) {
	spot := orderSpot(`{"order":{"total_minor":"500","sku":"sku-a"}}`)
	spot.Volatile = []string{"**.sku"}
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"order":{"total_minor":"999","sku":"sku-b"}}`))
	rec.Volatile = []string{"**.sku", "**.total_minor"}
	rep := diff.CompareMasking(spot, rec, []string{"**"})
	if got := strings.Join(rep.UnapprovedVolatile, ","); got != "**.total_minor,**" {
		t.Fatalf("unapproved patterns = %q, want **.total_minor,**", got)
	}
	if rep.VolatileMasked != 2 {
		t.Errorf("volatile-masked values = %d, want 2 (sku approved, total_minor not)", rep.VolatileMasked)
	}
	if strings.Join(rep.UnapprovedMasked, ",") != "fetch_order order.total_minor" {
		t.Errorf("values hidden only by unapproved patterns = %v", rep.UnapprovedMasked)
	}
	text := rep.Text()
	for _, want := range []string{"did not approve", "**.total_minor", "fetch_order order.total_minor"} {
		if !strings.Contains(text, want) {
			t.Errorf("report must say %q:\n%s", want, text)
		}
	}
	if strings.HasPrefix(text, "no drift") {
		t.Errorf("a replay masked by patterns the safe spot did not approve is not plain no drift:\n%s", text)
	}
}

func TestApprovedVolatileIsCountedButNotFlagged(t *testing.T) {
	spot := orderSpot(`{"order":{"total_minor":"500","sku":"sku-a"}}`)
	spot.Volatile = []string{"**.sku"}
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"order":{"total_minor":"500","sku":"sku-b"}}`))
	rec.Volatile = []string{"**.sku"}
	rep := diff.CompareMasking(spot, rec, []string{"**.sku"})
	if len(rep.UnapprovedVolatile) != 0 || !rep.Clean() || rep.VolatileMasked != 1 {
		t.Fatalf("approved mask: unapproved=%v clean=%v volatile-masked=%d\n%s", rep.UnapprovedVolatile, rep.Clean(), rep.VolatileMasked, rep.Text())
	}
	if strings.Join(rep.VolatilePaths, ",") != "fetch_order order.sku" {
		t.Errorf("volatile paths = %v", rep.VolatilePaths)
	}
}

func TestAnUnapprovedPatternDoesNotListValuesThatOnlyEchoAFixtureName(t *testing.T) {
	spot := &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create", Call: "ThingService/Fetch", Status: runner.StatusPassed,
			Request: json.RawMessage(`{"sku":"sku-first"}`), Response: json.RawMessage(`{"n":1}`)},
		{ID: "list", Call: "ThingService/Fetch", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"products":[{"sku":"sku-first","qty":3}]}`)},
	}}
	rec := runOf("run",
		stepAs("create", runner.StatusPassed, `{"n":1}`),
		stepAs("list", runner.StatusPassed, `{"products":[{"sku":"sku-again","qty":4}]}`))
	rec.Steps[0].Request = json.RawMessage(`{"sku":"sku-again"}`)
	rec.Steps[1].Volatile = []string{"products"}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = diff.CompareRequests(spot, rec, nil)
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Named: func(step, path string) bool { return step == "create" && path == "sku" }})
	if !rep.Widened() {
		t.Fatal("the pattern is still unapproved")
	}
	for _, p := range rep.UnapprovedMasked {
		if p == "list products.0.sku" {
			t.Fatalf("products.0.sku only echoes the fixture name, which verify masks anyway, so the pattern hid nothing there: %v", rep.UnapprovedMasked)
		}
	}
	found := false
	for _, p := range rep.UnapprovedMasked {
		found = found || p == "list products.0.qty"
	}
	if !found {
		t.Fatalf("a real value the pattern hid is still listed: %v", rep.UnapprovedMasked)
	}
}

func TestAnUnapprovedVolatileThatHidOnlyAFixtureEchoHidNothing(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "dup", Call: "S/Create", Status: runner.StatusPassed, Request: json.RawMessage(`{"name":"Dup t-first"}`),
			Response: json.RawMessage(`{"id":"x","message":"Dup t-first exists"}`)},
	}}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Volatile: []string{"**.message"}, Steps: []*runner.StepRecord{
		{ID: "dup", Call: "S/Create", Status: runner.StatusPassed, Request: json.RawMessage(`{"name":"Dup t-second"}`),
			Response: json.RawMessage(`{"id":"x","message":"Dup t-second exists"}`)},
	}}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "dup", Path: "name", Kind: diff.KindChanged, Want: "Dup t-first", Got: "Dup t-second"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Named: func(step, path string) bool { return path == "name" }})
	text := rep.Text()
	if !strings.Contains(text, "hid nothing this run") {
		t.Fatalf("the unapproved pattern hid only a fixture echo, so it hid nothing this run:\n%s", text)
	}
	if rep.VolatileMasked != 0 || len(rep.FixtureEchoed) != 1 {
		t.Fatalf("the echo is counted as a fixture echo, not as a value under volatile paths: volatile %d, echoed %d\n%s",
			rep.VolatileMasked, len(rep.FixtureEchoed), text)
	}
}

func TestARedactPatternNeitherComparesNorHidesWhatItShouldNot(t *testing.T) {
	for _, c := range []struct {
		name, spot, run    string
		redacted, approved []string
		clean              bool
		unapproved         string
	}{
		{"added after approval", `{"results":[{"qty_on_hand":5,"name":"Widget"}]}`, `{"results":[{"qty_on_hand":"<redacted>","name":"Widget"}]}`,
			[]string{"**.password", "**.qty_on_hand"}, nil, true, "**.qty_on_hand"},
		{"lacked by the safe spot's run", `{"name":"Widget"}`, `{"name":"Widget"}`, []string{"**.password", "**.pin"}, []string{"**.password"}, true, "**.pin"},
		{"redacted in the safe spot only", `{"qty_on_hand":"<redacted>","name":"Widget"}`, `{"qty_on_hand":7,"name":"Widget"}`, nil, nil, true, ""},
		{"empty then a secret", `{"access_token":"","name":"Widget"}`, `{"access_token":"<redacted>","name":"Widget"}`, []string{"**.access_token"}, []string{"**.access_token"}, false, ""},
		{"a secret then empty", `{"access_token":"<redacted>","name":"Widget"}`, `{"access_token":"","name":"Widget"}`, []string{"**.access_token"}, []string{"**.access_token"}, false, ""},
	} {
		rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, c.run))
		rec.Redacted = c.redacted
		rep := diff.Compare(orderSpot(c.spot), rec)
		if c.approved != nil {
			rep.NoteApprovedRedact(c.approved, rec)
		}
		if rep.Clean() != c.clean || rep.Widened() != (c.unapproved != "") || strings.Join(rep.UnapprovedRedact, ",") != c.unapproved {
			t.Errorf("%s: clean=%v widened=%v unapproved=%v, want %v %q:\n%s", c.name, rep.Clean(), rep.Widened(), rep.UnapprovedRedact, c.clean, c.unapproved, rep.Text())
		}
	}
	spot := orderSpot(`{"name":"Widget"}`)
	spot.Steps[0].Request = []byte(`{"qty":5}`)
	got := stepAs("fetch_order", runner.StatusPassed, `{"name":"Widget"}`)
	got.Request = []byte(`{"qty":"<redacted>"}`)
	if changes := diff.CompareRequests(spot, runOf("run", got), nil); len(changes) != 0 {
		t.Fatalf("a request value redacted on one side only is not different input: %+v", changes)
	}
}

func TestAVolatileListStillReportsGoingEmptyOrFilling(t *testing.T) {
	full := `{"items":[{"id":"a"},{"id":"b"}],"status":"ok"}`
	for _, c := range []struct{ was, now string }{
		{full, `{"status":"ok"}`},
		{full, `{"items":[],"status":"ok"}`},
		{`{"status":"ok"}`, full},
		{`{"items":[],"status":"ok"}`, full},
	} {
		was, now := step("list", c.was), step("list", c.now)
		was.Volatile, now.Volatile = []string{"items"}, []string{"items"}
		rep := diff.Compare(spotOf(nil, was), recOf(now))
		if rep.Clean() || !strings.Contains(rep.Text(), "items") {
			t.Errorf("%s -> %s: a volatile list that went empty or filled is still a change:\n%s", c.was, c.now, rep.Text())
		}
	}
	was, now := step("list", full), step("list", `{"items":[{"id":"c"}],"status":"ok"}`)
	was.Volatile, now.Volatile = []string{"items"}, []string{"items"}
	if rep := diff.Compare(spotOf(nil, was), recOf(now)); !rep.Clean() {
		t.Errorf("other items in a volatile list are not a change:\n%s", rep.Text())
	}
}

func TestAVolatileListShowsItsLengthOnlyWhenItsOwnExpectationFailed(t *testing.T) {
	long := `{"products":[{"id":"a"},{"id":"b"},{"id":"c"}]}`
	short := `{"products":[{"id":"z"}]}`
	failed := func() *runner.StepRecord {
		st := stepAs("list_all", runner.StatusFailed, short)
		st.Expect = []chain.ExpectResult{{Path: "products", Rule: "includes", Want: map[string]any{"id": "a"}, Got: 1}}
		return st
	}
	want := "[list_all] length products want=3 got=1"
	rep := diff.CompareMasking(spotOf([]string{"products"}, step("list_all", long)), recOf(failed()), nil)
	if !strings.Contains(rep.Text(), want) {
		t.Fatalf("a volatile list whose expectation failed must show its length change:\n%s", rep.Text())
	}
	runs := compareRuns(runOf("run-a", stepAs("list_all", runner.StatusPassed, long)), runOf("run-b", failed()), "products")
	if !strings.Contains(runs.Text(), "[list_all] length products a=3 b=1") {
		t.Fatalf("diff must show the length change too:\n%s", runs.Text())
	}
	quiet := diff.CompareMasking(spotOf([]string{"products"}, step("list_all", long)), recOf(step("list_all", short)), nil)
	if !quiet.Clean() {
		t.Fatalf("a volatile list whose expectations passed stays masked:\n%s", quiet.Text())
	}
}

func TestAVolatileValueIsHiddenOnlyWhileItHoldsAValue(t *testing.T) {
	body := func(created string) string {
		return `{"product":{"id":"p1","name":"w"` + created + `}}`
	}
	recorded := body(`,"created_at":"2026-09-01T10:00:00Z"`)
	for _, c := range []struct {
		was, now string
		clean    bool
	}{
		{recorded, body(`,"created_at":"2026-09-02T11:00:00Z"`), true},
		{body(`,"created_at":null`), body(""), true},
		{body(""), body(`,"created_at":null`), true},
		{recorded, body(""), false},
		{recorded, body(`,"created_at":null`), false},
		{recorded, body(`,"created_at":""`), false},
		{recorded, body(`,"created_at":"1970-01-01T00:00:00Z"`), false},
		{recorded, body(`,"created_at":"0001-01-01T00:00:00Z"`), false},
		{body(""), recorded, false},
	} {
		rep := diff.Compare(spotOf([]string{"**.created_at"}, step("create", c.was)), recOf(step("create", c.now)))
		if rep.Clean() != c.clean || (!c.clean && (changeKeys(rep.Changes) == "" || !strings.Contains(rep.Text(), "**.created_at"))) {
			t.Errorf("%s -> %s: verify clean=%v, want %v, naming the pattern when not:\n%s", c.was, c.now, rep.Clean(), c.clean, rep.Text())
		}
		if c.clean && c.now != body(`,"created_at":"2026-09-02T11:00:00Z"`) && (rep.VolatileMasked != 1 || len(rep.VolatileValues) != 1 || rep.VolatileValues[0].Mask != "**.created_at") {
			t.Errorf("%s -> %s: a null and an absent value are one masked value: %+v", c.was, c.now, rep.VolatileValues)
		}
		a, b := runOf("a", stepAs("create", runner.StatusPassed, c.was)), runOf("b", stepAs("create", runner.StatusPassed, c.now))
		a.Volatile, b.Volatile = []string{"**.created_at"}, []string{"**.created_at"}
		if compareRuns(a, b).Same() != c.clean || compareRuns(b, a).Same() != c.clean {
			t.Errorf("%s -> %s: diff must agree with verify (clean=%v)", c.was, c.now, c.clean)
		}
	}
}

func TestRunDiffListsEachMaskedDifferenceWithWhatHidIt(t *testing.T) {
	a := runOf("a", stepAs("create", runner.StatusPassed,
		`{"product":{"id_product":"prd-0123456789ab","note":"x1","created_at":"2026-09-01T10:00:00Z"}}`))
	b := runOf("b", stepAs("create", runner.StatusPassed,
		`{"product":{"id_product":"prd-ba9876543210","note":"x2","created_at":"2026-09-02T10:00:00Z"}}`))
	a.Volatile, b.Volatile = []string{"**.created_at", "product.note"}, []string{"**.created_at", "product.note"}
	rep := compareRuns(a, b)
	if !rep.Same() {
		t.Fatalf("every difference is masked:\n%s", rep.Text())
	}
	masks := map[string]string{}
	for _, c := range rep.MaskedChanges {
		masks[c.Path] = c.Mask
	}
	if masks["product.created_at"] != "**.created_at" || masks["product.note"] != "product.note" {
		t.Fatalf("each value hidden by a volatile pattern names that pattern: %+v", rep.MaskedChanges)
	}
	if !strings.Contains(masks["product.id_product"], "shape") {
		t.Fatalf("an id masked by its shape says so: %+v", rep.MaskedChanges)
	}
	list := rep.MaskedList()
	for _, want := range []string{"product.created_at", "2026-09-01T10:00:00Z -> 2026-09-02T10:00:00Z", "hidden by **.created_at", "hidden by product.note"} {
		if !strings.Contains(list, want) {
			t.Fatalf("the masked list names %q:\n%s", want, list)
		}
	}
}

func staleSteps(sent, echoed, message string) []*runner.StepRecord {
	return []*runner.StepRecord{
		{ID: "cust", Call: "S/CreateCustomer", Status: runner.StatusPassed,
			Request:  []byte(`{"email":"` + sent + `"}`),
			Response: []byte(`{"customer":{"email":"` + echoed + `"},"status":{"message":"` + message + `"}}`)},
	}
}

func TestAResponseStillCarryingTheConfirmedFixtureNameIsAChange(t *testing.T) {
	fixture := func(step, path string) bool { return step == "cust" && path == "email" }
	for _, tc := range []struct {
		name, echoed, message string
		changed               []string
	}{
		{"echoes the new name", "w-px3@example.test", "made w-px3@example.test", nil},
		{"echoes the old email", "w-g1@example.test", "made w-px3@example.test", []string{"cust customer.email"}},
		{"old name in a message", "w-px3@example.test", "made w-g1@example.test", []string{"cust status.message"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: staleSteps("w-g1@example.test", "w-g1@example.test", "made w-g1@example.test")}
			rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: staleSteps("w-px3@example.test", tc.echoed, tc.message)}
			rep := diff.CompareMasking(spot, rec, nil)
			rep.RequestChanges = diff.CompareRequests(spot, rec, nil)
			rep.SeparateInput(spot, rec, nil, diff.Fixtures{Named: fixture})
			got := []string{}
			for _, c := range rep.Changes {
				got = append(got, c.Step+" "+c.Path)
			}
			if strings.Join(got, ",") != strings.Join(tc.changed, ",") {
				t.Fatalf("verify changes %v, want %v\n%s", got, tc.changed, rep.Text())
			}
			if len(tc.changed) > 0 && !strings.Contains(rep.Text(), "want=w-px3@example.test") && !strings.Contains(rep.Text(), "want=made w-px3@example.test") {
				t.Fatalf("the change says what an echo of this run's input would read:\n%s", rep.Text())
			}
			a := &runner.Record{RunID: "a", Chain: "c", Status: runner.StatusPassed, Steps: spot.Steps}
			runs := diff.CompareRunsSkipping(a, rec, nil, diff.Fixtures{Named: fixture})
			got = got[:0]
			for _, c := range runs.Changes {
				got = append(got, c.Step+" "+c.Path)
			}
			if strings.Join(got, ",") != strings.Join(tc.changed, ",") {
				t.Fatalf("diff changes %v, want %v\n%s", got, tc.changed, runs.Text())
			}
		})
	}
}

func TestAStaleEchoUnderAnUnapprovedVolatileIsAHiddenValue(t *testing.T) {
	fixture := func(step, path string) bool { return step == "cust" && path == "email" }
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: staleSteps("w-g1@example.test", "w-g1@example.test", "ok")}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Volatile: []string{"**.email"},
		Steps: staleSteps("w-px3@example.test", "w-g1@example.test", "ok")}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = diff.CompareRequests(spot, rec, nil)
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Named: fixture})
	text := rep.Text()
	if strings.Contains(text, "they hid nothing this run") || !strings.Contains(text, "cust customer.email") {
		t.Fatalf("the unapproved **.email hid the stale echo customer.email, so it hid a value:\n%s", text)
	}
}

func fixtureRun(id, tag, message string) *runner.Record {
	return &runner.Record{RunID: id, Chain: "c", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed,
			Request:  []byte(`{"sku":"sku-` + tag + `"}`),
			Response: []byte(`{"product":{"sku":"sku-` + tag + `","name":"Widget sku-` + tag + `"},"note":"` + message + `"}`)},
	}}
}

func TestDiffMasksAResponseValueThatOnlyEchoesTheFixtureName(t *testing.T) {
	fixture := func(step, path string) bool { return step == "create" && path == "sku" }
	rep := diff.CompareRunsSkipping(fixtureRun("a", "first", "ok"), fixtureRun("b", "second", "ok"), nil, diff.Fixtures{Named: fixture})
	if len(rep.Changes) != 0 {
		t.Fatalf("both response values only echo the new sku, as verify masks them; want no change, got %d:\n%s", len(rep.Changes), rep.Text())
	}
	if !strings.Contains(rep.Text(), "not counted: 3 (-masked lists them)") {
		t.Fatalf("the report counts the echoes:\n%s", rep.Text())
	}
	rep = diff.CompareRunsSkipping(fixtureRun("a", "first", "ok"), fixtureRun("b", "second", "late"), nil, diff.Fixtures{Named: fixture})
	if len(rep.Changes) != 1 || rep.Changes[0].Path != "note" {
		t.Fatalf("a real response difference is still shown: %+v\n%s", rep.Changes, rep.Text())
	}
}

func TestDiffDoesNotCallFixtureVarsADifferentInput(t *testing.T) {
	named := func(step, path string) bool { return step == "create" && path == "sku" }
	a, b := fixtureRun("a", "first", "ok"), fixtureRun("b", "second", "ok")
	a.Vars, b.Vars = map[string]any{"tag": "first"}, map[string]any{"tag": "second"}
	fx := diff.Fixtures{Named: named, Var: func(name string) bool { return name == "tag" }}
	text := diff.CompareRunsSkipping(a, b, nil, fx).Text()
	if strings.Contains(text, "the runs used different vars") {
		t.Fatalf("tag is only a fixture var whose echoes are masked; it is no different input:\n%s", text)
	}
	if !strings.Contains(diff.CompareRunsSkipping(a, b, nil, fx).MaskedList(), "fixture vars differ, echoes masked: tag a=first b=second") {
		t.Fatalf("the report still names the fixture var:\n%s", text)
	}
	a.Vars["qty"], b.Vars["qty"] = 1, 2
	text = diff.CompareRunsSkipping(a, b, nil, fx).Text()
	if !strings.Contains(text, "the runs used different vars, so a difference may come from the input rather than the backend: qty a=1 b=2") {
		t.Fatalf("a var that is not a fixture var is still a different input:\n%s", text)
	}
	if !strings.Contains(diff.CompareRunsSkipping(a, b, nil, fx).MaskedList(), "fixture vars differ, echoes masked: tag a=first b=second") {
		t.Fatalf("the fixture var is named apart:\n%s", text)
	}
}

func unknownCustomerRun(id, tag string) *runner.Record {
	return &runner.Record{RunID: id, Chain: "c", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{ID: "get_customer_unknown", Call: "S/GetCustomer", Status: runner.StatusPassed,
			Request:  []byte(`{"id_customer":"cus-missing-` + tag + `"}`),
			Response: []byte(`{"status":{"code":"REJECTED","message":"no customer cus-missing-` + tag + `"}}`)},
	}}
}

func TestDiffMasksARefusalMessageEchoingAFixtureSentInAnIDShapedField(t *testing.T) {
	fixture := func(step, path string) bool { return step == "get_customer_unknown" && path == "id_customer" }
	rep := diff.CompareRunsSkipping(unknownCustomerRun("a", "ci17-a-1"), unknownCustomerRun("b", "ci17-b-1"), nil, diff.Fixtures{Named: fixture})
	if len(rep.Changes) != 0 || rep.FixtureEchoed != 1 {
		t.Fatalf("the refusal message only echoes the id the run sent, built from the fixture var, as verify masks it: %+v\n%s", rep.Changes, rep.Text())
	}
}
