package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func confirmRecord(product string, stock, after string, between ...recStep) *runner.Record {
	steps := []recStep{
		shopStep("create", shopCreate, `{"product":{"id_product":"`+product+`"},`+shopOK+`}`),
		shopStep("add_stock", shopAdd, `{"qty_on_hand":"`+stock+`",`+shopOK+`}`, "create").with(func(st *runner.StepRecord) {
			st.Request = json.RawMessage(`{"id_product":"` + product + `","qty":"` + stock + `"}`)
		}),
		shopStep("order", shopOrder, `{"order":{"id_order":"o1","lines":[{"id_product":"`+product+`","qty":"2"}]},`+shopOK+`}`, "create"),
		shopStep("confirm", shopConfirm, `{"order":{"id_order":"o1"},`+shopOK+`}`, "order").as("clerk"),
	}
	steps = append(steps, between...)
	return shopRecord(append(steps, shopStep("get", shopGet, `{"product":{"id_product":"`+product+`","qty_on_hand":"`+after+`"},`+shopOK+`}`, "create"))...)
}

func TestARowSaysHowFarAWriteMovedAFieldAgainstTheApprovedRun(t *testing.T) {
	read := gateItem{Step: "get", Path: "product.qty_on_hand", Reason: reason{Kind: reasonWrite, Step: "confirm", RPC: shopConfirm}}
	spot := confirmRecord("p9", "10", "8").Steps
	cancel := shopStep("cancel", shopCancel, `{"order":{"id_order":"o1"},`+shopOK+`}`, "order")
	e := effectsEnv(t)
	for _, c := range []struct {
		name               string
		e                  *env
		rec                *runner.Record
		effect, times, why string
	}{
		{"the confirm took twice the approved fall", e, confirmRecord("p1", "10", "6"), "fell 4 from 10 to 6 where the approved run fell 2 from 10 to 8", "2x", ""},
		{"half the approved fall is a simple fraction", e, confirmRecord("p1", "10", "9"), "fell 1 from 10 to 9 where the approved run fell 2 from 10 to 8", "1/2x", ""},
		{"the same fall from another start has no ratio", e, confirmRecord("p1", "9", "7"), "fell 2 from 9 to 7 where the approved run fell 2 from 10 to 8", "", ""},
		{"a write that may move the field between them says nothing", e, confirmRecord("p1", "10", "6", cancel), "", "", "cancel acts on that record between"},
		{"without contracts the confirm is no known counter, so nothing", &env{}, confirmRecord("p1", "10", "6"), "", "", notMeasured},
	} {
		effect, times, why := effectOf(runAttribution(c.e, c.rec), spot, read)
		if effect != c.effect || times != c.times || why != c.why {
			t.Errorf("%s: got %q %q %q, want %q %q %q", c.name, effect, times, why, c.effect, c.times, c.why)
		}
	}
	noStock := confirmRecord("p1", "10", "6")
	noStock.Steps = append(noStock.Steps[:1], noStock.Steps[2:]...)
	if effect, _, why := effectOf(runAttribution(e, noStock), spot, read); effect != "" || why != noEarlier {
		t.Errorf("no earlier read of the product, so no effect: %q %q", effect, why)
	}
}

func TestAnEffectIsMeasuredOnlyAcrossACounterWriteFromThatRecordsOwnEarlierValue(t *testing.T) {
	e := effectsEnv(t)
	order := func(id, total string) string {
		return `{"order":{"id_order":"` + id + `","id_customer":"c1","total_minor":"` + total + `"},` + shopOK + `}`
	}
	created := func(total string) *runner.Record {
		return shopRecord(
			shopStep("create_order", shopOrder, order("o1", "2500")),
			shopStep("create_order_3_lines", shopOrder, order("o2", total)),
			shopStep("fetch_order_after_create_order_3_lines", shopFetch, order("o2", total), "create_order_3_lines"),
		)
	}
	read := gateItem{Step: "fetch_order_after_create_order_3_lines", Path: "order.total_minor", Reason: reason{Kind: reasonWrite, Step: "create_order_3_lines", RPC: shopOrder}}
	if effect, times, why := effectOf(runAttribution(e, created("21595")), created("58630").Steps, read); effect != "" || times != "" || why != notMeasured {
		t.Errorf("the write that created the order declares no increase or decrease of its total, so nothing is measured from another order's total: %q %q %q", effect, times, why)
	}
	shelved := func(rec *runner.Record) *runner.Record {
		for _, st := range rec.Steps {
			st.Response = json.RawMessage(strings.Replace(string(st.Response), `"id_product":"p1"`, `"id_product":"p1","id_shelf":"s1"`, 1))
		}
		return rec
	}
	other := shelved(confirmRecord("p1", "10", "6"))
	neighbour := shopStep("get_neighbour", shopGet, `{"product":{"id_product":"p2","id_shelf":"s1","qty_on_hand":"12"},`+shopOK+`}`)
	other.Steps = slices.Insert(other.Steps, 2, neighbour.StepRecord)
	confirm := gateItem{Step: "get", Path: "product.qty_on_hand", Reason: reason{Kind: reasonWrite, Step: "confirm", RPC: shopConfirm}}
	effect, times, _ := effectOf(runAttribution(e, other), shelved(confirmRecord("p1", "10", "8")).Steps, confirm)
	if effect != "fell 4 from 10 to 6 where the approved run fell 2 from 10 to 8" || times != "2x" {
		t.Errorf("another product on the same shelf is no earlier value of this one, the stock added to this one is: %q %q", effect, times)
	}
}

func guardRecord(lines, after string, confirm reason) (*runner.Record, gateItem) {
	return shopRecord(
		shopStep("create_product_b", shopCreate, `{"product":{"id_product":"p2","qty_on_hand":"0"},`+shopOK+`}`),
		shopStep("stock_batch", shopBatch, `{"results":[`+lines+`],`+shopOK+`}`, "create_product_b"),
		shopStep("order_fits", shopOrder, `{"order":{"id_order":"o1","lines":[{"id_product":"p2","qty":"1"}]},`+shopOK+`}`, "create_product_b"),
		shopStep("confirm_fits", shopConfirm, `{"order":{"id_order":"o1"},`+shopOK+`}`, "order_fits"),
		shopStep("stock_b_after_fits", shopGet, `{"product":{"id_product":"p2","qty_on_hand":"`+after+`"},`+shopOK+`}`, "create_product_b"),
	), gateItem{Step: "stock_b_after_fits", Path: "product.qty_on_hand", Reason: confirm}
}

func TestTheEarlierValueIsNeverARefusedLineAndIsTheLastAppliedLineOfABatch(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	chain.SetItemEnvelope("results[].status.code")
	defer chain.SetEnvelope("", "")
	defer chain.SetItemEnvelope("")
	const refused, applied = `{"id_product":"p2","qty_on_hand":"0","status":{"code":"REJECTED"}}`, `{"id_product":"p2","qty_on_hand":"1",` + shopOK + `}`
	both := reason{Kind: reasonUnclear, Step: "stock_batch", RPC: shopBatch, Or: []reason{{Step: "stock_batch", RPC: shopBatch}, {Step: "confirm_fits", RPC: shopConfirm}}}
	confirm := reason{Kind: reasonWrite, Step: "confirm_fits", RPC: shopConfirm}
	approved, _ := guardRecord(refused+","+applied, "0", both)
	e := effectsEnv(t)
	for _, c := range []struct {
		name, lines, after string
		r                  reason
		effect, times, why string
	}{
		{"the batch stored only its refused first line and the confirm took 1 as approved: the note spans both suspects, so it says nothing", refused + "," + applied, "-1", both, "", "", notMeasured},
		{"the confirm alone took 2: measured from the batch's last applied line, 1", refused + "," + applied, "-1", confirm, "fell 2 from 1 to -1 where the approved run fell 1 from 1 to 0", "2x", ""},
		{"a batch whose only line for the record was refused is no earlier value, and it may move the field", refused, "-2", confirm, "", "", "stock_batch acts on that record between"},
	} {
		rec, read := guardRecord(c.lines, c.after, c.r)
		effect, times, why := effectOf(runAttribution(e, rec), approved.Steps, read)
		if effect != c.effect || times != c.times || why != c.why {
			t.Errorf("%s: got %q %q %q, want %q %q %q", c.name, effect, times, why, c.effect, c.times, c.why)
		}
	}
	rec, read := guardRecord(refused+","+applied, "-1", confirm)
	rec.Steps[4].BodyRefs = map[string]string{"a": "${order_fits.x}"}
	if effect, _, why := effectOf(runAttribution(e, rec), approved.Steps, read); effect != "" || why != notMeasured {
		t.Errorf("a read the suspects were weighed for without the batch takes no earlier value from the batch's answer, which may not be what it stored: %q %q", effect, why)
	}
}

func TestARatioIsAWholeMultipleOrASimpleFraction(t *testing.T) {
	for _, c := range []struct {
		d, approved float64
		want        string
	}{
		{-4, -2, "2x"}, {3, 9, "1/3x"}, {6, 4, "3/2x"}, {5, 11, ""}, {4, -2, ""}, {2, 2, ""}, {0, 3, ""}, {3, 0, ""},
	} {
		if got := times(c.d, c.approved); got != c.want {
			t.Errorf("times(%v, %v) = %q, want %q", c.d, c.approved, got, c.want)
		}
	}
}

func TestARowClaimsARatioOnEveryFailingStepOnlyWhenEachHasIt(t *testing.T) {
	at := func(step, times string) gateRef {
		it := gateItem{Step: step, Path: "product.qty_on_hand", Times: times}
		switch times {
		case "-", "list":
			it.Times, it.Unmeasured = "", map[string]string{"list": "read in a list of several records", "-": notMeasured}[times]
		default:
			it.Effect = "fell 4 from 10 to 6 where the approved run fell 2 from 10 to 8"
		}
		return gateRef{chain: "c", it: it}
	}
	status := gateRef{chain: "c", it: gateItem{Step: "confirm", Path: "order.status"}}
	for _, c := range []struct {
		name string
		refs []gateRef
		want string
	}{
		{"all 2x", []gateRef{at("a", "2x"), at("b", "2x")}, " on every failing step"},
		{"a step of another field is not one of them", []gateRef{at("a", "2x"), at("b", "2x"), status}, " on every failing step"},
		{"one not measured", []gateRef{at("a", "2x"), at("b", "2x"), at("c", "-")}, " on 2 of 3 failing steps; c not measured"},
		{"the first unmeasured step says why, the rest are counted", []gateRef{at("a", "2x"), at("b", "2x"), at("list_prefix", "list"), at("d", "-")},
			" on 2 of 4 failing steps; list_prefix not measured (read in a list of several records) (+1 more)"},
		{"a write between is named as acting on the record, not as a second suspect", []gateRef{at("a", "2x"), at("b", "2x"), {chain: "c", it: gateItem{Step: "list_prefix", Path: "product.qty_on_hand", Unmeasured: "cancel_two acts on that record between"}}},
			" on 2 of 3 failing steps; list_prefix not measured (cancel_two acts on that record between)"},
		{"one other ratio", []gateRef{at("a", "2x"), at("b", "3x")}, ""},
	} {
		gr := &gateGroup{example: c.refs[0].it, refs: c.refs}
		if got, want := gr.effectNote(), "; qty_on_hand fell 4 from 10 to 6 where the approved run fell 2 from 10 to 8: 2x"+c.want; got != want {
			t.Errorf("%s: got %q, want %q", c.name, got, want)
		}
	}
}

func TestAKeptRedLineSaysWhenThePinWasMade(t *testing.T) {
	c := &chain.Chain{Description: "Slice of x.\nVERIFIED by 'shrt chain slice -verify': reproduced on 2026-09-28: slice run a gave step s the verdict it had in source run b."}
	if got := pinnedOn(c); got != "; pinned 2026-09-28" {
		t.Errorf("got %q", got)
	}
	if got := pinnedOn(&chain.Chain{}); got != "" {
		t.Errorf("a chain pinned by hand names no date: %q", got)
	}
}

func TestATransportErrorsCauseIsOneShortClause(t *testing.T) {
	st := &runner.StepRecord{ID: "create", Call: "shop.customers.v1.CustomerService/CreateCustomer", Status: runner.StatusFailed,
		Transport: &runner.TransportError{Code: "internal", Message: "marshal message: proto: field shop.customers.v1.Customer.name contains invalid UTF-8"}}
	if got := transportCause(st); got != "field Customer.name contains invalid UTF-8" {
		t.Errorf("got %q", got)
	}
	it := runAttribution(nil, &runner.Record{Steps: []*runner.StepRecord{st}}).item(gateItem{Step: "create", Call: st.Call, Path: "transport.code", Want: "ok", Got: "internal"})
	if it.Got != "internal: field Customer.name contains invalid UTF-8" {
		t.Errorf("the gate line carries the cause beside the code: %q", it.Got)
	}
	if got := transportCause(&runner.StepRecord{Transport: &runner.TransportError{Code: "unauthenticated", Message: "missing token"}}); got != "missing token" {
		t.Errorf("a short message is kept whole: %q", got)
	}
}
