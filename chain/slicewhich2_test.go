package chain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
)

func twoProductChain() *chain.Chain {
	return &chain.Chain{
		Name: "stock",
		Steps: []*chain.Step{
			{ID: "create_product_first", Call: "ProductService/CreateProduct", Body: map[string]any{"sku": "${vars.tag}-A"}},
			{ID: "create_product_second", Call: "ProductService/CreateProduct", Body: map[string]any{"sku": "${vars.tag}-B"}},
			{ID: "add_stock_second", Call: "StockService/AddStock", Body: map[string]any{"id_product": "${create_product_second.product.id_product}"}},
		},
	}
}

func TestSliceAnAliasMatchDominatesAReferencedStep(t *testing.T) {
	prereqs := func(rpc string) []chain.Prereq {
		if rpc == "StockService/AddStock" {
			return []chain.Prereq{{RPC: "ProductService/CreateProduct", Alias: "first", Edge: "from"}}
		}
		return nil
	}
	res, err := chain.Slice(twoProductChain(), "add_stock_second", chain.SliceOptions{Prereqs: prereqs})
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, k := range res.Kept {
		reasons[k.ID] = k.Reason
	}
	if got := reasons["create_product_first"]; got != "contract needs ProductService/CreateProduct@first (from)" {
		t.Fatalf("CreateProduct@first must be satisfied by the step carrying the alias, got %q in %+v", got, res.Kept)
	}
	if got := reasons["create_product_second"]; !strings.HasPrefix(got, "produces ") {
		t.Fatalf("create_product_second is kept for the reference, never labelled with another alias, got %q", got)
	}
}

func TestSliceSkipsAPrerequisiteScopedToAnotherAlias(t *testing.T) {
	prereqs := func(rpc string) []chain.Prereq {
		if rpc == "StockService/AddStock" {
			return []chain.Prereq{
				{RPC: "ProductService/CreateProduct", Alias: "first", Edge: "from", For: "first"},
				{RPC: "ProductService/CreateProduct", Alias: "second", Edge: "from", For: "second"},
			}
		}
		return nil
	}
	res, err := chain.Slice(twoProductChain(), "add_stock_second", chain.SliceOptions{Prereqs: prereqs})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(keptIDs(res), ","); got != "create_product_second,add_stock_second" {
		t.Fatalf("an edge declared by alias first does not bind a step of alias second, kept %s", got)
	}
}

func TestPinSliceTakesAnUndeclaredVarFromTheSourceRun(t *testing.T) {
	c := twoProductChain()
	res, err := chain.Slice(c, "create_product_second", chain.SliceOptions{
		Mode: chain.SliceModePin, RunID: "r1",
		Value:   func(string) (any, bool) { return nil, false },
		RunVars: map[string]any{"tag": "T1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Chain.Vars["tag"] != "T1" || len(res.MissingVars) != 0 {
		t.Fatalf("pin mode reuses the value the run used, got vars %v missing %v", res.Chain.Vars, res.MissingVars)
	}
	if len(res.FilledVars) != 1 || res.FilledVars[0].From != chain.VarFromRun {
		t.Fatalf("the filled var must say it came from the run: %+v", res.FilledVars)
	}
	if !strings.Contains(res.Chain.Description, "Var tag is not declared") {
		t.Fatalf("the description must say where tag came from:\n%s", res.Chain.Description)
	}
}

func TestClosureSliceNeverReusesTheRunsVarAndReportsItMissing(t *testing.T) {
	res, err := chain.Slice(twoProductChain(), "create_product_second", chain.SliceOptions{
		RunID: "r1", RunVars: map[string]any{"tag": "T1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.MissingVars, ",") != "tag" || res.Chain.Vars["tag"] != nil {
		t.Fatalf("closure re-creates what the run created, so the run's tag collides: vars %v missing %v", res.Chain.Vars, res.MissingVars)
	}
	res, err = chain.Slice(twoProductChain(), "create_product_second", chain.SliceOptions{Vars: map[string]any{"tag": "T2"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Chain.Vars["tag"] != "T2" || len(res.MissingVars) != 0 || res.FilledVars[0].From != chain.VarFromFlag {
		t.Fatalf("a -var value fills an undeclared var: vars %v filled %+v", res.Chain.Vars, res.FilledVars)
	}
}

func TestSliceDoesNotCountADroppedWriteTheSourceRunRefused(t *testing.T) {
	c := &chain.Chain{Name: "orders", Steps: []*chain.Step{
		{ID: "create_order", Call: "OrderService/CreateOrder"},
		{ID: "create_order_no_lines", Call: "OrderService/CreateOrder"},
		{ID: "confirm_twice", Call: "OrderService/ConfirmOrder"},
		{ID: "cancel_order", Call: "OrderService/CancelOrder", Body: map[string]any{"id": "${create_order.id}"}},
	}}
	refused := map[string]string{"create_order_no_lines": "refused: transport invalid_argument", "confirm_twice": "refused: error.code = 1303"}
	res, err := chain.Slice(c, "cancel_order", chain.SliceOptions{RunID: "r1", Refused: func(id string) (string, bool) {
		why, ok := refused[id]
		return why, ok
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.UnderIncluded || len(res.DroppedWrites) != 0 {
		t.Fatalf("refused writes wrote nothing and must not make the slice under-included: %+v", res.DroppedWrites)
	}
	if len(res.RefusedWrites) != 2 || res.RefusedWrites[1].Reason != "refused: error.code = 1303" {
		t.Fatalf("the refused writes must still be listed with why: %+v", res.RefusedWrites)
	}
	if !strings.Contains(res.Chain.Description, "refused in run r1 and wrote nothing") {
		t.Fatalf("the description must say they were refused:\n%s", res.Chain.Description)
	}
}

func TestCompareVerdictsNumbersExpectationsFromOne(t *testing.T) {
	src := chain.Verdict{Expect: []chain.ExpectResult{{Path: "error.code", Rule: "equals", Passed: true}, {Path: "qty", Rule: "equals", Passed: true}}}
	rep := chain.Verdict{Expect: []chain.ExpectResult{{Path: "error.code", Rule: "equals", Passed: true}, {Path: "qty", Rule: "equals", Passed: false}}}
	diffs := chain.CompareVerdicts(src, rep)
	if len(diffs) != 1 || !strings.HasPrefix(diffs[0], "expectation 2 (qty equals)") {
		t.Fatalf("expectations are numbered from 1 like steps, got %v", diffs)
	}
}

func TestMarkReproducedReplacesTheHypothesisInTheDescription(t *testing.T) {
	res, err := chain.Slice(twoProductChain(), "add_stock_second", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Chain.Description, "HYPOTHESIS") {
		t.Fatal("an unverified slice is a hypothesis")
	}
	res.MarkReproduced("src-run", "slice-run", time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC))
	d := res.Chain.Description
	if strings.Contains(d, "HYPOTHESIS") {
		t.Fatalf("a reproduced slice must stop calling itself a hypothesis:\n%s", d)
	}
	for _, want := range []string{"VERIFIED", "2026-09-24", "slice run slice-run", "source run src-run"} {
		if !strings.Contains(d, want) {
			t.Errorf("the description must record %q:\n%s", want, d)
		}
	}
	if !strings.HasPrefix(d, chain.SliceDescriptionPrefix("stock", "add_stock_second")) {
		t.Errorf("the description must keep the prefix that marks it as this slice:\n%s", d)
	}
}

func transportChains() []*chain.Chain {
	c := &chain.Chain{Name: "refusals", Steps: []*chain.Step{
		{ID: "add_stock_batch_empty", Call: "pkg.Stock/AddStockBatch", Expect: []chain.Expectation{
			{Path: "transport.code", Equals: "invalid_argument"},
			{Path: "transport.http_status", Equals: 400},
		}},
	}}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return []*chain.Chain{c}
}

func TestWhichCodeFindsATransportCodeAssertion(t *testing.T) {
	if !chain.IsCodePath("transport.code") {
		t.Fatal("transport.code names a failure code and must be searchable")
	}
	if chain.IsCodePath("transport.http_status") || chain.IsCodePath("transport.message") {
		t.Fatal("only transport.code is a code")
	}
	hits := chain.Which(transportChains(), chain.WhichQuery{Code: "invalid_argument"}, chain.WhichOptions{
		Observations: func(string) []chain.Observation {
			return []chain.Observation{{Run: "r1", Step: "add_stock_batch_empty", Status: "passed", Reached: true,
				Response: chain.TransportOutcome(400, "invalid_argument", "lines must not be empty")}}
		},
	})
	if len(hits) != 1 {
		t.Fatalf("the step asserts transport.code equals invalid_argument, got %d hit(s)", len(hits))
	}
	ev := hits[0].Matches[0].Observed
	if ev == nil || ev.Code != "invalid_argument" || ev.Path != "transport.code" || !ev.Holds {
		t.Fatalf("the transport code is read from the recorded outcome: %+v", ev)
	}
}

func TestWhichNeverReportsTransportOKAsTheFallbackCode(t *testing.T) {
	c := &chain.Chain{Name: "a", Steps: []*chain.Step{
		{ID: "s", Call: "pkg.Svc/Do", Expect: []chain.Expectation{{Path: "error.details.0.app_code", Equals: 1218}, {Path: "transport.code", Equals: "ok"}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	hits := chain.Which([]*chain.Chain{c}, chain.WhichQuery{Code: "1218"}, chain.WhichOptions{
		Observations: func(string) []chain.Observation {
			return []chain.Observation{{Run: "r1", Step: "s", Status: "failed", Reached: true, Response: chain.TransportOutcome(200, "", "")}}
		},
	})
	if ev := hits[0].Matches[0].Observed; ev == nil || ev.Code != "" {
		t.Fatalf("a call answered 200 has no failure code to cite, got %+v", ev)
	}
}

func TestWhichRPCRanksAFailedStepAfterAnAssertedOne(t *testing.T) {
	c := &chain.Chain{Name: "stock", Steps: []*chain.Step{
		{ID: "stock_after_cancel", Call: "pkg.Svc/GetProduct", Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}, {Path: "qty", Equals: 10}}},
		{ID: "stock_unrun", Call: "pkg.Svc/GetProduct", Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "stock_ok", Call: "pkg.Svc/GetProduct", Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	hits := chain.Which([]*chain.Chain{c}, chain.WhichQuery{RPC: "pkg.Svc/GetProduct"}, chain.WhichOptions{
		Observations: func(string) []chain.Observation {
			return []chain.Observation{
				{Run: "r1", Step: "stock_after_cancel", Status: "failed", Reached: true, Response: okResponse()},
				{Run: "r1", Step: "stock_ok", Status: "passed", Reached: true, Response: okResponse()},
			}
		},
	})
	got := []string{}
	for _, m := range hits[0].Matches {
		got = append(got, m.Step)
	}
	if strings.Join(got, ",") != "stock_ok,stock_unrun,stock_after_cancel" {
		t.Fatalf("a step that FAILED contradicts the chain and ranks last, as -code ranks it, got %v", got)
	}
}

func TestWhichReproduceCommandAsksForFreshVars(t *testing.T) {
	hits := chain.Which(transportChains(), chain.WhichQuery{Code: "invalid_argument"}, chain.WhichOptions{
		FreshVars: func(c *chain.Chain, step, run string) []string { return []string{"tag", "email"} },
	})
	if want := "shrt chain slice refusals -step add_stock_batch_empty -var email=<fresh> -var tag=<fresh>"; hits[0].Command != want {
		t.Fatalf("want %q\ngot  %q", want, hits[0].Command)
	}
}
