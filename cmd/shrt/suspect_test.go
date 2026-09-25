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
