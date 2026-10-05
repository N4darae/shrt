package diff_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func orderSpot(body string) *store.SafeSpot {
	return &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "fetch_order", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(body)},
	}}
}

func TestAnIdIsMaskedOnlyWhenBothSidesAreIdShapedOfOneKind(t *testing.T) {
	order := `{"order":{"id_order":"ord-819dac5f23ba","id_customer":"cus-5656cb156e31","created_at":"2026-09-24T15:59:18.1Z","total":"500"}}`
	fresh := func(field, value string) string {
		v := map[string]any{"id_order": "ord-0123456789ab", "id_customer": "cus-ba9876543210", "created_at": "2026-09-25T10:00:00Z", "total": "500"}
		if field != "" {
			v[field] = json.RawMessage(value)
		}
		if value == "" {
			delete(v, field)
		}
		b, _ := json.Marshal(map[string]any{"order": v})
		return string(b)
	}
	customer := func(id string) string { return `{"order":{"id_customer":"` + id + `"}}` }
	product := func(id, name string) string { return `{"product":{"id_product":"` + id + `","name":"` + name + `"}}` }
	mug := product("prd-60b05a32d153", "Mug")
	for _, c := range []struct {
		name, want, got string
		masked          int
	}{
		{"fresh ids and timestamp", order, fresh("", ""), 3},
		{"empty id", order, fresh("id_customer", `""`), -1},
		{"undefined id", order, fresh("id_customer", `"undefined"`), -1},
		{"null id", order, fresh("id_customer", `null`), -1},
		{"number id", order, fresh("id_customer", `7`), -1},
		{"missing id", order, fresh("id_order", ""), -1},
		{"empty timestamp", order, fresh("created_at", `""`), -1},
		{"junk timestamp", order, fresh("created_at", `"never"`), -1},
		{"numeric ids", `{"order":{"id":41}}`, `{"order":{"id":42}}`, 1},
		{"numeric id became 0", `{"order":{"id":41}}`, `{"order":{"id":0}}`, -1},
		{"fresh id of one kind", customer("cus-5656cb156e31"), customer("cus-0123456789ab"), 1},
		{"product id in a customer field", customer("cus-5656cb156e31"), customer("prd-000000000000"), -1},
		{"prefix dropped", customer("cus-5656cb156e31"), customer("5656cb156e31"), -1},
		{"underscore prefix of another", customer("cus-5656cb156e31"), customer("prd_5656cb156e31"), -1},
		{"zero short", customer("cus-5656cb156e31"), customer("cus-0"), -1},
		{"zero hex", customer("cus-5656cb156e31"), customer("cus-000000000000"), -1},
		{"bare zero", customer("cus-5656cb156e31"), customer("0"), -1},
		{"zero uuid", customer("3f2b8c1e-4a5d-4e6f-8a7b-1c2d3e4f5a6b"), customer("00000000-0000-0000-0000-000000000000"), -1},
		{"all-letter hex", mug, product("prd-edababebdffe", "Mug"), 1},
		{"word id", mug, product("prd-undefinedxx", "Mug"), -1},
		{"short word", mug, product("prd-none", "Mug"), -1},
		{"shorter hex", mug, product("prd-abcdef", "Mug"), -1},
		{"changed name", mug, product("prd-edababebdffe", "Cup"), -1},
		{"other prefix", mug, product("ord-edababebdffe", "Mug"), -1},
		{"extra segment", mug, product("prd-edababebdffe-x", "Mug"), -1},
		{"placeholder id", mug, product("prd-000000000000", "Mug"), -1},
	} {
		rep := diff.Compare(orderSpot(c.want), runOf("run", stepAs("fetch_order", runner.StatusPassed, c.got)))
		if rep.Clean() != (c.masked >= 0) || (c.masked >= 0 && rep.Masked != c.masked) {
			t.Errorf("%s: verify clean=%v masked=%d, want masked %d:\n%s", c.name, rep.Clean(), rep.Masked, c.masked, rep.Text())
		}
		runs := compareRuns(runOf("a", stepAs("fetch_order", runner.StatusPassed, c.want)), runOf("b", stepAs("fetch_order", runner.StatusPassed, c.got)))
		if runs.Same() != (c.masked >= 0) {
			t.Errorf("%s: diff same=%v, want %v:\n%s", c.name, runs.Same(), c.masked >= 0, runs.Text())
		}
	}
}

func TestMaskedListingNamesShapeMaskedValues(t *testing.T) {
	want := `{"order":{"id_order":"ord-819dac5f23ba","created_at":"2026-09-24T15:59:18Z","note":"a"}}`
	got := `{"order":{"id_order":"ord-0123456789ab","created_at":"2026-09-25T10:00:00Z","note":"b"}}`
	spot := orderSpot(want)
	spot.Volatile = []string{"**.note"}
	rep := diff.Compare(spot, runOf("run", stepAs("fetch_order", runner.StatusPassed, got)))
	list := rep.MaskedList()
	for _, s := range []string{
		"fetch_order order.id_order (ord-819dac5f23ba -> ord-0123456789ab)",
		"fetch_order order.created_at (2026-09-24T15:59:18Z -> 2026-09-25T10:00:00Z)",
		"fetch_order order.note (a -> b)",
	} {
		if !strings.Contains(list, s) {
			t.Errorf("-masked must list %q:\n%s", s, list)
		}
	}
	if len(rep.ShapeMasked) != 2 {
		t.Errorf("shape_masked = %+v, want the id and the timestamp", rep.ShapeMasked)
	}
}

func customerOrderSpot(cust, order, fetch string) *store.SafeSpot {
	return &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "cust", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(cust)},
		{ID: "order", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(order)},
		{ID: "fetch", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(fetch)},
	}}
}

func customerOrderRun(cust, order, fetch string) *runner.Record {
	return runOf("run",
		stepAs("cust", runner.StatusPassed, cust),
		stepAs("order", runner.StatusPassed, order),
		stepAs("fetch", runner.StatusPassed, fetch))
}

func TestAMaskedIdMustBeRenamedConsistentlyAcrossTheRecord(t *testing.T) {
	spot := customerOrderSpot(
		`{"customer":{"id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_order":"ord-819dac5f23ba","id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_order":"ord-819dac5f23ba","id_customer":"cus-5656cb156e31"}}`)

	consistent := customerOrderRun(
		`{"customer":{"id_customer":"cus-ba9876543210"}}`,
		`{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210"}}`,
		`{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210"}}`)
	if rep := diff.Compare(spot, consistent); !rep.Clean() {
		t.Fatalf("every id renamed to one new value is the same relationship, got drift:\n%s", rep.Text())
	}

	for name, fetch := range map[string]string{
		"another customer": `{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-000000000001"}}`,
		"zero customer":    `{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-0"}}`,
		"all zeros":        `{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-000000000000"}}`,
	} {
		run := customerOrderRun(
			`{"customer":{"id_customer":"cus-ba9876543210"}}`,
			`{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210"}}`,
			fetch)
		rep := diff.Compare(spot, run)
		if rep.Clean() {
			t.Errorf("%s: the order now points at a different customer than the one created, got no drift:\n%s", name, rep.Text())
			continue
		}
		text := rep.Text()
		if !strings.Contains(text, "fetch") || !strings.Contains(text, "order.id_customer") {
			t.Errorf("%s: the drift must name the step and path that broke the relationship:\n%s", name, text)
		}
	}

	merged := customerOrderRun(
		`{"customer":{"id_customer":"cus-ba9876543210"}}`,
		`{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210"}}`,
		`{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210","id_referrer":"cus-ba9876543210"}}`)
	spotMerged := customerOrderSpot(
		`{"customer":{"id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_order":"ord-819dac5f23ba","id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_order":"ord-819dac5f23ba","id_customer":"cus-5656cb156e31","id_referrer":"cus-77777777aaaa"}}`)
	rep := diff.Compare(spotMerged, merged)
	if rep.Clean() || !strings.Contains(rep.Text(), "id_referrer") {
		t.Fatalf("two different ids of the safe spot became one value, so two relationships collapsed into one:\n%s", rep.Text())
	}
}

func TestAnUnchangedIdThatIsRenamedElsewhereIsDrift(t *testing.T) {
	spot := customerOrderSpot(
		`{"customer":{"id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_customer":"cus-5656cb156e31"}}`)
	run := customerOrderRun(
		`{"customer":{"id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_customer":"cus-ba9876543210"}}`)
	if rep := diff.Compare(spot, run); rep.Clean() {
		t.Fatalf("the customer id stayed the same in two steps and changed in the third, got no drift:\n%s", rep.Text())
	}
}

func idInTextSteps(product, message string) []*runner.StepRecord {
	return []*runner.StepRecord{
		{ID: "create", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"product":{"id_product":"` + product + `"}}`)},
		{ID: "confirm", Call: "S/Confirm", Status: runner.StatusPassed, Response: []byte(`{"status":{"message":"` + message + `"}}`)},
	}
}

func TestARenamedIDInsideAMessageIsNotAChange(t *testing.T) {
	for _, tc := range []struct {
		name, was, now string
		changed        bool
	}{
		{"only the id", "not enough stock for prd-847c0a1b", "not enough stock for prd-b7703c2d", false},
		{"the text too", "not enough stock for prd-847c0a1b", "only 2 left for prd-b7703c2d", true},
		{"an id the renaming did not map", "not enough stock for prd-11110000", "not enough stock for prd-22220000", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: idInTextSteps("prd-847c0a1b", tc.was)}
			rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: idInTextSteps("prd-b7703c2d", tc.now)}
			rep := diff.CompareMasking(spot, rec, nil)
			if changed := !rep.Clean(); changed != tc.changed {
				t.Fatalf("verify: changed=%v, want %v\n%s", changed, tc.changed, rep.Text())
			}
			a := &runner.Record{RunID: "a", Chain: "c", Status: runner.StatusPassed, Steps: spot.Steps}
			runs := compareRuns(a, rec)
			if changed := len(runs.Changes) > 0; changed != tc.changed {
				t.Fatalf("diff: changed=%v, want %v\n%s", changed, tc.changed, runs.Text())
			}
			if !tc.changed && !strings.Contains(rep.MaskedList(), "id- or timestamp-shaped") {
				t.Fatalf("the renamed id is counted with the masked ids:\n%s", rep.Text())
			}
		})
	}
}

func TestARenamedIDInsideAMessageStaysMaskedWhenAListElsewhereBreaksTheRenaming(t *testing.T) {
	steps := func(product, message, listed string) []*runner.StepRecord {
		return append(idInTextSteps(product, message),
			&runner.StepRecord{ID: "list", Call: "S/List", Status: runner.StatusPassed, Response: []byte(`{"products":[{"id_product":"` + listed + `"}]}`)})
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: steps("prd-847c0a1b", "no product prd-847c0a1b-unknown", "prd-847c0a1b")}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: steps("prd-b7703c2d", "no product prd-b7703c2d-unknown", "prd-99990000")}
	rep := diff.CompareMasking(spot, rec, nil)
	a := &runner.Record{RunID: "a", Chain: "c", Status: runner.StatusPassed, Steps: spot.Steps}
	runs := compareRuns(a, rec)
	for name, changes := range map[string][]diff.Change{"verify": rep.Changes, "diff": runs.Changes} {
		var list, message bool
		for _, c := range changes {
			list = list || c.Step == "list"
			message = message || c.Step == "confirm"
		}
		if (name == "verify" && !list) || message {
			t.Fatalf("%s: list reported=%v, message reported=%v (want false): %+v", name, list, message, changes)
		}
	}
}

var wantGot = regexp.MustCompile(`want=(\S+) got=(\S+)`)

func TestAStaleIdIsNotPrintedAsWantAndGotThatLookEqual(t *testing.T) {
	for name, tc := range map[string][2]string{
		"stale after a rename": {`{"customer":{"id_customer":"cus-9389f45a3d9e"}}`, `{"customer":{"id_customer":"cus-9389aa000001"}}`},
		"renamed elsewhere":    {`{"customer":{"id_customer":"cus-9389f45a3d9e"}}`, `{"customer":{"id_customer":"cus-c82a220685c5"}}`},
	} {
		spot := customerOrderSpot(tc[0],
			`{"order":{"id_customer":"cus-9389f45a3d9e"}}`,
			`{"order":{"id_customer":"cus-9389f45a3d9e"}}`)
		run := customerOrderRun(tc[1],
			`{"order":{"id_customer":"cus-9389f45a3d9e"}}`,
			`{"order":{"id_customer":"cus-9389f45a3d9e"}}`)
		rep := diff.Compare(spot, run)
		if rep.Clean() {
			t.Fatalf("%s: the order still names the safe spot's customer, got no drift", name)
		}
		seen := false
		for _, line := range strings.Split(rep.Text(), "\n") {
			if !strings.Contains(line, "[order]") || !strings.Contains(line, "id_customer") {
				continue
			}
			seen = true
			m := wantGot.FindStringSubmatch(line)
			if m == nil || m[1] == m[2] {
				t.Errorf("%s: a changed id must not print want and got as the same text: %s", name, line)
			}
		}
		if !seen {
			t.Errorf("%s: no line for order id_customer:\n%s", name, rep.Text())
		}
	}
}

func TestIdsNotRenamedConsistentlyAtOneListPathAreOneLine(t *testing.T) {
	spotBatch := `{"results":[{"id_product":"prd-aaaa1111aaaa"},{"id_product":"prd-bbbb2222bbbb"}]}`
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{
		stepAs("create_a", runner.StatusPassed, `{"product":{"id_product":"prd-aaaa1111aaaa"}}`),
		stepAs("create_b", runner.StatusPassed, `{"product":{"id_product":"prd-bbbb2222bbbb"}}`),
		stepAs("batch", runner.StatusPassed, spotBatch),
		stepAs("batch_2", runner.StatusPassed, spotBatch),
	}}
	runBatch := `{"results":[{"id_product":"prd-cccc3333cccc"},{"id_product":"prd-cccc3333cccc"}]}`
	sent := json.RawMessage(`{"lines":[{"id_product":"prd-cccc3333cccc"},{"id_product":"prd-dddd4444dddd"}]}`)
	rec := runOf("run",
		stepAs("create_a", runner.StatusPassed, `{"product":{"id_product":"prd-cccc3333cccc"}}`),
		stepAs("create_b", runner.StatusPassed, `{"product":{"id_product":"prd-dddd4444dddd"}}`),
		stepAs("batch", runner.StatusPassed, runBatch),
		stepAs("batch_2", runner.StatusPassed, runBatch))
	rec.Steps[2].Request, rec.Steps[3].Request = sent, sent
	text := diff.Compare(spot, rec).Text()
	if n := strings.Count(text, "not renamed consistently"); n != 1 {
		t.Fatalf("one line per list path, got %d:\n%s", n, text)
	}
	if !strings.Contains(text, "[batch, batch_2] changed results[].id_product at 2 items") ||
		!strings.Contains(text, "every item of results holds one value in each step, the request's lines.0.id_product") {
		t.Fatalf("the line names the steps, the path, the count and the one value every item holds:\n%s", text)
	}
}

func loginSpot(at time.Time, body string) *store.SafeSpot {
	return &store.SafeSpot{Chain: "thing-flow", RunID: at.UTC().Format("20060102T150405Z") + "-aaaa", Steps: []*runner.StepRecord{
		{ID: "login", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(body)},
	}}
}

func loginRun(at time.Time, body string) *runner.Record {
	rec := runOf(at.UTC().Format("20060102T150405Z")+"-bbbb", stepAs("login", runner.StatusPassed, body))
	rec.StartedAt = at
	rec.DurationMS = 20
	return rec
}

func TestTimestampMaskingKeepsUnitAndWindow(t *testing.T) {
	then := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	now := time.Now().Truncate(time.Second)
	secs := func(at time.Time) string { return fmt.Sprint(at.Add(time.Hour).Unix()) }
	spotBody := fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%s"}`, secs(then), then.Format(time.RFC3339), secs(then))
	same := fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%s"}`, secs(now), now.Format(time.RFC3339), secs(now))
	rep := diff.Compare(loginSpot(then, spotBody), loginRun(now, same))
	if !rep.Clean() || rep.Masked != 3 {
		t.Fatalf("now-ish timestamps of the same unit differ every run and must be masked, masked=%d:\n%s", rep.Masked, rep.Text())
	}
	ms := now.Add(time.Hour).UnixMilli()
	for name, tc := range map[string]struct{ body, line string }{
		"seconds to milliseconds": {fmt.Sprintf(`{"expires_at":%d,"issued_at":"%s","expiresAt":"%s"}`, ms, now.Format(time.RFC3339), secs(now)),
			"expires_at changed unit: seconds -> milliseconds"},
		"int64 text seconds to milliseconds": {fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%d"}`, secs(now), now.Format(time.RFC3339), ms),
			"expiresAt changed unit: seconds -> milliseconds"},
		"int64 text seconds to microseconds": {fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%d"}`, secs(now), now.Format(time.RFC3339), now.UnixMicro()),
			"expiresAt changed unit: seconds -> microseconds"},
		"seconds to RFC3339": {fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%s"}`, secs(now), now.Format(time.RFC3339), now.Format(time.RFC3339)),
			"expiresAt changed unit: seconds -> RFC3339 text"},
		"RFC3339 to seconds": {fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%s"}`, secs(now), secs(now), secs(now)),
			"issued_at changed unit: RFC3339 text -> seconds"},
		"seconds far in the future": {fmt.Sprintf(`{"expires_at":%d,"issued_at":"%s","expiresAt":"%s"}`, now.AddDate(150, 0, 0).Unix(), now.Format(time.RFC3339), secs(now)),
			"outside the time window of this run"},
		"RFC3339 far in the past": {fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%s"}`, secs(now), "1999-01-01T00:00:00Z", secs(now)),
			"issued_at is 1999-01-01T00:00:00Z, outside the time window"},
	} {
		rep := diff.Compare(loginSpot(then, spotBody), loginRun(now, tc.body))
		if rep.Clean() || rep.Counted() == 0 {
			t.Errorf("%s: a timestamp that changed unit or left the run's time window is a change, got none:\n%s", name, rep.Text())
		}
		if !strings.Contains(rep.Text(), tc.line) {
			t.Errorf("%s: verify must say %q:\n%s", name, tc.line, rep.Text())
		}
		if strings.Contains(rep.MaskedList(), "expires_at ("+secs(then)+" -> "+fmt.Sprint(ms)) {
			t.Errorf("%s: a unit change must not be listed as masked:\n%s", name, rep.MaskedList())
		}
		runs := compareRuns(loginRun(then, spotBody), loginRun(now, tc.body))
		if runs.Same() || !strings.Contains(runs.Text(), tc.line) {
			t.Errorf("%s: shrt diff must show the change and say %q:\n%s", name, tc.line, runs.Text())
		}
	}
}
