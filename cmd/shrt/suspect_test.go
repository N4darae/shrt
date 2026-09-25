package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestASuspectWriteIsTheWriteAReadObserves(t *testing.T) {
	step := func(id, call string, refs ...string) *runner.StepRecord {
		st := &runner.StepRecord{ID: id, Call: "x.v1.S/" + call, BodyRefs: map[string]string{}}
		for i, r := range refs {
			st.BodyRefs[string(rune('a'+i))] = "${" + r + ".id}"
		}
		return st
	}
	rec := &runner.Record{Steps: []*runner.StepRecord{
		step("create_item", "CreateItem"),
		step("create_box", "CreateBox", "create_item"),
		step("move_box", "MoveBox", "create_box"),
		step("fill_item", "FillItem", "create_item"),
		step("get_item", "GetItem", "create_item"),
		step("get_item_after_move_box", "GetItem", "create_item"),
		step("list_items", "ListItems"),
	}}
	for _, c := range []struct {
		read, bad, want string
		knock           bool
	}{
		{"get_item", "", "fill_item", false},
		{"get_item", "move_box", "move_box", false},
		{"get_item_after_move_box", "fill_item", "move_box", false},
		{"list_items", "create_box", "create_box", true},
		{"list_items", "", "", false},
		{"fill_item", "", "", false},
	} {
		i, knock := suspectWrite(rec, c.read, map[string]bool{c.bad: c.bad != ""})
		got := ""
		if i >= 0 {
			got = rec.Steps[i].ID
		}
		if got != c.want || knock != c.knock {
			t.Errorf("%s with %q failing: got %q knock-on %v, want %q %v", c.read, c.bad, got, knock, c.want, c.knock)
		}
	}
}

func TestTheReadIsTheSuspectWhenTheFaultSitsInTheReadItself(t *testing.T) {
	e := &env{cat: catalogtest.Shop()}
	step := func(id, call, response string, refs ...string) *runner.StepRecord {
		st := &runner.StepRecord{ID: id, Call: call, Status: runner.StatusPassed, Response: json.RawMessage(response), BodyRefs: map[string]string{}}
		for i, r := range refs {
			st.BodyRefs[string(rune('a'+i))] = "${" + r + ".product.id_product}"
		}
		return st
	}
	const create, get, add, list = "shop.catalog.v1.ProductService/CreateProduct", "shop.catalog.v1.ProductService/GetProduct",
		"shop.catalog.v1.StockService/AddStock", "shop.catalog.v1.ProductService/ListProducts"
	failing := func(st *runner.StepRecord, path string, want, got any) *runner.StepRecord {
		st.Status = runner.StatusFailed
		st.Expect = append(st.Expect, chain.ExpectResult{Path: path, Rule: "equals", Want: want, Got: got})
		return st
	}
	echoed := func(st *runner.StepRecord) *runner.StepRecord {
		st.Expect = append(st.Expect, chain.ExpectResult{Path: "product.sku", Rule: "equals", Want: "SKU-A", Got: "SKU-A", Passed: true})
		return st
	}
	for _, c := range []struct {
		name  string
		steps []*runner.StepRecord
		read  string
		path  string
		own   string
		write string
	}{
		{"the read answers a field its write returned otherwise", []*runner.StepRecord{
			echoed(step("create", create, `{"product":{"id_product":"p1","sku":"SKU-A"}}`)),
			failing(step("get", get, `{"product":{"id_product":"p1","sku":"sku-a"}}`, "create"), "product.sku", "SKU-A", "sku-a"),
		}, "get", "product.sku", "GetProduct answers product.sku differently from what CreateProduct returned for the same record", ""},
		{"the write's own answer is not known to be unchanged", []*runner.StepRecord{
			step("create", create, `{"product":{"id_product":"p1","sku":"SKU-A"}}`),
			failing(step("get", get, `{"product":{"id_product":"p1","sku":"sku-a"}}`, "create"), "product.sku", "SKU-A", "sku-a"),
		}, "get", "product.sku", "", "create"},
		{"the write carries no such field, so a wrong value after it stays on the write", []*runner.StepRecord{
			echoed(step("create", create, `{"product":{"id_product":"p1","sku":"SKU-A","price_minor":"5"}}`)),
			step("add", add, `{"qty_on_hand":"6"}`, "create"),
			failing(step("get", get, `{"product":{"id_product":"p1","sku":"SKU-A","price_minor":"6"}}`, "create"), "product.price_minor", "5", "6"),
		}, "get", "product.price_minor", "", "add"},
		{"a server error is the read's own", []*runner.StepRecord{
			step("create", create, `{"product":{"id_product":"p1"}}`),
			func() *runner.StepRecord {
				st := step("get", get, ``, "create")
				st.Status, st.HTTPStatus, st.Transport, st.Error = runner.StatusFailed, 500, &runner.TransportError{Code: "internal", Message: "pool exhausted"}, "internal: pool exhausted"
				return st
			}(),
			func() *runner.StepRecord {
				st := step("get_again", get, `{}`, "create")
				st.Status = runner.StatusFailed
				st.Expect = []chain.ExpectResult{{Path: "product.sku", Rule: "unevaluated", Detail: `not evaluated: ${get.product.sku} reads step "get", which did not pass`}}
				return st
			}(),
		}, "get_again", "product.sku", "GetProduct fails on its own (internal: pool exhausted)", ""},
		{"the same items in another order are the read's", []*runner.StepRecord{
			step("create", create, `{"product":{"id_product":"p1"}}`),
			step("create_2", create, `{"product":{"id_product":"p2"}}`),
			failing(step("list", list, `{"products":[{"id_product":"p2"},{"id_product":"p1"}]}`, "create", "create_2"), "products.0.id_product", "p1", "p2"),
		}, "list", "products.0.id_product", "ListProducts answers the same items in another order", ""},
	} {
		rec := &runner.Record{Steps: c.steps}
		b := runAttribution(e, rec).of(c.read, c.path)
		write := ""
		if b.write >= 0 {
			write = rec.Steps[b.write].ID
		}
		if b.own != c.own || write != c.write {
			t.Errorf("%s: got own %q write %q, want own %q write %q", c.name, b.own, write, c.own, c.write)
		}
	}
}

func TestAReadChangingAfterTwoDifferentWritesIsSuspectedItself(t *testing.T) {
	item := func(chainName, write string) *gateChain {
		return &gateChain{name: chainName, items: []gateItem{{Step: "get", Call: "x.v1.S/Get", Path: "thing.state", Want: "a", Got: "b", Suspect: "x.v1.S/" + write, SuspectStep: "w"}}}
	}
	out := captureStdout(t, func() { printGateGroups([]*gateChain{item("one", "Move"), item("two", "Fill")}) })
	if !strings.Contains(out, "suspect the read: Get changes thing.state after 2 different writes (Move, Fill)") {
		t.Errorf("two different writes before the same change of one read point at the read:\n%s", out)
	}
	out = captureStdout(t, func() { printGateGroups([]*gateChain{item("one", "Move"), item("two", "Move")}) })
	if strings.Contains(out, "suspect the read") || !strings.Contains(out, "S/Move: passed itself") {
		t.Errorf("one write before every change stays the suspect:\n%s", out)
	}
}

type recStep struct {
	*runner.StepRecord
}

func shopStep(id, call, response string, refs ...string) recStep {
	st := &runner.StepRecord{ID: id, Call: call, Status: runner.StatusPassed, Response: json.RawMessage(response), BodyRefs: map[string]string{}}
	for i, r := range refs {
		st.BodyRefs[string(rune('a'+i))] = "${" + r + ".x}"
	}
	return recStep{st}
}

func (s recStep) failing(path string, want, got any) recStep {
	s.Status = runner.StatusFailed
	s.Expect = append(s.Expect, chain.ExpectResult{Path: path, Rule: "equals", Want: want, Got: got})
	return s
}

func (s recStep) heldBy(src, path string) recStep {
	s.Status = runner.StatusFailed
	s.Expect = append(s.Expect, chain.ExpectResult{Path: path, Rule: "unevaluated",
		Detail: `not evaluated: ${` + src + `.` + path + `} reads step "` + src + `", which did not pass`})
	return s
}

func shopRecord(steps ...recStep) *runner.Record {
	rec := &runner.Record{}
	for _, s := range steps {
		rec.Steps = append(rec.Steps, s.StepRecord)
	}
	return rec
}

const (
	shopCreate  = "shop.catalog.v1.ProductService/CreateProduct"
	shopGet     = "shop.catalog.v1.ProductService/GetProduct"
	shopList    = "shop.catalog.v1.ProductService/ListProducts"
	shopOrder   = "shop.orders.v1.OrderService/CreateOrder"
	shopConfirm = "shop.orders.v1.OrderService/ConfirmOrder"
	shopCancel  = "shop.orders.v1.OrderService/CancelOrder"
	shopFetch   = "shop.orders.v1.OrderService/FetchOrder"
)

func blameOf(t *testing.T, rec *runner.Record, step, path string) (string, string, string) {
	t.Helper()
	b := runAttribution(&env{cat: catalogtest.Shop()}, rec).of(step, path)
	write := ""
	if b.write >= 0 {
		write = rec.Steps[b.write].ID
	}
	return write, b.own, b.cascade
}

func TestAStepUnevaluatedBehindAFailedWriteIsFiledUnderThatWrite(t *testing.T) {
	rec := shopRecord(
		shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`).failing("product.sku", "A", nil),
		shopStep("add", "shop.catalog.v1.StockService/AddStock", `{"qty_on_hand":"1"}`, "create"),
		shopStep("get_after_add", shopGet, `{"product":{"id_product":"p1"}}`, "create").heldBy("create", "product.sku"),
	)
	write, own, cascade := blameOf(t, rec, "get_after_add", "status")
	if write != "create" || own != "" || cascade != "unevaluated because CreateProduct lost product.sku" {
		t.Errorf("got write %q own %q cascade %q", write, own, cascade)
	}
}

func TestAChangeTheWriteItselfAnsweredIsTheWritesNotTheLaterSteps(t *testing.T) {
	order := `{"order":{"id_order":"o1","total_minor":"7"}}`
	rec := shopRecord(
		shopStep("create_order", shopOrder, order).failing("order.total_minor", "9", "7"),
		shopStep("confirm_order", shopConfirm, order, "create_order").failing("order.total_minor", "9", "7"),
		shopStep("fetch_after_confirm", shopFetch, order, "create_order").failing("order.total_minor", "9", "7"),
		shopStep("cancel_order", shopCancel, order, "create_order").failing("order.total_minor", "9", "7"),
		shopStep("fetch_order_after_cancel_order", shopFetch, order, "create_order").failing("order.total_minor", "9", "7"),
	)
	for _, step := range []string{"confirm_order", "fetch_after_confirm", "cancel_order", "fetch_order_after_cancel_order"} {
		if write, own, _ := blameOf(t, rec, step, "order.total_minor"); write != "create_order" || own != "" {
			t.Errorf("%s: got write %q own %q, want create_order", step, write, own)
		}
	}
}

func TestAWrongLevelAfterAPassingWriteThatDoesNotCarryItStaysOnThatWrite(t *testing.T) {
	product := `{"product":{"id_product":"p1","price_minor":"6"}}`
	order := `{"order":{"id_order":"o1"}}`
	rec := shopRecord(
		shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","price_minor":"5"}}`),
		shopStep("create_order", shopOrder, order, "create_product"),
		shopStep("confirm_order", shopConfirm, order, "create_order"),
		shopStep("get_product_after_confirm_order", shopGet, product, "create_product").failing("product.price_minor", "5", "6"),
		shopStep("cancel_order", shopCancel, order, "create_order"),
		shopStep("get_product_after_cancel_order", shopGet, product, "create_product").failing("product.price_minor", "5", "6"),
	)
	for _, step := range []string{"get_product_after_confirm_order", "get_product_after_cancel_order"} {
		if write, own, _ := blameOf(t, rec, step, "product.price_minor"); write != "confirm_order" || own != "" {
			t.Errorf("%s: got write %q own %q, want confirm_order", step, write, own)
		}
	}
}

func TestAListAnsweringOtherItemsAfterUnchangedWritesIsTheReadsOwn(t *testing.T) {
	rec := shopRecord(
		shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`),
		shopStep("create_2", shopCreate, `{"product":{"id_product":"p2"}}`),
		shopStep("list", shopList, `{"products":[{"id_product":"p9"},{"id_product":"p1"},{"id_product":"p2"}]}`, "create", "create_2").
			failing("products.0.id_product", "p1", "p9"),
	)
	rec.Steps[2].Expect = append(rec.Steps[2].Expect, chain.ExpectResult{Path: "products.2", Rule: "exists", Want: false, Got: true})
	_, own, _ := blameOf(t, rec, "list", "products.0.id_product")
	if own != "ListProducts answers another set of products, and the writes before it answered as before" {
		t.Errorf("got own %q", own)
	}
	rec.Steps[1].Status = runner.StatusFailed
	if _, own, _ := blameOf(t, rec, "list", "products.0.id_product"); strings.Contains(own, "another set") {
		t.Errorf("a write that failed before the list keeps the read from being blamed for its set, got own %q", own)
	}
}

func TestTheGateSummaryHasOneLinePerSuspectRpcAndFoldsUnevaluatedSteps(t *testing.T) {
	var items []gateItem
	for _, p := range []string{"list.0.a", "list.0.b", "list.0.c", "list.0.d", "list"} {
		items = append(items, gateItem{Step: "list", Call: "x.v1.S/List", Path: p, Want: "1", Got: "2", Own: "List answers another set of list"})
	}
	items = append(items,
		gateItem{Step: "list_2", Call: "x.v1.S/List", Path: "list", Want: "1", Got: "2", Own: "List answers the same items in another order"},
		gateItem{Step: "find", Call: "x.v1.S/Find", Path: "n", Want: "1", Got: "2", Own: "Find fails on its own (internal)"},
		gateItem{Step: "make", Call: "x.v1.S/Make", Path: "n", Want: "1", Got: "2"},
		gateItem{Step: "get", Call: "x.v1.S/Get", Path: "status", Want: "passed", Got: "failed", Suspect: "x.v1.S/Make", SuspectStep: "make", Cascade: "unevaluated because Make lost n"},
		gateItem{Step: "get_2", Call: "x.v1.S/Get", Path: "status", Want: "passed", Got: "failed", Suspect: "x.v1.S/Make", SuspectStep: "make", Cascade: "unevaluated because Make lost n"},
	)
	out := captureStdout(t, func() { printGateGroups([]*gateChain{{name: "one", items: items}}) })
	if n := strings.Count(out, "  S/List:"); n != 1 || !strings.Contains(out, "(+1 other reason(s))") {
		t.Errorf("one line for List with its paths merged, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "  S/Find:") || strings.Contains(out, "more\n") {
		t.Errorf("a distinct suspect rpc is never cut:\n%s", out)
	}
	if !strings.Contains(out, "    +2 step(s) in 1 chain(s) unevaluated because Make lost n\n") || strings.Contains(out, "S/Get:") {
		t.Errorf("unevaluated steps fold into one line under the producing write:\n%s", out)
	}
}

func TestTheGateSuspectLineAgreesWithTheSummary(t *testing.T) {
	g := func(name, write string) *gateChain {
		return &gateChain{name: name, failed: true, firstAt: "get thing.state", sent: map[string]string{"get": " sent {}", "w": " sent {\"w\":1}"},
			items: []gateItem{{Step: "get", Call: "x.v1.S/Get", Path: "thing.state", Want: "a", Got: "b", Suspect: "x.v1.S/" + write, SuspectStep: "w"}}}
	}
	chains := []*gateChain{g("one", "Move"), g("two", "Fill")}
	settleGate(chains)
	if line := chains[0].suspectLine(); line != "suspect read get (S/Get) sent {}" {
		t.Errorf("the per-chain line follows the settled attribution, got %q", line)
	}
	chains = []*gateChain{g("one", "Move"), g("two", "Move")}
	settleGate(chains)
	if line := chains[0].suspectLine(); line != "suspect write w (S/Move) sent {\"w\":1}" {
		t.Errorf("got %q", line)
	}
}

func TestTheVerboseGateListsEachChangedPathOnce(t *testing.T) {
	g := &gateChain{name: "one", failed: true, items: []gateItem{
		{Step: "make", Path: "thing.n", Want: "1", Got: "2"},
		{Step: "get", Path: "thing.n", Want: "1", Got: "2"},
		{Step: "get", Path: "thing.n", Want: "1", Got: "2"},
		{Step: "list", Path: "things.3.n", Want: "1", Got: "2"},
		{Step: "later", Path: "status", Want: "passed", Got: "failed", Cascade: "unevaluated because Make lost thing.n"},
	}}
	out := captureStdout(t, g.printChanges)
	for _, want := range []string{
		"    thing.n at 2 step(s) (make, get); e.g. want=1 got=2\n",
		"    things[].n at 1 step(s) (list); e.g. want=1 got=2\n",
		"    1 step(s) unevaluated because Make lost thing.n\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

func TestChangesAfterAWriteThatFailedAtTheTransportAreFiledUnderIt(t *testing.T) {
	add := shopStep("add", "shop.catalog.v1.StockService/AddStock", `{"code":"unavailable"}`, "create")
	add.Status, add.HTTPStatus, add.Transport = runner.StatusError, 503, &runner.TransportError{Code: "unavailable", Message: "busy"}
	order := `{"order":{"id_order":"o1","lines":[{"id_product":"p1","qty":"3"}]}}`
	rec := shopRecord(
		shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`),
		shopStep("create_2", shopCreate, `{"product":{"id_product":"p2"}}`),
		add,
		shopStep("create_order", shopOrder, order, "create", "create_2").failing("order.lines.0.qty", "2", "3"),
		shopStep("confirm_order", shopConfirm, order, "create_order").failing("status.code", "SUCCESS", "REJECTED"),
		shopStep("get", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"0"}}`, "create").failing("product.qty_on_hand", "5", "0"),
		shopStep("get_2", shopGet, `{"product":{"id_product":"p2","qty_on_hand":"0"}}`, "create_2").failing("product.qty_on_hand", "5", "0"),
	)
	for _, c := range []struct{ step, path, write string }{
		{"get", "product.qty_on_hand", "add"},
		{"confirm_order", "status.code", "add"},
		{"get_2", "product.qty_on_hand", "add"},
	} {
		if write, own, cascade := blameOf(t, rec, c.step, c.path); write != c.write || own != "" || cascade != "after AddStock failed on the same record" {
			t.Errorf("%s: got write %q own %q cascade %q", c.step, write, own, cascade)
		}
	}
	if write, _, cascade := blameOf(t, rec, "create_order", "order.lines.0.qty"); write == "add" || cascade != "" {
		t.Errorf("a field the failed write does not answer is not its: got write %q cascade %q", write, cascade)
	}
	other := shopRecord(recStep{rec.Steps[0]}, recStep{rec.Steps[1]}, add, recStep{rec.Steps[6]})
	if write, _, cascade := blameOf(t, other, "get_2", "product.qty_on_hand"); write == "add" || cascade != "" {
		t.Errorf("another record is not the failed write's: got write %q cascade %q", write, cascade)
	}
	after := func(step string) gateItem {
		return gateItem{Step: step, Call: shopGet, Path: "product.qty_on_hand", Want: "5", Got: "0",
			Suspect: "shop.catalog.v1.StockService/AddStock", SuspectStep: "add", Cascade: "after AddStock failed on the same record"}
	}
	chains := []*gateChain{
		{name: "one", items: []gateItem{after("get"), {Step: "get_2", Call: shopGet, Path: "product.qty_on_hand", Want: "5", Got: "0", Suspect: shopCancel, SuspectStep: "cancel"}}},
		{name: "two", items: []gateItem{after("get"), {Step: "get_2", Call: shopGet, Path: "product.qty_on_hand", Want: "5", Got: "0", Suspect: shopConfirm, SuspectStep: "confirm"}}},
	}
	out := captureStdout(t, func() { printGateGroups(chains) })
	if !strings.Contains(out, "    +2 step(s) in 2 chain(s) after AddStock failed on the same record\n") {
		t.Errorf("the steps after the failed write fold into one line under it:\n%s", out)
	}
	if !strings.Contains(out, "suspect the read: GetProduct changes product.qty_on_hand after 2 different writes (CancelOrder, ConfirmOrder)") {
		t.Errorf("the steps after the failed write do not count as writes the read changes after:\n%s", out)
	}
}
