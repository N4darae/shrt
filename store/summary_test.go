package store_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func oneStep(st *runner.StepRecord) *runner.Record {
	st.Index, st.Status = 1, runner.StatusPassed
	return &runner.Record{RunID: "run-1", Chain: "c", Target: "http://localhost", Status: runner.StatusPassed, Steps: []*runner.StepRecord{st}}
}

func withEnvelope(t *testing.T, item string) {
	chain.SetEnvelope("status.code", "SUCCESS")
	chain.SetItemEnvelope(item)
	t.Cleanup(func() { chain.SetEnvelope("", ""); chain.SetItemEnvelope("") })
}

func TestProposalSummaryShowsWhatTheApproverJudges(t *testing.T) {
	growing := []string{"list_all products", "create product.sku"}
	for i := 30; i < 400; i++ {
		growing = append(growing, fmt.Sprintf("list_all products.%d.name", i), fmt.Sprintf("list_all products.%d.sku", i))
	}
	masked := passingRun("run-1")
	masked.Volatile = []string{"**.sku"}
	masked.Steps = append(masked.Steps, &runner.StepRecord{Index: 2, ID: "fetch", Call: "ThingService/Fetch", Status: runner.StatusPassed,
		Response: json.RawMessage(`{"thing":{"total":"500"},"status":{"code":"OK"}}`), Volatile: []string{"thing", "status.code"}})
	allMasked := passingRun("run-1")
	allMasked.Steps = append(allMasked.Steps, &runner.StepRecord{Index: 2, ID: "fetch", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"thing":{"total":"500"}}`)})
	allMasked.Volatile = []string{"**"}
	redacted := oneStep(&runner.StepRecord{ID: "get_product", Call: "ProductService/GetProduct",
		Request: json.RawMessage(`{"id_product":"prd-1"}`), Response: json.RawMessage(`{"product":{"qty_on_hand":"<redacted>","name":"Widget"}}`)})
	redacted.Redacted = []string{"**.qty_on_hand"}
	for _, tc := range []struct {
		name     string
		envelope bool
		rec      *runner.Record
		compared string
		unstable []string
		want     []string
		not      []string
	}{
		{name: "blank value exact", envelope: true, rec: oneStep(&runner.StepRecord{ID: "create_blank_sku", Call: "ProductService/CreateProduct",
			Request:  json.RawMessage(`{"sku":"   ","name":"a  b"}`),
			Response: json.RawMessage(`{"status":{"code":"REJECTED","message":"sku   is blank"}}`),
			Expect:   []chain.ExpectResult{{Path: "status.code", Rule: "equals", Want: "REJECTED", Got: "REJECTED", Passed: true}}}),
			want: []string{`sku="   "`, "REJECTED sku is blank"}},
		{name: "whitespace quoted", rec: oneStep(&runner.StepRecord{ID: "create", Call: "ProductService/CreateProduct",
			Request: json.RawMessage(`{"sku":" ","name":"","note":" padded","plain":"widget two"}`), Response: json.RawMessage(`{}`)}),
			want: []string{`sku=" "`, `name=""`, `note=" padded"`, `plain=widget two`}},
		{name: "long literal in full", rec: oneStep(&runner.StepRecord{ID: "create", Call: "ThingService/Create", Response: json.RawMessage(`{}`),
			Request: json.RawMessage(`{"email":"cust-order-confirm-long@example.test","name":"Customer order-confirm-long","id_customer":"cus-1234567890abcdef1234"}`)}),
			want: []string{"email=cust-order-confirm-long@example.test", "name=Customer order-confirm-long"}},
		{name: "redacted never compared", rec: redacted, want: []string{"Redacted, never compared by `shrt verify`", "get_product product.qty_on_hand"}},
		{name: "drift warning", rec: passingRun("run-2"), compared: "run-1", unstable: []string{"create product.sku"},
			want: []string{"Warning: 1 field(s) differ from the earlier passing run `run-1`", "`create product.sku`"}},
		{name: "no earlier run", rec: passingRun("run-2"), want: []string{"Not checked for fields that change every run"}},
		{name: "growing list", rec: passingRun("run-2"), compared: "run-1", unstable: growing,
			want: []string{"`list_all products`: 741 field(s)", "`volatile: [products]` on step `list_all`", "`unordered: [products]`", "`create product.sku`"}},
		{name: "same length list field", rec: passingRun("run-2"), compared: "run-1", unstable: []string{"list_customer_orders orders.0.total_minor: 750 -> 1"},
			want: []string{"`orders.0.total_minor` 750 -> 1", "may be a real change", "`volatile: [orders.*.total_minor]`"},
			not:  []string{"`volatile: [orders]`", "items change from run to run"}},
		{name: "field new in items", rec: passingRun("run-2"), compared: "run-1",
			unstable: []string{"list_orders orders.0.note: absent -> gift wrap", "list_orders orders.1.note: absent -> gift wrap"},
			want:     []string{"`volatile: [orders.*.note]`"}, not: []string{"`volatile: [orders]`", "items change from run to run"}},
		{name: "list grew", rec: passingRun("run-2"), compared: "run-1",
			unstable: []string{"list_all products: 1 item(s) -> 2 item(s)", "list_all products.1: absent -> map[name:b]"},
			want:     []string{"`volatile: [products]`", "1 item(s) -> 2 item(s)"}},
		{name: "volatile and masked steps", rec: masked, want: []string{"`**.sku`", "`thing`", "`status.code`", "every response field of step(s) fetch"},
			not: []string{"step(s) create,", "fetch, create"}},
		{name: "all masked", rec: allMasked, want: []string{"every response field of step(s) create, fetch"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envelope {
				withEnvelope(t, "")
			}
			text := store.ProposalSummary(&store.Proposal{Chain: tc.rec.Chain, RunID: tc.rec.RunID, ComparedTo: tc.compared, Unstable: tc.unstable}, tc.rec)
			for _, w := range tc.want {
				if !strings.Contains(text, w) {
					t.Errorf("%s: want %q in:\n%s", tc.name, w, text)
				}
			}
			for _, w := range tc.not {
				if strings.Contains(text, w) {
					t.Errorf("%s: want no %q in:\n%s", tc.name, w, text)
				}
			}
			if tc.name == "growing list" && strings.Count(text, "\n") > 40 {
				t.Errorf("a list of 742 changed fields must not print a line each:\n%s", text)
			}
		})
	}
}

func stepRow(t *testing.T, rec *runner.Record, index string) []string {
	text := store.ProposalSummary(&store.Proposal{Chain: rec.Chain, RunID: rec.RunID}, rec)
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "| "+index+" |") {
			return strings.Split(line, " | ")
		}
	}
	t.Fatalf("no row %s:\n%s", index, text)
	return nil
}

func TestProposalRowLeadsWithInputsAndShowsUnassertedBaselineValues(t *testing.T) {
	withEnvelope(t, "")
	rec := oneStep(&runner.StepRecord{ID: "create_order", Call: "OrderService/CreateOrder",
		Request: json.RawMessage(`{"id_customer":"cus-8f2c61d0a1b2","idempotency_key":"0b8e2f4c-6a51-4c1e-9d3e-7f0a2b4c6d8e",` +
			`"lines":[{"id_product":"prd-e7a980d9914a","qty":3},{"id_product":"prd-1c2d3e4f5a6b","qty":2}]}`),
		Response: json.RawMessage(`{"status":{"code":"SUCCESS","details":[],"message":""},"order":{"id_order":"ord-0a1b2c3d4e5f",` +
			`"status":"ORDER_STATUS_PENDING","total_minor":4548,"id_customer":"cus-8f2c61d0a1b2"}}`),
		Expect: []chain.ExpectResult{
			{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
			{Path: "order.status", Rule: "equals", Want: "ORDER_STATUS_PENDING", Got: "ORDER_STATUS_PENDING", Passed: true},
			{Path: "order.total_minor", Rule: "equals", Want: 4548, Got: 4548, Passed: true},
			{Path: "order.id_customer", Rule: "equals", Want: "cus-8f2c61d0a1b2", Got: "cus-8f2c61d0a1b2", Passed: true},
		}})
	cells := stepRow(t, rec, "1")
	if !strings.HasPrefix(cells[2], "lines.0.qty=3 lines.1.qty=2") {
		t.Errorf("the sent column must lead with the literal inputs a person judges, not ids: %q", cells[2])
	}
	if !strings.Contains(cells[4], "order.total_minor=4548") || !strings.Contains(cells[4], "order.status=ORDER_STATUS_PENDING") {
		t.Errorf("backend answered must show the values the step asserts: %q", cells[4])
	}
	if n := len([]rune(strings.Join(cells, " | "))); n > 700 {
		t.Errorf("the row must stay readable in chat: %d runes", n)
	}
	rec = oneStep(&runner.StepRecord{ID: "create_order", Call: "OrderService/CreateOrder", Request: json.RawMessage(`{"id_customer":"cus-1"}`),
		Response: json.RawMessage(`{"status":{"code":"SUCCESS","details":[],"message":""},"order":{
			"id_order":"ord-8f2a1c","id_customer":"cus-1","status":"ORDER_STATUS_PENDING","total_minor":"300",
			"created_at":"2026-09-24T19:14:11Z","updated_at":"2026-09-24T19:14:11Z","note":"for m2x",
			"lines":[{"id_product":"prd-1","qty":"1","unit_price_minor":"100"},{"id_product":"prd-2","qty":"1","unit_price_minor":"200"}]}}`),
		Expect: []chain.ExpectResult{
			{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
			{Path: "order.status", Rule: "equals", Want: "ORDER_STATUS_PENDING", Got: "ORDER_STATUS_PENDING", Passed: true},
		}})
	rec.Vars, rec.Volatile = map[string]any{"tag": "m2x"}, []string{"**.created_at"}
	row := strings.Join(stepRow(t, rec, "1"), " | ")
	if !strings.Contains(row, "also baselined: order.total_minor=300") || !strings.Contains(row, "more") {
		t.Fatalf("an unasserted business value that becomes the baseline is shown, capped with +N more:\n%s", row)
	}
	for _, hidden := range []string{"id_order", "created_at", "updated_at", "for m2x", "also baselined: order.status", "status.message"} {
		if strings.Contains(row[strings.Index(row, "also baselined"):], hidden) {
			t.Errorf("ids, timestamps, volatile values, fixture echoes and asserted paths stay out of the list (%s):\n%s", hidden, row)
		}
	}
}

func TestProposalSummaryShowsPerItemOutcomeSentAndReadableAssertions(t *testing.T) {
	withEnvelope(t, "results[].status.code")
	rec := oneStep(&runner.StepRecord{ID: "batch_one_bad_line", Call: "StockService/AddStockBatch",
		Request: json.RawMessage(`{"lines":[{"id_product":"prd-36a53e2c593c","qty":6},{"id_product":"prd-36a53e2c593c","qty":0}]}`),
		Response: json.RawMessage(`{"status":{"code":"SUCCESS","details":[],"message":""},"results":[
			{"status":{"code":"SUCCESS","details":[]}},
			{"status":{"code":"REJECTED","details":[{"app_code":1203,"reason":"InvalidQty"}],"message":"qty must be greater than zero"}},
			{"status":{"code":"REJECTED","details":[{"app_code":1203,"reason":"InvalidQty"}]}}]}`),
		Expect: []chain.ExpectResult{
			{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
			{Path: "results.0.note", Rule: "equals", Want: strings.Repeat("x", 200), Got: strings.Repeat("x", 200), Passed: true},
			{Path: "results.1.status.code", Rule: "equals", Want: "REJECTED", Got: "REJECTED", Passed: true},
			{Path: "results.1.status.details.0.app_code", Rule: "equals", Want: 1203, Got: 1203, Passed: true},
		}})
	text := store.ProposalSummary(&store.Proposal{Chain: rec.Chain, RunID: rec.RunID}, rec)
	for _, want := range []string{
		"| # | step | sent | asserted, all held | backend answered |",
		"SUCCESS; items: 1 SUCCESS, 2 REJECTED 1203 InvalidQty;",
		"| lines.0.qty=6 lines.1.qty=0 lines.0.id_product=prd-36a53e2c593c",
		"results.1.status.details.0.app_code equals 1203",
		"results.0.note equals " + strings.Repeat("x", 200) + ";",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("want %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "…") || strings.Count(text, "\n| 1 |") != 1 {
		t.Fatalf("one row per step, no asserted or answered value clipped:\n%s", text)
	}
}

func TestProposalBriefSummarisesARunInsteadOfTablingEveryStep(t *testing.T) {
	withEnvelope(t, "")
	rec := &runner.Record{RunID: "run-1", Chain: "orders", Target: "http://localhost", Status: runner.StatusPassed}
	for i := 1; i <= 40; i++ {
		rec.Steps = append(rec.Steps, &runner.StepRecord{
			Index: i, ID: fmt.Sprintf("create_order_%d", i), Call: "shop.orders.v1.OrderService/CreateOrder", Status: runner.StatusPassed,
			Request:  json.RawMessage(`{"qty":2}`),
			Response: json.RawMessage(`{"status":{"code":"SUCCESS"},"order":{"total_minor":500}}`),
			Expect: []chain.ExpectResult{
				{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
				{Path: "order.total_minor", Rule: "equals", Want: 500, Got: 500, Passed: true},
			},
		})
	}
	rec.Steps = append(rec.Steps,
		&runner.StepRecord{Index: 41, ID: "create_order_unknown", Call: "shop.orders.v1.OrderService/CreateOrder", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"status":{"code":"REJECTED","details":[{"app_code":1302,"reason":"OrderNotFound"}]}}`),
			Expect:   []chain.ExpectResult{{Path: "status.code", Rule: "not_equal", Want: "SUCCESS", Got: "REJECTED", Passed: true}}},
		&runner.StepRecord{Index: 42, ID: "fetch_order", Call: "shop.orders.v1.OrderService/FetchOrder", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"status":{"code":"SUCCESS"}}`)},
		&runner.StepRecord{Index: 43, ID: "fetch_again", Call: "shop.orders.v1.OrderService/FetchOrder", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"status":{"code":"SUCCESS"}}`), Warning: "refused in-band",
			Expect: []chain.ExpectResult{{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true}}},
	)
	p := &store.Proposal{Chain: rec.Chain, RunID: rec.RunID, Target: rec.Target, Checked: "totals are 5 x 100",
		Unstable: []string{"fetch_order order.note: a -> b"}, ComparedTo: "run-0"}
	brief := store.ProposalBrief(p, rec, "reach CreateOrder")
	if full := store.ProposalSummary(p, rec); len(brief) > len(full)/3 {
		t.Fatalf("the brief is a summary, %d bytes against the table's %d:\n%s", len(brief), len(full), brief)
	}
	for _, want := range []string{
		"43/43 steps passed", "What it does: reach CreateOrder", "totals are 5 x 100",
		"43 steps calling CreateOrder ×41, FetchOrder ×2",
		"SUCCESS ×42, REJECTED 1302 OrderNotFound ×1",
		"82 assertions, all held", "order.total_minor ×40",
		"1 step(s) assert nothing", "`fetch_order`",
		"assert only the verdict: `fetch_again`",
		"1 step(s) carry a warning", "refused in-band",
		"1 field(s) differ from the earlier passing run", "fetch_order order.note",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief lacks %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "| # | step |") || strings.Contains(brief, "create_order_17") {
		t.Fatalf("the brief does not table every step:\n%s", brief)
	}
}

func TestAProposalRowNamesWhatToCheck(t *testing.T) {
	withEnvelope(t, "results[].status.code")
	watch := func(id, code string, expect ...chain.ExpectResult) *runner.StepRecord {
		return &runner.StepRecord{ID: id, Call: "x.v1.OrderService/WatchOrder", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"messages":[{"status":{"code":"` + code + `"},"order":{"id":"o1"}}]}`), Expect: expect}
	}
	ok := chain.ExpectResult{Path: "messages.0.status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true}
	envOK := chain.ExpectResult{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true}
	for _, tc := range []struct {
		name            string
		volatile        []string
		steps           []*runner.StepRecord
		refusals, check string
		brief           string
	}{
		{"unary", []string{"**.created_at"}, []*runner.StepRecord{
			{ID: "create", Call: "x.v1.OrderService/CreateOrder", Status: runner.StatusPassed, Response: json.RawMessage(`{"status":{"code":"SUCCESS"}}`), Expect: []chain.ExpectResult{envOK}},
			{ID: "unknown", Call: "x.v1.OrderService/FetchOrder", Status: runner.StatusPassed,
				Response: json.RawMessage(`{"status":{"code":"REJECTED","details":[{"app_code":1302,"reason":"OrderNotFound"}]}}`),
				Expect:   []chain.ExpectResult{{Path: "status.code", Rule: "equals", Want: "REJECTED", Got: "REJECTED", Passed: true}}},
			{ID: "fetch", Call: "x.v1.OrderService/FetchOrder", Status: runner.StatusPassed, Response: json.RawMessage(`{"status":{"code":"SUCCESS"}}`)},
		}, "REJECTED 1302 OrderNotFound ×1", "1 step(s) assert nothing; 1 step(s) assert only the verdict", ""},
		{"streamed envelope", nil, []*runner.StepRecord{
			watch("watch", "SUCCESS", ok),
			watch("watch_record", "SUCCESS", ok, chain.ExpectResult{Path: "messages.0.order.id", Rule: "equals", Want: "o1", Got: "o1", Passed: true}),
			watch("watch_unknown", "REJECTED", chain.ExpectResult{Path: "messages.0.status.code", Rule: "not_equal", Want: "SUCCESS", Got: "REJECTED", Passed: true}),
		}, "REJECTED ×1", "1 step(s) assert only the verdict", "assert only the verdict: `watch`"},
		{"refused batch line", nil, []*runner.StepRecord{{ID: "batch", Call: "x.v1.StockService/AddStockBatch", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"status":{"code":"SUCCESS"},"results":[{"status":{"code":"SUCCESS"}},{"status":{"code":"REJECTED","details":[{"app_code":1203,"reason":"InvalidQty"}]}},{"status":{"code":"REJECTED"}}]}`),
			Expect: []chain.ExpectResult{envOK,
				{Path: "results.1.status.code", Rule: "not_equal", Want: "SUCCESS", Got: "REJECTED", Passed: true},
				{Path: "results.1.status.details.0.app_code", Rule: "equals", Want: "1203", Got: "1203", Passed: true},
			}}}, "REJECTED 1203 InvalidQty ×1", "", ""},
	} {
		rec := &runner.Record{RunID: "run-1", Chain: "c", Status: runner.StatusPassed, Volatile: tc.volatile, Steps: tc.steps}
		p := &store.Proposal{Chain: "c", RunID: "run-1", ComparedTo: "run-0"}
		row := store.ProposalRowOf(p, rec)
		if row.Steps != fmt.Sprintf("%d/%d", len(tc.steps), len(tc.steps)) || row.Refusals != tc.refusals || (tc.check != "" && row.Check != tc.check) {
			t.Errorf("%s: %+v", tc.name, row)
		}
		if tc.volatile != nil && row.Volatile != "`**.created_at`" {
			t.Errorf("%s: volatile %q", tc.name, row.Volatile)
		}
		if brief := store.ProposalBrief(p, rec, ""); tc.brief != "" && (strings.Contains(brief, "nothing at") || !strings.Contains(brief, tc.brief)) {
			t.Errorf("%s: the brief reads the streamed envelope too:\n%s", tc.name, brief)
		}
	}
}
