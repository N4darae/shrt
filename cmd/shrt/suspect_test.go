package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
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
		{ID: "get_item_unknown_id", Call: "x.v1.S/GetItem", BodyRefs: map[string]string{"id_item": "${create_item.id}-unknown"}},
	}}
	for _, c := range []struct {
		read, bad, want string
		knock           bool
	}{
		{"get_item", "", "fill_item", false},
		{"get_item", "move_box", "move_box", false},
		{"get_item_after_move_box", "fill_item", "fill_item", false},
		{"get_item_after_move_box", "move_box", "move_box", false},
		{"list_items", "create_box", "create_box", true},
		{"list_items", "", "", false},
		{"fill_item", "", "", false},
		{"get_item_unknown_id", "", "", false},
	} {
		i, knock := suspectWrite(rec, c.read, "", map[string]bool{c.bad: c.bad != ""})
		got := ""
		if i >= 0 {
			got = rec.Steps[i].ID
		}
		if got != c.want || knock != c.knock {
			t.Errorf("%s with %q failing: got %q knock-on %v, want %q %v", c.read, c.bad, got, knock, c.want, c.knock)
		}
	}
}

func TestASuspectWriteIsFoundThroughExportedIDs(t *testing.T) {
	step := func(id, call string, exported string, refs map[string]string) *runner.StepRecord {
		st := &runner.StepRecord{ID: id, Call: "shop.v1.S/" + call, BodyRefs: refs}
		if exported != "" {
			st.Exported = map[string]any{exported: id + "-1"}
		}
		return st
	}
	for _, orderRef := range []string{"${id_order}", "${exports.id_order}"} {
		rec := &runner.Record{Steps: []*runner.StepRecord{
			step("create_product_a", "CreateProduct", "id_a", nil),
			step("add_stock_a", "AddStock", "", map[string]string{"id_product": "${id_a}"}),
			step("create_order", "CreateOrder", "id_order", map[string]string{"lines.0.id_product": "${id_a}"}),
			step("confirm_order", "ConfirmOrder", "", map[string]string{"id_order": orderRef}),
			step("fetch_order", "FetchOrder", "", map[string]string{"id_order": orderRef}),
			step("stock_a_confirmed", "GetProduct", "", map[string]string{"id_product": "${id_a}"}),
		}}
		i, knock := suspectWrite(rec, "stock_a_confirmed", "", map[string]bool{})
		if i < 0 || rec.Steps[i].ID != "confirm_order" || knock {
			t.Errorf("%s: got step %d knock-on %v, want confirm_order", orderRef, i, knock)
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
	const batch = "shop.catalog.v1.StockService/AddStockBatch"
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
		why   string
	}{
		{"one read answers a field its write returned otherwise and no other read settles it", []*runner.StepRecord{
			echoed(step("create", create, `{"product":{"id_product":"p1","sku":"SKU-A"}}`)),
			failing(step("get", get, `{"product":{"id_product":"p1","sku":"sku-a"}}`, "create"), "product.sku", "SKU-A", "sku-a"),
		}, "get", "product.sku", "", "create", "the write or the read: create answered product.sku=SKU-A, get reads sku-a"},
		{"an empty value the read answers is shown", []*runner.StepRecord{
			echoed(step("create", create, `{"product":{"id_product":"p1","sku":"SKU-A"}}`)),
			failing(step("get", get, `{"product":{"id_product":"p1","sku":""}}`, "create"), "product.sku", "SKU-A", ""),
		}, "get", "product.sku", "", "create", `the write or the read: create answered product.sku=SKU-A, get reads ""`},
		{"without a reference the read agreed with, the contradiction stays on the write", []*runner.StepRecord{
			echoed(step("create", create, `{"product":{"id_product":"p1","sku":"SKU-A"}}`)),
			failing(step("get", get, `{"product":{"id_product":"p1","sku":"sku-a"}}`, "create"), "product.sku", "SKU-B", "sku-a"),
		}, "get", "product.sku", "", "create", "create answered product.sku=SKU-A, get reads sku-a"},
		{"a later read after another write does not settle it", []*runner.StepRecord{
			echoed(step("create", create, `{"product":{"id_product":"p1","sku":"SKU-A"}}`)),
			failing(step("get", get, `{"product":{"id_product":"p1","sku":"sku-a"}}`, "create"), "product.sku", "SKU-A", "sku-a"),
			step("add", add, `{"qty_on_hand":"6"}`, "create"),
			step("list", list, `{"products":[{"id_product":"p1","sku":"SKU-A"}]}`),
		}, "get", "product.sku", "", "create", "the write or the read: create answered product.sku=SKU-A, get reads sku-a"},
		{"two read rpcs agree against what the write answered", []*runner.StepRecord{
			echoed(step("create", create, `{"product":{"id_product":"p1","sku":"SKU-A"}}`)),
			failing(step("get", get, `{"product":{"id_product":"p1","sku":"sku-a"}}`, "create"), "product.sku", "SKU-A", "sku-a"),
			step("list", list, `{"products":[{"id_product":"p0","sku":"SKU-0"},{"id_product":"p1","sku":"sku-a"}]}`),
		}, "get", "product.sku", "", "create",
			`CreateProduct answered product.sku SKU-A, but GetProduct, ListProducts read sku-a: it did not store what it answered`},
		{"another read agrees with the write, so the disagreeing read is the suspect", []*runner.StepRecord{
			echoed(step("create", create, `{"product":{"id_product":"p1","sku":"SKU-A"}}`)),
			failing(step("get", get, `{"product":{"id_product":"p1","sku":"sku-a"}}`, "create"), "product.sku", "SKU-A", "sku-a"),
			step("list", list, `{"products":[{"id_product":"p1","sku":"SKU-A"}]}`),
		}, "get", "product.sku", "GetProduct answers product.sku differently from what CreateProduct returned for the same record", "", ""},
		{"the write's own answer is not known to be unchanged", []*runner.StepRecord{
			step("create", create, `{"product":{"id_product":"p1","sku":"SKU-A"}}`),
			failing(step("get", get, `{"product":{"id_product":"p1","sku":"sku-a"}}`, "create"), "product.sku", "SKU-A", "sku-a"),
		}, "get", "product.sku", "", "create", ""},
		{"the write carries no such field, so a wrong value after it stays on the write", []*runner.StepRecord{
			echoed(step("create", create, `{"product":{"id_product":"p1","sku":"SKU-A","price_minor":"5"}}`)),
			step("add", add, `{"qty_on_hand":"6"}`, "create"),
			failing(step("get", get, `{"product":{"id_product":"p1","sku":"SKU-A","price_minor":"6"}}`, "create"), "product.price_minor", "5", "6"),
		}, "get", "product.price_minor", "", "add", ""},
		{"a list item the write answered is shown with its index", []*runner.StepRecord{
			step("create", create, `{"product":{"id_product":"p1"}}`),
			func() *runner.StepRecord {
				st := step("batch", batch, `{"results":[{"id_product":"p1","qty_on_hand":"0"},{"id_product":"p1","qty_on_hand":"12"}]}`, "create")
				st.Expect = []chain.ExpectResult{{Path: "results.1.qty_on_hand", Rule: "equals", Want: "12", Got: "12", Passed: true}}
				return st
			}(),
			failing(step("get", get, `{"product":{"id_product":"p1","qty_on_hand":"6"}}`, "create"), "product.qty_on_hand", "12", "6"),
		}, "get", "product.qty_on_hand", "", "batch", "the write or the read: batch answered results.1.qty_on_hand=12, get reads 6"},
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
		}, "get_again", "product.sku", "GetProduct fails on its own (internal: pool exhausted)", "", ""},
		{"the same items in another order are the read's", []*runner.StepRecord{
			step("create", create, `{"product":{"id_product":"p1"}}`),
			step("create_2", create, `{"product":{"id_product":"p2"}}`),
			failing(step("list", list, `{"products":[{"id_product":"p2"},{"id_product":"p1"}]}`, "create", "create_2"), "products.0.id_product", "p1", "p2"),
		}, "list", "products.0.id_product", "ListProducts answers the same items in another order", "", ""},
	} {
		rec := &runner.Record{Steps: c.steps}
		b := runAttribution(e, rec).of(c.read, c.path)
		write := ""
		if b.write >= 0 {
			write = rec.Steps[b.write].ID
		}
		if b.own != c.own || write != c.write || b.why != c.why {
			t.Errorf("%s: got own %q write %q why %q, want own %q write %q why %q", c.name, b.own, write, b.why, c.own, c.write, c.why)
		}
	}
}

func TestTheGateFilesAWriteTheReadsAgreeAgainstUnderTheWriteWithTheReason(t *testing.T) {
	why := `Confirm answered thing.state DONE, but Get, List read OPEN: it did not store what it answered`
	item := func(chainName, write string) *gateChain {
		return &gateChain{name: chainName, items: []gateItem{{Step: "get", Call: "x.v1.S/Get", Path: "thing.state", Want: "DONE", Got: "OPEN",
			Suspect: "x.v1.S/" + write, SuspectStep: "w", Why: why, Firm: true}}}
	}
	out := captureStdout(t, func() { printGateGroups([]*gateChain{item("one", "Confirm"), item("two", "Fill")}) })
	if !strings.Contains(out, "S/Confirm: suspect the write: "+why+"; e.g. one w") || strings.Contains(out, "suspect the read") {
		t.Errorf("reads agreeing against the write keep the write the suspect, with the reason:\n%s", out)
	}
}

func TestAReadContradictingTwoDifferentWritesIsSuspectedItself(t *testing.T) {
	item := func(chainName, write, why string) *gateChain {
		return &gateChain{name: chainName, items: []gateItem{{Step: "get", Call: "x.v1.S/Get", Path: "thing.state", Want: "a", Got: "b", Suspect: "x.v1.S/" + write, SuspectStep: "w", Why: why}}}
	}
	out := captureStdout(t, func() {
		printGateGroups([]*gateChain{item("one", "Move", "w answered thing.state=a, get reads b"), item("two", "Fill", "w answered thing.state=a, get reads b")})
	})
	if !strings.Contains(out, "suspect the read: Get reads thing.state unlike what 2 different writes answered (Move, Fill)") {
		t.Errorf("a read disagreeing with what two different writes answered is the suspect:\n%s", out)
	}
	for _, chains := range [][]*gateChain{
		{item("one", "Move", "w answered thing.state=a, get reads b"), item("two", "Move", "w answered thing.state=a, get reads b")},
		{item("one", "Move", ""), item("two", "Fill", "")},
		{item("one", "Move", "w answered thing.state=a, get reads b"), item("two", "Fill", "")},
	} {
		out = captureStdout(t, func() { printGateGroups(chains) })
		if strings.Contains(out, "suspect the read") || !strings.Contains(out, "  S/Move: ") {
			t.Errorf("each chain's own lineage keeps its write the suspect:\n%s", out)
		}
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
			items: []gateItem{{Step: "get", Call: "x.v1.S/Get", Path: "thing.state", Want: "a", Got: "b", Suspect: "x.v1.S/" + write, SuspectStep: "w",
				Why: "w answered thing.state=a, get reads b"}}}
	}
	chains := []*gateChain{g("one", "Move"), g("two", "Fill")}
	settleGate(chains)
	if line := chains[0].suspectLine(); line != "suspect read get (S/Get) sent {}" {
		t.Errorf("the per-chain line follows the settled attribution, got %q", line)
	}
	chains = []*gateChain{g("one", "Move"), g("two", "Move")}
	settleGate(chains)
	if line := chains[0].suspectLine(); line != "suspect write w (S/Move) sent {\"w\":1}; w answered thing.state=a, get reads b" {
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
		{Step: "put", Path: "(error)", Got: "unavailable: busy"},
		{Step: "put", Path: "code", Want: "<none>", Got: "unavailable"},
		{Step: "put_2", Path: "(error)", Got: "unavailable: busy"},
		{Step: "put_2", Path: "code", Want: "<none>", Got: "unavailable"},
	}}
	out := captureStdout(t, g.printChanges)
	for _, want := range []string{
		"    thing.n at 2 step(s) (make, get); e.g. want=1 got=2\n",
		"    things[].n at 1 step(s) (list); e.g. want=1 got=2\n",
		"    1 step(s) unevaluated because Make lost thing.n\n",
		"    (error), code at 2 step(s) (put, put_2); e.g. (error) unavailable: busy\n",
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
		{name: "one", items: []gateItem{after("get"), {Step: "get_2", Call: shopGet, Path: "product.qty_on_hand", Want: "5", Got: "0", Suspect: shopCancel, SuspectStep: "cancel", Why: "cancel answered 5"}}},
		{name: "two", items: []gateItem{after("get"), {Step: "get_2", Call: shopGet, Path: "product.qty_on_hand", Want: "5", Got: "0", Suspect: shopConfirm, SuspectStep: "confirm", Why: "confirm answered 5"}}},
	}
	out := captureStdout(t, func() { printGateGroups(chains) })
	if !strings.Contains(out, "    +2 step(s) in 2 chain(s) after AddStock failed on the same record\n") {
		t.Errorf("the steps after the failed write fold into one line under it:\n%s", out)
	}
	if !strings.Contains(out, "suspect the read: GetProduct reads product.qty_on_hand unlike what 2 different writes answered (CancelOrder, ConfirmOrder)") {
		t.Errorf("the steps after the failed write do not count as writes the read changes after:\n%s", out)
	}
}

func TestAWriteFailingOnAnotherRecordIsNotBlamedOnAnEarlierWriteOfTheSameRpc(t *testing.T) {
	const add = "shop.catalog.v1.StockService/AddStock"
	rec := shopRecord(
		shopStep("create_a", shopCreate, `{"product":{"id_product":"p1"}}`),
		shopStep("create_b", shopCreate, `{"product":{"id_product":"p2"}}`),
		shopStep("add_zero", add, `{"qty_on_hand":"1"}`, "create_a").failing("qty_on_hand", "0", "1"),
		shopStep("add_as_other", add, `{"qty_on_hand":"10"}`, "create_b").failing("qty_on_hand", "0", "10"),
		shopStep("add_more_to_a", add, `{"qty_on_hand":"11"}`, "create_a").failing("qty_on_hand", "10", "11"),
	)
	if write, own, _ := blameOf(t, rec, "add_as_other", "qty_on_hand"); write != "" || own != "" {
		t.Errorf("a write on another record failed on its own; got write %q own %q", write, own)
	}
	if write, _, _ := blameOf(t, rec, "add_more_to_a", "qty_on_hand"); write != "add_zero" {
		t.Errorf("a write on the same record still echoes the earlier write; got %q", write)
	}
}

func TestARefusedRepeatOrAReplayOfAnEarlierWriteIsNeverTheSuspect(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	order := `{"order":{"id_order":"o1"},` + shopOK + `}`
	create := shopStep("create_order", shopOrder, order, "create_product")
	replay := shopStep("replay_order", shopOrder, order, "create_product")
	replay.BodyRefs["idempotency_key"] = "${steps.create_order.request.idempotency_key}"
	rec := shopRecord(
		shopStep("create_product", shopCreate, `{"product":{"id_product":"p1"},`+shopOK+`}`),
		create,
		shopStep("confirm_order", shopConfirm, order, "create_order"),
		replay,
		shopStep("confirm_again", shopConfirm, `{"status":{"code":"REJECTED","details":[{"app_code":1303}]}}`, "create_order"),
		shopStep("get_product", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"14"},`+shopOK+`}`, "create_product").failing("product.qty_on_hand", "15", "14"),
	)
	candidates := func() []int {
		pos := map[string]int{}
		for i, st := range rec.Steps {
			pos[st.ID] = i
		}
		return entityWrites(rec, len(rec.Steps)-1, "", map[string]bool{}, pos)
	}
	if got := candidates(); !slices.Equal(got, []int{2, 1, 0}) {
		t.Errorf("got steps %v, want the writes before it without the repeats", got)
	}
	rec.Steps[2] = shopStep("confirm_order", shopConfirm, `{"status":{"code":"REJECTED","details":[{"app_code":1305}]}}`, "create_order").StepRecord
	rec.Steps = append(rec.Steps[:4], rec.Steps[5])
	if got := candidates(); !slices.Equal(got, []int{2, 1, 0}) {
		t.Errorf("a first refused write is still one the read observes, got %v", got)
	}
	e := &env{cat: catalogtest.Shop()}
	if b := runAttribution(e, rec).of("get_product", "product.qty_on_hand"); b.write != 2 || b.why != "" {
		t.Errorf("without a reference the nearest write is named, got %+v", b)
	}
	moved := []diff.Change{{Step: "get_product", Path: "product.qty_on_hand", Kind: diff.KindChanged, Want: "15", Got: "14"}}
	if b := changesAttribution(e, rec, moved).of("get_product", "product.qty_on_hand"); b.write >= 0 || b.why != "the write or the read: create_order (CreateOrder), or earlier create_product, answered as before" {
		t.Errorf("against a reference, a write refused as before is no candidate and the nearest other is named first, got %+v", b)
	}
}

func TestAListItemIsTheWritesRecordOnlyWhenItsOwnIDMatches(t *testing.T) {
	var cancel, list, one any
	_ = json.Unmarshal([]byte(`{"order":{"id_order":"o2","id_customer":"c1","status":"CANCELLED"}}`), &cancel)
	_ = json.Unmarshal([]byte(`{"orders":[{"id_order":"o3","id_customer":"c1","status":"CONFIRMED"},{"id_order":"o2","id_customer":"c1","status":"CANCELLED"}]}`), &list)
	_ = json.Unmarshal([]byte(`{"orders":[{"id_order":"o3","id_customer":"c1","status":"CONFIRMED"}]}`), &one)
	for _, c := range []struct {
		read any
		path string
		same bool
	}{
		{list, "orders.0.status", false},
		{list, "orders.1.status", true},
		{one, "orders.0.status", false},
	} {
		if got := sameEntity(c.read, c.path, cancel, "order.status"); got != c.same {
			t.Errorf("%s: same record %v, want %v", c.path, got, c.same)
		}
	}
}

func TestAWriteAnsweringOtherThanALaterReadOfTheRecordShowsBothValues(t *testing.T) {
	fetched := shopStep("fetch_order", shopFetch, `{"order":{"id_order":"o1","total_minor":"9"}}`, "create_order")
	fetched.Expect = []chain.ExpectResult{{Path: "order.total_minor", Rule: "equals", Want: "9", Got: "9", Passed: true}}
	rec := shopRecord(
		shopStep("create_order", shopOrder, `{"order":{"id_order":"o1","total_minor":"9"}}`),
		shopStep("confirm_order", shopConfirm, `{"order":{"id_order":"o1","total_minor":"7"}}`, "create_order").failing("order.total_minor", "9", "7"),
		fetched,
		shopStep("cancel_order", shopCancel, `{"order":{"id_order":"o1","total_minor":"0"}}`, "create_order"),
	)
	b := runAttribution(&env{cat: catalogtest.Shop()}, rec).of("confirm_order", "order.total_minor")
	want := "ConfirmOrder answered order.total_minor 7, but FetchOrder read 9: it answered other than it stored"
	if b.write >= 0 || b.own != "" || b.why != want {
		t.Errorf("got write %d own %q why %q, want the write's own failure with %q", b.write, b.own, b.why, want)
	}
	fetched.Response = []byte(`{"order":{"id_order":"o1","total_minor":""}}`)
	if b := runAttribution(&env{cat: catalogtest.Shop()}, rec).of("confirm_order", "order.total_minor"); b.why != `ConfirmOrder answered order.total_minor 7, but FetchOrder read "": it answered other than it stored` {
		t.Errorf("an empty read is shown, got %q", b.why)
	}
	rec.Steps[2], rec.Steps[3] = rec.Steps[3], rec.Steps[2]
	if b := runAttribution(&env{cat: catalogtest.Shop()}, rec).of("confirm_order", "order.total_minor"); b.why != "" {
		t.Errorf("a read after a later write of the record says nothing about what the first write stored: %q", b.why)
	}
}

func TestAnEarlierWriteThatChangedTheSameFieldOnTheRecordIsTheSuspectOfWhatFollows(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	order := `{"order":{"id_order":"o1","status":"ORDER_STATUS_PENDING"},` + shopOK + `}`
	refused := `{"status":{"code":"REJECTED","details":[{"app_code":1305}]}}`
	rec := shopRecord(
		shopStep("create_product", shopCreate, `{"product":{"id_product":"p1"},`+shopOK+`}`),
		shopStep("add_stock", "shop.catalog.v1.StockService/AddStock", `{"qty_on_hand":"9",`+shopOK+`}`, "create_product").failing("qty_on_hand", "10", "9"),
		shopStep("create_order", shopOrder, order, "create_product"),
		shopStep("confirm_order", shopConfirm, order, "create_order"),
		shopStep("get_product_after_confirm_order", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"7"},`+shopOK+`}`, "create_product").failing("product.qty_on_hand", "8", "7"),
		shopStep("confirm_exact", shopConfirm, refused, "create_order").failing("status.code", "SUCCESS", "REJECTED"),
		shopStep("fetch_order_after_confirm_exact", shopFetch, order, "create_order").failing("order.status", "ORDER_STATUS_CONFIRMED", "ORDER_STATUS_PENDING"),
	)
	for _, c := range []struct{ step, path string }{
		{"get_product_after_confirm_order", "product.qty_on_hand"},
		{"confirm_exact", "status.code"},
		{"fetch_order_after_confirm_exact", "order.status"},
	} {
		if write, own, _ := blameOf(t, rec, c.step, c.path); write != "add_stock" || own != "" {
			t.Errorf("%s: got write %q own %q, want add_stock, whose own qty_on_hand changed first", c.step, write, own)
		}
	}
}

func TestAWriteAnsweringItsListInAnotherOrderThanTheReadSaysSo(t *testing.T) {
	stored := `{"order":{"id_order":"o1","lines":[{"id_product":"a","qty":"2"},{"id_product":"b","qty":"3"}]}}`
	reversed := `{"order":{"id_order":"o1","lines":[{"id_product":"b","qty":"3"},{"id_product":"a","qty":"2"}]}}`
	fetched := shopStep("fetch_order", shopFetch, stored, "create_order")
	fetched.Expect = []chain.ExpectResult{{Path: "order.lines.0.qty", Rule: "equals", Want: "2", Got: "2", Passed: true}}
	rec := shopRecord(
		shopStep("create_order", shopOrder, reversed).failing("order.lines.0.qty", "2", "3"),
		fetched,
	)
	b := runAttribution(&env{cat: catalogtest.Shop()}, rec).of("create_order", "order.lines.0.qty")
	want := "CreateOrder answered order.lines in another order than FetchOrder read: it answered other than it stored"
	if b.why != want {
		t.Errorf("why %q, want %q", b.why, want)
	}
	fetched.Expect[0].Passed = false
	if b := runAttribution(&env{cat: catalogtest.Shop()}, rec).of("create_order", "order.lines.0.qty"); b.why != "" {
		t.Errorf("a read that moved too says nothing about what the write stored, got %q", b.why)
	}
}

func TestTheGateNamesTheWriteOrTheReadWithTheRootWriteAsExample(t *testing.T) {
	why := "the write or the read: w answered results.2.qty_on_hand=12, get reads 6"
	g := &gateChain{name: "one", items: []gateItem{
		{Step: "later", Call: "x.v1.S/Batch", Path: "results.1.qty_on_hand", Want: "18", Got: "12"},
		{Step: "get", Call: "x.v1.S/Get", Path: "product.qty_on_hand", Want: "12", Got: "6", Suspect: "x.v1.S/Batch", SuspectStep: "w", Why: why},
	}}
	out := captureStdout(t, func() { printGateGroups([]*gateChain{g}) })
	if !strings.Contains(out, "; "+why+"; e.g. one w\n") {
		t.Errorf("the group shows both values and the root write:\n%s", out)
	}
	g.items = g.items[1:]
	out = captureStdout(t, func() { printGateGroups([]*gateChain{g}) })
	if !strings.Contains(out, "  S/Batch or Get: suspect "+why+"; e.g. one w\n") {
		t.Errorf("got:\n%s", out)
	}
}

func TestTheWriteOrTheReadIsNamedByBothRpcsInEveryGateLine(t *testing.T) {
	why := "the write or the read: create answered customer.name=Ada, get reads ada@x"
	g := func(name string) *gateChain {
		return &gateChain{name: name, failed: true, firstAt: "get customer.name", sent: map[string]string{"create": ` sent {"name":"Ada"}`},
			items: []gateItem{{Step: "get", Call: "x.v1.S/GetCustomer", Path: "customer.name", Want: "Ada", Got: "ada@x",
				Suspect: "x.v1.S/CreateCustomer", SuspectStep: "create", Why: why}}}
	}
	chains := []*gateChain{g("one"), g("two")}
	settleGate(chains)
	headlineGate(chains)
	if line := chains[0].suspectLine(); line != `suspect create (S/CreateCustomer or GetCustomer) sent {"name":"Ada"}; `+why {
		t.Errorf("got %q", line)
	}
	if want := "1 step(s) from CreateCustomer or GetCustomer, reported above"; chains[1].first != want {
		t.Errorf("got %q, want %q", chains[1].first, want)
	}
	if out := captureStdout(t, func() { printGateGroups(chains) }); !strings.Contains(out, "  S/CreateCustomer or GetCustomer: suspect "+why) {
		t.Errorf("got:\n%s", out)
	}
}

func TestAGroupWhoseRpcFailedItselfRanksAboveOnesThatPassedThemselves(t *testing.T) {
	var chains []*gateChain
	for _, name := range []string{"one", "two", "three"} {
		chains = append(chains, &gateChain{name: name, items: []gateItem{{Step: "get", Call: "x.v1.S/Get", Path: "n", Want: "1", Got: "2", Suspect: "x.v1.S/Make", SuspectStep: "make"}}})
	}
	chains = append(chains, &gateChain{name: "four", items: []gateItem{{Step: "add", Call: "x.v1.S/Add", Path: "n", Want: "1", Got: "2"}}},
		&gateChain{name: "five", items: []gateItem{{Step: "read", Call: "x.v1.S/Read", Path: "n", Want: "1", Got: "2", Why: eitherWhy + "add (Add), or earlier make, answered as before"}}})
	out := captureStdout(t, func() { printGateGroups(chains) })
	add, read, made := strings.Index(out, "  S/Add:"), strings.Index(out, "  S/Read: 1 step(s) in 1 chain(s), paths n; the write or the read: add (Add), or earlier make, answered as before;"), strings.Index(out, "  S/Make: passed itself")
	if add < 0 || read < 0 || made < add || made < read {
		t.Errorf("groups that failed themselves come first:\n%s", out)
	}
}

func TestAnEnvelopeIsNeverAStoredFieldAWriteAndARecordReadCanDisagreeOn(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	get := shopStep("get_product", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"0"},`+shopOK+`}`, "create_product")
	get.Expect = []chain.ExpectResult{{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true}}
	rec := shopRecord(
		shopStep("create_product", shopCreate, `{"product":{"id_product":"p1"},`+shopOK+`}`),
		shopStep("batch", "shop.catalog.v1.StockService/AddStockBatch", `{"status":{"code":"REJECTED"},"results":[{"id_product":"p1","status":{"code":"REJECTED"}}]}`, "create_product").failing("status.code", "SUCCESS", "REJECTED"),
		get,
	)
	a := runAttribution(&env{cat: catalogtest.Shop()}, rec)
	for _, path := range []string{"status.code", "results.0.status.code"} {
		if b := a.of("batch", path); b.why != "" {
			t.Errorf("%s: got %q", path, b.why)
		}
	}
}

func TestAStepHeldBackByAReadThatEchoesTheWriteNamesTheWrite(t *testing.T) {
	order := `{"order":{"id_order":"o1","total_minor":"7"}}`
	rec := shopRecord(
		shopStep("create_order", shopOrder, order).failing("order.total_minor", "9", "7"),
		shopStep("fetch_before", shopFetch, order, "create_order").failing("order.total_minor", "9", "7"),
		shopStep("fetch_after", shopFetch, order, "create_order").heldBy("fetch_before", "order.total_minor"),
	)
	write, own, cascade := blameOf(t, rec, "fetch_after", "order.total_minor")
	if write != "create_order" || own != "" || cascade != "unevaluated because CreateOrder changed order.total_minor" {
		t.Errorf("got write %q own %q cascade %q", write, own, cascade)
	}
}
