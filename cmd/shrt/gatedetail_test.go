package main

import (
	"encoding/json"
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
		name          string
		e             *env
		rec           *runner.Record
		effect, times string
	}{
		{"the confirm took twice the approved fall", e, confirmRecord("p1", "10", "6"), "fell 4 from 10 to 6 where the approved run fell 2 from 10 to 8", "2x"},
		{"half the approved fall is a simple fraction", e, confirmRecord("p1", "10", "9"), "fell 1 from 10 to 9 where the approved run fell 2 from 10 to 8", "1/2x"},
		{"the same fall from another start has no ratio", e, confirmRecord("p1", "9", "7"), "fell 2 from 9 to 7 where the approved run fell 2 from 10 to 8", ""},
		{"a write that may move the field between them says nothing", e, confirmRecord("p1", "10", "6", cancel), "", ""},
		{"without contracts the order's effect is unknown, so nothing", &env{}, confirmRecord("p1", "10", "6"), "", ""},
	} {
		effect, times := effectOf(runAttribution(c.e, c.rec), spot, read)
		if effect != c.effect || times != c.times {
			t.Errorf("%s: got %q %q, want %q %q", c.name, effect, times, c.effect, c.times)
		}
	}
	noStock := confirmRecord("p1", "10", "6")
	noStock.Steps = append(noStock.Steps[:1], noStock.Steps[2:]...)
	if effect, _ := effectOf(runAttribution(e, noStock), spot, read); effect != "" {
		t.Errorf("no earlier read of the product, so no effect: %q", effect)
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
		if times != "-" {
			it.Effect = "fell 4 from 10 to 6 where the approved run fell 2 from 10 to 8"
		} else {
			it.Times = ""
		}
		return gateRef{chain: "c", it: it}
	}
	for _, c := range []struct {
		name string
		refs []gateRef
		want string
	}{
		{"all 2x", []gateRef{at("a", "2x"), at("b", "2x")}, " on every failing step"},
		{"one not measured", []gateRef{at("a", "2x"), at("b", "2x"), at("c", "-")}, " on 2 of 3 failing steps (no measure for the rest)"},
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
