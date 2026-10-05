package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestUnansweredStepsAndTheChangesAfterThemAreNoVerdict(t *testing.T) {
	for name, create := range map[string]*runner.StepRecord{
		"refused authentication": {ID: "create", Status: runner.StatusError, HTTPStatus: 401, AuthRetry: runner.AuthRetryNotResent,
			Response: []byte(`{"code":"unauthenticated"}`), Error: "unauthenticated: invalid or expired token\nmore"},
		"dropped connection": {ID: "create", Status: runner.StatusError, Error: "the backend closed the connection before a response arrived (EOF)"},
	} {
		rec := &runner.Record{Steps: []*runner.StepRecord{
			{ID: "seed", Status: runner.StatusPassed, HTTPStatus: 200}, create, {ID: "list", Status: runner.StatusPassed, HTTPStatus: 200},
		}}
		after := &diff.Report{Changes: []diff.Change{{Step: "create", Path: "status", Kind: diff.KindStatus}, {Step: "list", Path: "orders", Kind: diff.KindLength}}}
		step, why, ok := unansweredOnly(rec, after)
		if !ok || step != "create" || (create.HTTPStatus == 401 && !strings.Contains(why, "refused authentication")) {
			t.Errorf("%s: got step=%q why=%q ok=%v", name, step, why, ok)
		}
		before := &diff.Report{Changes: append([]diff.Change{{Step: "seed", Path: "total", Kind: diff.KindChanged}}, after.Changes...)}
		if _, _, ok := unansweredOnly(rec, before); ok {
			t.Errorf("%s: a change before the unanswered step is evidence and must still be judged", name)
		}
	}
}

func TestCouldNotVerifySaysWhatTheUnansweredStepLeftUnjudged(t *testing.T) {
	for _, c := range []struct {
		step, why string
		steps     []*runner.StepRecord
		has, not  []string
	}{
		{"add", "the backend refused authentication: unauthenticated: token rejected", []*runner.StepRecord{
			{ID: "seed", Status: runner.StatusPassed, HTTPStatus: 200},
			{ID: "add", Status: runner.StatusError, HTTPStatus: 401, AuthRetry: runner.AuthRetryNotResent,
				Error: "unauthenticated: token rejected\n       the backend refused a token that a login in this run had just issued: " +
					"the credentials work and the token is current, so this may be an auth regression in the backend"},
		}, []string{"may be an auth regression", "login in this run"}, []string{"not a verdict about the backend", "fix the credentials"}},
		{"p", "the backend refused authentication", []*runner.StepRecord{
			{ID: "p", Status: runner.StatusError, HTTPStatus: 401, AuthRetry: runner.AuthRetryResent},
			{ID: "cust", Status: runner.StatusPassed, HTTPStatus: 200},
			{ID: "cust2", Status: runner.StatusPassed, HTTPStatus: 200},
		}, []string{"2 step(s) after it", "not judged"}, []string{"nothing past it was compared"}},
		{"p", "connection refused", []*runner.StepRecord{
			{ID: "p", Status: runner.StatusError}, {ID: "cust", Status: runner.StatusSkipped},
		}, []string{"nothing after it got an answer"}, nil},
	} {
		err := couldNotVerifyAfter("c", c.step, c.why, &runner.Record{Steps: c.steps}, nil)
		if exitCodeOf(err) != 3 {
			t.Errorf("%s: could not verify exits 3, got %v", c.why, err)
			continue
		}
		for _, w := range c.has {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: want %q in %s", c.why, w, err)
			}
		}
		for _, w := range c.not {
			if strings.Contains(err.Error(), w) {
				t.Errorf("%s: unwanted %q in %s", c.why, w, err)
			}
		}
	}
}

func verTRoots(items []gateItem) map[string]bool {
	roots := map[string]bool{}
	for _, it := range items {
		roots[it.root()] = true
	}
	return roots
}

func TestVerifyNamesTheFirstChangeAndEachDistinctRoot(t *testing.T) {
	shop := &env{cat: catalogtest.Shop()}
	spot := func(steps ...recStep) *store.SafeSpot {
		return &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: shopRecord(steps...).Steps}
	}
	for _, c := range []struct {
		list  string
		roots int
		kind  string
	}{
		{`{"products":[{"id_product":"p2","price_minor":"7"},{"id_product":"p1","price_minor":"6"}]}`, 2, reasonOrder},
		{`{"products":[{"id_product":"p1","price_minor":"6"},{"id_product":"p2","price_minor":"7"}]}`, 1, ""},
	} {
		rec := shopRecord(
			shopStep("create", shopCreate, `{"product":{"id_product":"p1","price_minor":"6"}}`),
			shopStep("create_2", shopCreate, `{"product":{"id_product":"p2","price_minor":"7"}}`),
			shopStep("list", shopList, c.list),
		)
		report := diff.Compare(spot(
			shopStep("create", shopCreate, `{"product":{"id_product":"p1","price_minor":"5"}}`),
			shopStep("create_2", shopCreate, `{"product":{"id_product":"p2","price_minor":"7"}}`),
			shopStep("list", shopList, `{"products":[{"id_product":"p1","price_minor":"5"},{"id_product":"p2","price_minor":"7"}]}`),
		), rec)
		first, _ := firstChange(report, rec)
		items := verifyItems(shop, rec, report)
		if first == nil || first.Step != "create" || first.Path != "product.price_minor" || len(verTRoots(items)) != c.roots {
			t.Errorf("list %s: first %+v, want %d root(s) in %+v", c.list, first, c.roots, items)
		}
		kinds := map[string]bool{}
		for _, it := range items {
			if it.Step == "list" {
				kinds[it.Reason.Kind] = true
			}
		}
		if c.kind != "" && !kinds[c.kind] {
			t.Errorf("the reordered list is its own root %q, got %+v", c.kind, items)
		}
	}

	spotRec := shopRecord(shopStep("list", shopList, `{"products":[{"id_product":"p1"}]}`))
	rec := shopRecord(shopStep("list", shopList, `{"products":[{"id_product":"p9"}]}`).failing("products.0.id_product", "p1", "p9"))
	report := diff.CompareMasking(&store.SafeSpot{Chain: "shop", RunID: "spot", Volatile: []string{"products"}, Steps: spotRec.Steps}, rec, nil)
	if items := verifyItems(shop, rec, report); len(items) != 1 || items[0].headline() != "products.0.id_product want=p1 got=p9" {
		t.Errorf("a status-only change is named by the expectation that failed, got %+v", items)
	}

	rec = shopRecord(
		shopStep("create_order", shopOrder, `{"order":{"id_order":"o1","lines":[{"qty":"4"}]}}`),
		shopStep("replay_same", shopOrder, `{"order":{"id_order":"o2","lines":[{"qty":"4"}]}}`).failing("order.id_order", "o1", "o2"),
		shopStep("replay_other_body", shopOrder, `{"order":{"id_order":"o3","lines":[{"qty":"7"}]}}`),
	)
	report = &diff.Report{Changes: []diff.Change{
		{Step: "replay_other_body", Path: "order.lines.0.qty", Kind: diff.KindChanged, Want: "4", Got: "7"},
		{Step: "replay_same", Path: "order.id_order", Kind: diff.KindChanged, Want: "o1", Got: "o2"},
	}}
	if first, _ := firstChange(report, rec); first == nil || first.Step != "replay_same" {
		t.Errorf("a failing step leads, got %+v", first)
	}
	if items := verifyItems(shop, rec, report); len(items) != 2 || items[0].Step != "replay_same" {
		t.Errorf("the gate items lead with the step verify names first, got %+v", items)
	}

	spotRec = shopRecord(
		shopStep("list", shopList, `{"products":[{"sku":"a"},{"sku":"b"},{"sku":"c"}]}`),
		shopStep("list_all", shopList, `{"products":[{"sku":"a"},{"sku":"b"}]}`),
	)
	rec = shopRecord(
		shopStep("list", shopList, `{"products":[{"sku":"c"},{"sku":"a"},{"sku":"b"}]}`).failing("products.0.sku", "a", "c"),
		shopStep("list_all", shopList, `{"products":[]}`).failing("products", "a", "0"),
	)
	report = diff.Compare(&store.SafeSpot{Chain: "shop", RunID: "spot", Steps: spotRec.Steps}, rec)
	if first, _ := firstChange(report, rec); first == nil || first.Step != "list_all" {
		t.Errorf("a failure that is not only another order leads over an order-only one, got %+v", first)
	}
	if items := verifyItems(shop, rec, report); len(items) == 0 || items[0].Step != "list_all" {
		t.Errorf("the gate leads with list_all too, got %+v", items)
	}
	if want, got := gatePair("  Customer a  ", "  Customer a"); want != `"  Customer a  "` || got != `"  Customer a"` {
		t.Errorf("a string whose edge spaces changed is quoted, got want=%s got=%s", want, got)
	}
}

func TestARefusedWriteOfTheSameRpcIsItsOwnRoot(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	steps := func(status, qty string) *runner.Record {
		return shopRecord(
			shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","qty_on_hand":"10"},`+shopOK+`}`),
			shopStep("create_product_2", shopCreate, `{"product":{"id_product":"p2","qty_on_hand":"10"},`+shopOK+`}`),
			shopStep("create_order", shopOrder, `{"order":{"id_order":"o1","lines":[{"id_product":"p1","qty":"2"}]},`+shopOK+`}`, "create_product"),
			shopStep("confirm_order", shopConfirm, `{"order":{"id_order":"o1","status":"`+status+`"},`+shopOK+`}`, "create_order"),
			shopStep("create_order_2", shopOrder, `{"order":{"id_order":"o2","lines":[{"id_product":"p2","qty":"20"}]},`+shopOK+`}`, "create_product_2"),
			shopStep("confirm_short", shopConfirm, `{"status":{"code":"REJECTED","details":[{"app_code":1305}]}}`, "create_order_2"),
			shopStep("get_product_2", shopGet, `{"product":{"id_product":"p2","qty_on_hand":"`+qty+`"},`+shopOK+`}`, "create_product_2"),
		)
	}
	rec := steps("PENDING", "-10")
	report := diff.Compare(&store.SafeSpot{Chain: "shop", RunID: "spot", Steps: steps("CONFIRMED", "10").Steps}, rec)
	items := verifyItems(effectsEnv(t), rec, report)
	for _, it := range items {
		if it.Step == "get_product_2" {
			if it.Reason.Kind != reasonWrite || it.Reason.Step != "confirm_short" {
				t.Errorf("the refused confirm moved stock: want suspect write confirm_short, got %+v", it.Reason)
			}
			if len(verTRoots(items)) < 2 {
				t.Errorf("it is a root apart from the confirm that answered PENDING: %+v", items)
			}
			return
		}
	}
	t.Fatalf("no item for get_product_2 in %+v", items)
}

func TestAKeepGoingRunNamesEachDistinctSuspectMostFailingStepsFirst(t *testing.T) {
	list := shopStep("list_cancelled", "shop.orders.v1.OrderService/ListOrders", `{"orders":[{"id_order":"o3"},{"id_order":"o2"},{"id_order":"o1"}]}`).failing("orders.0.id_order", "o2", "o3")
	list.Expect = append(list.Expect, chain.ExpectResult{Path: "orders.1", Rule: "exists", Want: false, Got: true})
	rec := shopRecord(
		shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`).failing("product.sku", "A", nil),
		list,
		shopStep("get_after_add", shopGet, `{"product":{"id_product":"p1"}}`, "create").heldBy("create", "product.sku"),
		shopStep("get_again", shopGet, `{"product":{"id_product":"p1"}}`, "create").heldBy("create", "product.sku"),
	)
	for _, st := range rec.Steps {
		st.Request = json.RawMessage(`{"n":1}`)
	}
	rec.Status, rec.KeepGoing = runner.StatusFailed, true
	lines := failureRequests(nil, rec, false)
	if len(lines) != 3 || lines[0] != `suspect write create (CreateProduct), sent {"n":1}` || !strings.HasPrefix(lines[1], "suspect read list_cancelled (") {
		t.Fatalf("one line per distinct suspect, the one behind most failing steps first, got %q", lines)
	}
	if failureRequests(nil, rec, true) != nil {
		t.Fatal("a dry run sent nothing, so it names no request")
	}
}

func TestVerifyTellsAWriteFromTheReadThroughAnotherRead(t *testing.T) {
	steps := func(qty string) *runner.Record {
		return shopRecord(
			shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","qty_on_hand":"0"}}`),
			shopStep("add_stock", shopAdd, `{"qty_on_hand":"10"}`, "create_product"),
			shopStep("get", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"`+qty+`"}}`, "create_product"),
		)
	}
	rec := steps("9")
	report := diff.Compare(&store.SafeSpot{Chain: "shop", RunID: "spot", Steps: steps("10").Steps}, rec)
	shop := &env{cat: catalogtest.Shop()}
	line, _ := verifyVerdict(shop, "shop", rec, report, verifyItems(shop, rec, report), nil, false, errors.New("shop: regression"), "")
	if !strings.Contains(line, "; unclear: write add_stock (AddStock) or the read") ||
		!strings.Contains(line, "\n  tell them apart: read product.qty_on_hand through ListProducts (products[].qty_on_hand)\n") {
		t.Errorf("got:\n%s", line)
	}
}

func TestARefusedReadsWantIsItsShapeNotTheApprovedValues(t *testing.T) {
	step := func(response string) *runner.StepRecord {
		return &runner.StepRecord{ID: "fetch", Call: "shrt.test.v1.ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(response)}
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{step(`{"error":{"code":"OK"},"order":{"id_order":"ord-d475"},"note":"n"}`)}}
	rec := &runner.Record{Chain: "c", RunID: "run", Status: runner.StatusFailed, Steps: []*runner.StepRecord{step(`{"error":{"code":"NOT_FOUND"},"order":null}`)}}
	rec.Steps[0].Status = runner.StatusFailed
	report := diff.Compare(spot, rec)
	noteRefused(report, rec)
	text := report.Text()
	for _, want := range []string{"[fetch] type order want=object got=null: refused NOT_FOUND",
		"[fetch] missing note want=string got=absent: refused NOT_FOUND"} {
		if !strings.Contains(text, want) {
			t.Errorf("want %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ord-d475") {
		t.Errorf("the approved run's ids are not shown for a refused read:\n%s", text)
	}
	rec.Steps[0].Response = json.RawMessage(`{"error":{"code":"OK"},"order":null}`)
	report = diff.Compare(spot, rec)
	noteRefused(report, rec)
	if !strings.Contains(report.Text(), "ord-d475") {
		t.Errorf("an answered read that lost its object shows what the approved run had:\n%s", report.Text())
	}
}
