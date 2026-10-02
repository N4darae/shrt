package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

func TestSliceVerdictSaysAClockBoundWasComparedByDistance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "login.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: shrt/v1
name: login
steps:
    - id: login
      call: AuthService/Login
      expect:
          - path: status.code
            equals: SUCCESS
          - path: expires_at
            within:
                of: ${nowunix+3600}
                by: 5
`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	verdict := func(want, got string) chain.Verdict {
		return chain.Verdict{Step: "login", Status: "failed", Expect: []chain.ExpectResult{
			{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
			{Path: "expires_at", Rule: "within", Want: want, Got: got},
		}}
	}
	res := &chain.SliceResult{Target: "login", Chain: c}
	for _, tc := range []struct {
		name, sourceGot, sliceGot string
		want                      []string
	}{
		{"same distance", "1790352732", "1790352760", []string{"got source 1790352732, slice 1790352760", "distance source bound+3s, slice bound+3s", "same distance, match"}},
		{"timestamps", "1790352729682", "1790352757712", []string{"source 1790352729682, slice 1790352757712",
			"source bound+1.788562376953e+12s, slice bound+1.788562404955e+12s", "matched as timestamps"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, replay := verdict("1790352729 ± 5", tc.sourceGot), verdict("1790352757 ± 5", tc.sliceGot)
			same := sameUpToFixtures(nil, nil)
			if diffs := compareSliceVerdicts(res, source, replay, same); len(diffs) != 0 {
				t.Fatalf("the verdicts match: %v", diffs)
			}
			v := &sliceVerdict{Step: "login", Outcome: sliceReproduced, Source: source, Replay: replay, SourceRun: "a", SliceRun: "b",
				ByDistance: clockDistanceLines(res, source, replay, same)}
			out := v.text()
			for _, want := range append(tc.want, "compared by distance from the bound: expires_at within") {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestTheNextCommandAsksForAFreshValueOfAnInterpolatedVar(t *testing.T) {
	res := &chain.SliceResult{
		Source: "orders", Target: "create_order",
		Chain: &chain.Chain{
			Vars: map[string]any{"tag": "T0", "qty": 2},
			Steps: []*chain.Step{
				{ID: "create_product", Body: map[string]any{"sku": "SKU-${vars.tag}", "qty": "${vars.qty}"}},
			},
		},
	}
	got := sliceVerifyCommand(res, "r1", sliceVerifyArgs{vars: map[string]any{"qty": 3}}, []string{"add_stock"})
	if !strings.Contains(got, "-var tag=<fresh>") {
		t.Fatalf("tag makes created names unique and the source run used it, so pasting it again collides: %s", got)
	}
	if !strings.Contains(got, "-var qty=3") {
		t.Fatalf("a var used as a whole value keeps the value the user gave: %s", got)
	}
}

func TestTheFreshnessRefusalNamesTheValueTheSourceRunUsed(t *testing.T) {
	source := &chain.Chain{Name: "wide", Vars: map[string]any{"tag": "w1"}}
	res := &chain.SliceResult{FreshVars: []string{"tag"}, Chain: &chain.Chain{Vars: map[string]any{"tag": "w1"}}}
	rec := &runner.Record{RunID: "R1", Vars: map[string]any{"tag": "sl1"}}
	err := freshVarsError(res, source, rec, varFlags{})
	if err == nil {
		t.Fatal("a kept write interpolating tag must be refused without a fresh -var")
	}
	msg := err.Error()
	if !strings.Contains(msg, "tag=sl1, the value run R1 used") {
		t.Fatalf("the source run created with tag=sl1, so that is the value that is not fresh: %s", msg)
	}
	if strings.Contains(msg, "tag=w1") {
		t.Fatalf("the chain default w1 is not what the source run created with: %s", msg)
	}
	given := freshVarsError(res, source, rec, varFlags{"tag": "sl1"})
	if given == nil || !strings.Contains(given.Error(), "-var tag=sl1 is the value run R1 used") {
		t.Fatalf("a -var equal to the source run's value is not fresh and must be refused before anything is sent: %v", given)
	}
	if err := freshVarsError(res, source, rec, varFlags{"tag": "sl2"}); err != nil {
		t.Fatalf("a -var the source run did not use is fresh: %v", err)
	}
	same := &runner.Record{RunID: "R2", Vars: map[string]any{"tag": "w1"}}
	if msg := freshVarsError(res, source, same, varFlags{}).Error(); !strings.Contains(msg, "tag=w1, the value run R2 used") {
		t.Fatalf("a run that used the default is named as the run's value: %s", msg)
	}
}

func TestSliceVerdictMasksIDsAndFixturesInsideObjectOperands(t *testing.T) {
	verdict := func(path string, want any) chain.Verdict {
		return chain.Verdict{Step: "list", Status: "failed", Expect: []chain.ExpectResult{
			{Path: path, Rule: "includes", Want: want, Got: 0},
		}}
	}
	same := sameUpToFixtures(map[string]any{"tag": "xz1"}, map[string]any{"tag": "xz2"})
	for _, tc := range []struct {
		name, path   string
		source, echo any
		alike        bool
	}{
		{"fresh id", "products", map[string]any{"id_product": "prd-38bc1a2b3c4d"}, map[string]any{"id_product": "prd-64b2e5f6a7b8"}, true},
		{"fixture name", "products", map[string]any{"sku": "sku-xz1-2", "qty": float64(2)}, map[string]any{"sku": "sku-xz2-2", "qty": 2}, true},
		{"one_of list", "product.id_product", []any{"prd-38bc1a2b3c4d", "prd-11aa22bb33cc"}, []any{"prd-64b2e5f6a7b8", "prd-44dd55ee66ff"}, true},
		{"another value", "products", map[string]any{"id_product": "prd-38bc1a2b3c4d", "name": "a"}, map[string]any{"id_product": "prd-64b2e5f6a7b8", "name": "b"}, false},
		{"another key", "products", map[string]any{"id_product": "prd-38bc1a2b3c4d"}, map[string]any{"id_order": "prd-64b2e5f6a7b8"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diffs := chain.CompareVerdictsMasking(verdict(tc.path, tc.source), verdict(tc.path, tc.echo), same)
			if (len(diffs) == 0) != tc.alike {
				t.Fatalf("alike=%v, differences: %v", tc.alike, diffs)
			}
		})
	}
}

func TestTheNextCommandLeavesOutAKeptStepThatFailedInTheSourceRun(t *testing.T) {
	res := &chain.SliceResult{
		Source: "confirm-all-lines", Target: "stock_b",
		DroppedWrites: []chain.Dropped{{Index: 3, ID: "stock", Call: "StockService/AddStockBatch"}},
		UnderIncluded: true,
		Chain: &chain.Chain{Steps: []*chain.Step{
			{ID: "confirm", Call: "OrderService/ConfirmOrder"},
			{ID: "stock_b", Call: "ProductService/GetProduct"},
		}},
	}
	rec := &runner.Record{RunID: "r1", Steps: []*runner.StepRecord{
		{Index: 3, ID: "stock", Status: runner.StatusPassed},
		{Index: 6, ID: "confirm", Status: runner.StatusFailed},
		{Index: 7, ID: "stock_b", Status: runner.StatusFailed},
	}}
	v := &sliceVerdict{Step: "stock_b"}
	v.suggestKeep(res, rec, sliceVerifyArgs{keep: []string{"confirm"}}, []string{"stock"})
	if v.Next == "" {
		t.Fatalf("stock passed in the source run, so a next: command can keep it: %+v", v)
	}
	if strings.Contains(v.Next, "-keep confirm") || strings.Contains(v.Next, ",confirm") {
		t.Fatalf("confirm failed in the source run, so a next: command keeping it stops there and can only give DID NOT RUN: %s", v.Next)
	}
	if !strings.Contains(v.Next, "-keep stock ") {
		t.Fatalf("next: must keep the dropped write that passed: %s", v.Next)
	}
	if !strings.Contains(v.Reason, "left out of next: confirm (failed)") {
		t.Fatalf("the reason must say the -keep id was left out and why: %s", v.Reason)
	}
}

func TestAnInconclusiveSliceNamesTheWithoutCommandForTheNearestDroppedWrite(t *testing.T) {
	res := &chain.SliceResult{
		Source: "confirm-all-lines", Target: "stock_b",
		DroppedWrites: []chain.Dropped{{Index: 2, ID: "stock", Call: "StockService/AddStock"}, {Index: 4, ID: "cancel", Call: "OrderService/CancelOrder"}},
		UnderIncluded: true,
		Chain:         &chain.Chain{Steps: []*chain.Step{{ID: "stock_b", Call: "ProductService/GetProduct"}}},
	}
	rec := &runner.Record{RunID: "r1", Steps: []*runner.StepRecord{
		{Index: 2, ID: "stock", Status: runner.StatusPassed},
		{Index: 4, ID: "cancel", Status: runner.StatusPassed},
		{Index: 7, ID: "stock_b", Status: runner.StatusFailed},
	}}
	v := &sliceVerdict{Step: "stock_b"}
	v.suggestKeep(res, rec, sliceVerifyArgs{}, []string{"stock", "cancel"})
	if want := "shrt chain slice confirm-all-lines -without cancel -verify -run r1"; v.Prove != want {
		t.Fatalf("prove %q, want %q", v.Prove, want)
	}
}

func TestSliceWithoutVerifyRunLatestComparesTheReplayThatFailed(t *testing.T) {
	stockWorkspace(t, 0)
	e, err := loadEnv(true)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := e.store.LatestRun("stock")
	if err != nil {
		t.Fatal(err)
	}
	own := copyRun(t, failed, "29990101T000000Z-own00001")
	own.Status = runner.StatusPassed
	for _, st := range own.Steps {
		st.Status = runner.StatusPassed
	}
	replay := copyRun(t, failed, "29990101T000001Z-replay07")
	replay.ReplayOf = own.RunID
	for _, rec := range []*runner.Record{own, replay} {
		if _, err := e.store.SaveRun(rec); err != nil {
			t.Fatal(err)
		}
	}
	out, _ := slcSlice(t, false, "stock", "-without", "stray_add", "-verify", "-run", "latest")
	if !strings.Contains(out, "failed in source run "+replay.RunID) {
		t.Fatalf("-run latest compares the replay in which steps failed:\n%s", out)
	}
	out, _ = slcSlice(t, false, "stock", "-without", "stray_add", "-verify", "-run", own.RunID)
	want := "no step left in failed in source run " + own.RunID + "; newer record " + replay.RunID + ": 3 steps failed, pass -run " + replay.RunID
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}

func TestSliceFailedLinePrintsObjectOperandsAsCompactJSON(t *testing.T) {
	want := map[string]any{"id_product": "p-1", "qty": float64(2), "note": "a<b"}
	got := map[string]any{"id_product": "p-1", "qty": float64(3), "note": "a<b"}
	list := []any{"x", map[string]any{"k": true}}
	source := chain.Verdict{Expect: []chain.ExpectResult{
		{Path: "order.lines.0", Rule: "equals", Want: want, Got: got},
		{Path: "tags", Rule: "equals", Want: list, Got: []any{"x"}},
	}}
	replay := chain.Verdict{Expect: []chain.ExpectResult{
		{Path: "order.lines.0", Rule: "equals", Want: want, Got: got},
		{Path: "tags", Rule: "equals", Want: list, Got: []any{"x"}},
		{Path: "order", Rule: "equals", Want: map[string]any{"status": "OPEN"}, Got: map[string]any{"status": "DONE"}},
	}}
	lines := strings.Join(failedExpectLines(source, replay), "\n")
	for _, s := range []string{
		`failed: order.lines.0 want={"id_product":"p-1","note":"a<b","qty":2} source got={"id_product":"p-1","note":"a<b","qty":3}, slice got={"id_product":"p-1","note":"a<b","qty":3}`,
		`failed: tags want=["x",{"k":true}] source got=["x"], slice got=["x"]`,
		`failed in the slice only: order want={"status":"OPEN"} got={"status":"DONE"}`,
	} {
		if !strings.Contains(lines, s) {
			t.Errorf("want line %s\nin:\n%s", s, lines)
		}
	}
	if strings.Contains(lines, "map[") {
		t.Errorf("object operands must not print in Go map format:\n%s", lines)
	}
}

func TestASliceFailureReadingAnOrderNoStepCreatedIsACaveat(t *testing.T) {
	create := &runner.StepRecord{ID: "create_order", Status: runner.StatusPassed,
		Request:  json.RawMessage(`{"id_customer":"cus-111111111111"}`),
		Response: json.RawMessage(`{"order":{"id_order":"ord-aaaaaaaaaaa1","id_customer":"cus-111111111111"}}`)}
	list := &runner.StepRecord{ID: "list_orders_cancelled", Status: runner.StatusFailed,
		Request: json.RawMessage(`{"id_customer":"cus-111111111111","status":"ORDER_STATUS_CANCELLED"}`),
		Response: json.RawMessage(`{"orders":[{"id_order":"ord-ffffffffff99","id_customer":"cus-999999999999","status":"ORDER_STATUS_CANCELLED"},` +
			`{"id_order":"ord-aaaaaaaaaaa1","id_customer":"cus-111111111111","status":"ORDER_STATUS_PENDING"}]}`),
		Expect: []chain.ExpectResult{
			{Path: "orders.0.id_order", Rule: "equals", Want: "ord-aaaaaaaaaaa1", Got: "ord-ffffffffff99"},
			{Path: "orders.1", Rule: "exists", Want: false, Got: true},
		}}
	run := &runner.Record{RunID: "slice", Steps: []*runner.StepRecord{create, list}}
	got := outsideState(run, list)
	joined := strings.Join(got, "; ")
	for _, want := range []string{"orders.0.id_order ord-ffffffffff99", "orders.0.id_customer cus-999999999999"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the other customer's cancelled order came from outside the slice, want %q in %v", want, got)
		}
	}
	if strings.Contains(joined, "ord-aaaaaaaaaaa1") || strings.Contains(joined, "cus-111111111111") {
		t.Errorf("an order the slice created is not outside state: %v", got)
	}
	if line := outsideStateCaveat(list.ID, got); !strings.Contains(line, "fails on server state the slice did not create") {
		t.Errorf("the caveat says so: %s", line)
	}
	list.Response = json.RawMessage(`{"orders":[{"id_order":"ord-aaaaaaaaaaa1","id_customer":"cus-111111111111","status":"ORDER_STATUS_PENDING"},{"id_order":"ord-aaaaaaaaaaa1"}]}`)
	list.Expect = []chain.ExpectResult{{Path: "orders.1", Rule: "exists", Want: false, Got: true}}
	if got := outsideState(run, list); len(got) != 0 {
		t.Errorf("a failure over what the slice created carries no caveat: %v", got)
	}
}

func TestAnIdTheFailingWriteItselfAnsweredIsNotOutsideState(t *testing.T) {
	create := &runner.StepRecord{ID: "create_order", Call: "shop.orders.v1.OrderService/CreateOrder", Status: runner.StatusPassed,
		Request:  json.RawMessage(`{"idempotency_key":"k-1"}`),
		Response: json.RawMessage(`{"order":{"id_order":"ord-aaaaaaaaaaa1"}}`)}
	replay := &runner.StepRecord{ID: "create_order_replay", Call: "shop.orders.v1.OrderService/CreateOrder", Status: runner.StatusFailed,
		Request:  json.RawMessage(`{"idempotency_key":"k-1"}`),
		Response: json.RawMessage(`{"order":{"id_order":"ord-bbbbbbbbbbb2"}}`),
		Expect:   []chain.ExpectResult{{Path: "order.id_order", Rule: "equals", Want: "ord-aaaaaaaaaaa1", Got: "ord-bbbbbbbbbbb2"}}}
	run := &runner.Record{RunID: "slice", Steps: []*runner.StepRecord{create, replay}}
	if got := outsideState(run, replay); len(got) != 0 {
		t.Errorf("the replay created ord-bbbbbbbbbbb2 itself, inside the slice: %v", got)
	}
	replay.Call = "shop.orders.v1.OrderService/FetchOrder"
	if got := outsideState(run, replay); len(got) != 1 {
		t.Errorf("a read answering an id no step sent or received did not create it: %v", got)
	}
}

func TestAStepSentAndRefusedAsErrorIsReached(t *testing.T) {
	rec := &runner.Record{RunID: "r1", Status: runner.StatusError, Steps: []*runner.StepRecord{
		{ID: "write_3", Status: runner.StatusError, HTTPStatus: 401, Request: json.RawMessage(`{"email":"w3@example.test"}`),
			Response:  json.RawMessage(`{"code":"unauthenticated","message":"invalid or expired token"}`),
			Transport: &runner.TransportError{Code: "unauthenticated", Message: "invalid or expired token"}},
		{ID: "write_4", Status: runner.StatusSkipped},
		{ID: "unresolved", Status: runner.StatusError, Error: "unresolved reference ${x.y}"},
	}}
	if ok, why := reachedStep(rec, "write_3"); !ok {
		t.Fatalf("write_3 was sent and the backend answered it, so the run reached it: %s", why)
	}
	if ok, _ := reachedStep(rec, "write_4"); ok {
		t.Fatal("a skipped step was never sent")
	}
	if ok, _ := reachedStep(rec, "unresolved"); ok {
		t.Fatal("a step that errored before anything was sent was not reached")
	}
}

func TestAWriteOnAnOrderActsOnTheProductsItsLinesName(t *testing.T) {
	c := &chain.Chain{Name: "stock", Steps: []*chain.Step{
		{ID: "create_product", Call: "ProductService/CreateProduct", Body: map[string]any{"sku": "a"}},
		{ID: "create_order", Call: "OrderService/CreateOrder", Body: map[string]any{"lines": []any{map[string]any{"id_product": "${create_product.product.id_product}"}}}},
		{ID: "confirm_order", Call: "OrderService/ConfirmOrder", Body: map[string]any{"id_order": "ord-1"}},
		{ID: "cancel_order", Call: "OrderService/CancelOrder", Body: map[string]any{"id_order": "ord-1"}},
		{ID: "get_product", Call: "ProductService/GetProduct", Body: map[string]any{"id_product": "${create_product.product.id_product}"}},
	}}
	step := func(id, call, req, resp string) *runner.StepRecord {
		return &runner.StepRecord{ID: id, Call: call, Status: runner.StatusPassed, HTTPStatus: 200, Request: []byte(req), Response: []byte(resp)}
	}
	order := `{"order":{"id_order":"ord-1","lines":[{"id_product":"prd-1","qty":"2"}]}}`
	rec := &runner.Record{RunID: "src", Chain: "stock", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		step("create_product", "ProductService/CreateProduct", `{"sku":"a"}`, `{"product":{"id_product":"prd-1","sku":"a"}}`),
		step("create_order", "OrderService/CreateOrder", `{"lines":[{"id_product":"prd-1"}]}`, order),
		step("confirm_order", "OrderService/ConfirmOrder", `{"id_order":"ord-1"}`, order),
		step("cancel_order", "OrderService/CancelOrder", `{"id_order":"ord-1"}`, order),
		step("get_product", "ProductService/GetProduct", `{"id_product":"prd-1"}`, `{"product":{"id_product":"prd-1","qty_on_hand":"8"}}`),
	}}
	prereqs := func(rpc string) []chain.Prereq {
		if rpc == "OrderService/ConfirmOrder" {
			return []chain.Prereq{{RPC: "StockService/AddStock", Edge: "needs"}}
		}
		return nil
	}
	res, err := chain.Slice(c, "get_product", chain.SliceOptions{RunID: rec.RunID, Refused: refusedIn(rec), Prereqs: prereqs})
	if err != nil {
		t.Fatal(err)
	}
	related, other := relatedDroppedWrites(res, rec)
	if !slices.Contains(related, "confirm_order") {
		t.Fatalf("confirm_order changes an existing order and, as its contract needs AddStock, the stock of the product on its line, which the target reads: related %v, other %v", related, other)
	}
}

func TestSliceNextIsWithheldWhenItsClosurePullsInAStepThatErredInTheSourceRun(t *testing.T) {
	c := &chain.Chain{Name: "flow", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create"},
		{ID: "stock", Call: "StockService/AddStock", Body: map[string]any{"id": "${create.id}"}},
		{ID: "order", Call: "OrderService/CreateOrder", Body: map[string]any{"id": "${create.id}"}},
		{ID: "confirm", Call: "OrderService/ConfirmOrder", Body: map[string]any{"id": "${order.id}"}},
		{ID: "get", Call: "ThingService/Fetch", Body: map[string]any{"id": "${create.id}"}},
	}}
	res, err := chain.Slice(c, "get", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rec := &runner.Record{RunID: "r1", Steps: []*runner.StepRecord{
		{ID: "create", Status: runner.StatusPassed},
		{ID: "stock", Status: runner.StatusPassed},
		{ID: "order", Status: runner.StatusError, Error: "connection reset"},
		{ID: "confirm", Status: runner.StatusPassed},
		{ID: "get", Status: runner.StatusFailed},
	}}
	v := &sliceVerdict{Step: "get"}
	v.suggestKeep(res, rec, sliceVerifyArgs{reslice: func(keep []string) *chain.SliceResult {
		next, err := chain.Slice(c, "get", chain.SliceOptions{Keep: keep})
		if err != nil {
			return nil
		}
		return next
	}}, []string{"stock", "confirm"})
	if v.Next != "" {
		t.Fatalf("keeping confirm pulls in order, which erred in the source run, so the command would stop there: %s", v.Next)
	}
	if !strings.Contains(v.Reason, "order") || !strings.Contains(v.Reason, "error") {
		t.Fatalf("the reason must name the step that would stop the slice and why: %s", v.Reason)
	}
}

func TestSliceRunLatestPicksTheRecordASliceShouldCompare(t *testing.T) {
	for _, c := range []struct {
		name    string
		replays []string
		fetch   string
		without bool
		picked  string
		err     []string
		note    []string
	}{
		{name: "the chain's own run over a verify replay", replays: []string{"29990101T000000Z-replay01"}, picked: "base",
			note: []string{"the newest `shrt run` record", "29990101T000000Z-replay01, is a `shrt verify` replay", "pass -run 29990101T000000Z-replay01"}},
		{name: "a newer replay that did not reach the step is refused", replays: []string{"29990101T000000Z-replay02"}, fetch: runner.StatusSkipped,
			err: []string{"run 29990101T000000Z-replay02, which did not evaluate step fetch", "-run BASE"}},
		{name: "the newer replay in which only it failed the step", replays: []string{"29990101T000000Z-replay03"}, fetch: runner.StatusFailed, picked: "29990101T000000Z-replay03",
			note: []string{"run 29990101T000000Z-replay03, the newest record, a `shrt verify` replay in which fetch failed", "BASE, it passed"}},
		{name: "the newest replay when no run was recorded beside it", replays: []string{"29990101T000000Z-replay04", "29990101T000001Z-replay05"}, picked: "29990101T000001Z-replay05",
			note: []string{"as shrt diff picks it", "-run BASE"}},
		{name: "-without picks the newer replay in which steps failed", replays: []string{"29990101T000000Z-replay06"}, fetch: runner.StatusFailed, without: true, picked: "29990101T000000Z-replay06",
			note: []string{"replay in which 1 step failed", "BASE, no step failed"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, e, base := approvedThingFlowRun(t)
			for _, id := range c.replays {
				replay := copyRun(t, base, id)
				replay.ReplayOf = base.RunID
				for _, st := range replay.Steps {
					if st.ID == "fetch" && c.fetch != "" {
						st.Status = c.fetch
					}
				}
				if _, err := e.store.SaveRun(replay); err != nil {
					t.Fatal(err)
				}
			}
			var rec *runner.Record
			var err error
			note := captureStderr(t, func() {
				if c.without {
					rec, err = latestRun(e, "cli-thing-flow", "")
				} else {
					rec, err = loadRunReaching(e, "cli-thing-flow", "cli-thing-flow", "latest", "fetch")
				}
			})
			fill := func(s string) string { return strings.ReplaceAll(s, "BASE", base.RunID) }
			if len(c.err) > 0 {
				for _, w := range c.err {
					if exitCodeOf(err) != 3 || !strings.Contains(err.Error(), fill(w)) {
						t.Fatalf("want exit 3 with %q, got %v", fill(w), err)
					}
				}
				if note != "" {
					t.Fatalf("a refusal needs no note: %q", note)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := strings.ReplaceAll(c.picked, "base", base.RunID); rec.RunID != want {
				t.Fatalf("picked %s, want %s", rec.RunID, want)
			}
			for _, w := range c.note {
				if !strings.Contains(note, fill(w)) {
					t.Errorf("missing %q in %q", fill(w), note)
				}
			}
			if c.without {
				if got := newerFailing(e, base); got == nil || got.RunID != c.picked {
					t.Fatalf("newerFailing = %v", got)
				}
			}
		})
	}
}

func TestRefusedFailureNeedsARefusedStepThatDidNotPass(t *testing.T) {
	refused := []transport.TokenRefusal{{Token: "t", Cached: true}}
	for _, tc := range []struct {
		name  string
		steps []*runner.StepRecord
		want  bool
	}{
		{"refusal recovered on a passed step, assertion failure elsewhere", []*runner.StepRecord{
			{ID: "prod_a", Status: runner.StatusPassed, TokenRefused: refused},
			{ID: "confirm", Status: runner.StatusFailed},
		}, false},
		{"the failing step was refused", []*runner.StepRecord{
			{ID: "read", Status: runner.StatusFailed, TokenRefused: refused},
		}, true},
	} {
		if got := refusedFailure(&runner.Record{Steps: tc.steps}); got != tc.want {
			t.Errorf("%s: refusedFailure = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAPartlyClearedWithoutIsInconclusiveWhenTheRestReadTheLeftOutWrites(t *testing.T) {
	v := &withoutVerdict{Without: []string{"stray_add"}, Cleared: []string{"fetch_total"}, StillFail: []string{"fetch_name"}, readsOut: true}
	if err := v.err(); exitCodeOf(err) != 3 || !strings.Contains(err.Error(), "INCONCLUSIVE without stray_add: 1 failing step(s) still fail") {
		t.Fatalf("exit %d: %v", exitCodeOf(err), err)
	}
}

func TestRepeatsCombineOverTheRunsThatCount(t *testing.T) {
	rv := func(outcome, broke string, matched bool) *sliceVerdict {
		return &sliceVerdict{Step: "get", SourceRun: "src", Outcome: outcome, brokeWhy: broke, matched: matched, SliceRun: "run-" + outcome}
	}
	cases := []struct {
		name     string
		verdicts []*sliceVerdict
		outcome  string
		head     string
		not      string
	}{
		{"a broken kept step leaves its repeat out", []*sliceVerdict{rv(sliceReproduced, "", true), rv(sliceInconclusive, "kept step add failed, unavailable", true), rv(sliceReproduced, "", true)},
			sliceReproduced, "verify reproduced 2/2 (repeat 2 not counted: kept step add failed, unavailable): step get", "slice runs"},
		{"a counted miss is intermittent and gives the details", []*sliceVerdict{rv(sliceReproduced, "", true), rv(sliceDidNotRun, "", false), rv(sliceNotReproduced, "", false)},
			sliceIntermittent, "verify intermittent: reproduced 1/2 (repeat 2 not counted: did not run): step get, source run src, details from slice run run-not_reproduced", ""},
		{"a matched inconclusive beside reproduced runs is not intermittent", []*sliceVerdict{rv(sliceReproduced, "", true), rv(sliceInconclusive, "", true), rv(sliceInconclusive, "", true)},
			sliceInconclusive, "verify INCONCLUSIVE: step get, source run src, the verdict matched in 3 of 3 slice runs,", "1/3"},
		{"no count on a run of misses", []*sliceVerdict{rv(sliceNotReproduced, "", false), rv(sliceNotReproduced, "", false)},
			sliceNotReproduced, "verify NOT REPRODUCED: step get, source run src, 2 slice runs,", "0/2"},
		{"every repeat broken stays inconclusive", []*sliceVerdict{rv(sliceInconclusive, "kept step add failed, internal", false), rv(sliceInconclusive, "kept step add failed, internal", false)},
			sliceInconclusive, "verify INCONCLUSIVE: step get, source run src, 2 slice runs,", "0/2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, _ := combineSliceVerdicts(c.verdicts)
			text := v.text()
			if v.Outcome != c.outcome || !strings.Contains(text, c.head) || c.not != "" && strings.Contains(text, c.not) {
				t.Fatalf("outcome %s:\n%s", v.Outcome, text)
			}
		})
	}
}

func TestAStreamedStepsVerdictIsReadFromItsMessages(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	step := func(response string) *runner.StepRecord {
		return &runner.StepRecord{ID: "watch", Status: runner.StatusFailed, Response: json.RawMessage(response)}
	}
	ok := step(`{"messages":[{"status":{"code":"SUCCESS"},"order":{"id_order":"o1"}},{"status":{"code":"SUCCESS"}}]}`)
	refused := step(`{"messages":[{"status":{"code":"SUCCESS"}},{"status":{"code":"REJECTED","message":"gone","details":[{"app_code":1302,"reason":"OrderNotFound"}]}}]}`)
	for _, c := range []struct {
		name, code, refusal string
		st                  *runner.StepRecord
	}{
		{"every message answered", "SUCCESS", "", ok},
		{"a message refused", "REJECTED", "1302 OrderNotFound", refused},
		{"a unary answer", "REJECTED", "1302", step(`{"status":{"code":"REJECTED","details":[{"app_code":1302}]}}`)},
	} {
		if v := verdictOf(c.st); v.ErrorCode != c.code || refusalOf(c.st) != c.refusal {
			t.Errorf("%s: got %q refusal %q, want %q %q", c.name, v.ErrorCode, refusalOf(c.st), c.code, c.refusal)
		}
	}
	if got := verdictPath(ok); got != "messages[].status.code" {
		t.Errorf("a streamed step's envelope is labelled %q", got)
	}
	v := &sliceVerdict{Step: "watch", Outcome: sliceReproduced, EnvelopePath: verdictPath(ok), Source: verdictOf(ok), Replay: verdictOf(ok), SourceRun: "a", SliceRun: "b"}
	if out := v.text(); !strings.Contains(out, `source: status failed, messages[].status.code "SUCCESS"`) {
		t.Errorf("%s", out)
	}
}
