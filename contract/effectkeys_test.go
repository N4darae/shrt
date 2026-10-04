package contract_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

var (
	anySummary     = regexp.MustCompile(`(?m)^(\s+)summary: .*$`)
	anyDescription = regexp.MustCompile(`(?m)^description: .*$`)
)

var shopEffects = map[string]string{
	"ProductService/CreateProduct": "{qty_on_hand: zero}",
	"StockService/AddStock":        "{qty_on_hand: {increase: qty}}",
	"StockService/AddStockBatch":   "{qty_on_hand: {increase: lines.qty}, lines: per_item}",
	"OrderService/CreateOrder":     "{qty_on_hand: none, total_minor: {sum: lines.qty, times: price_minor}}",
	"OrderService/ConfirmOrder":    "{qty_on_hand: {decrease: lines.qty, of: id_order}}",
	"OrderService/CancelOrder":     "{qty_on_hand: {restore: CONFIRMED}}",
}

func mute(body string) string {
	body = anySummary.ReplaceAllString(body, "${1}summary: Stated as data under effects.")
	body = anyDescription.ReplaceAllString(body, "description: Records and the numbers they hold.")
	body = strings.ReplaceAll(body, ", total_minor is the priced sum", "")
	body = strings.ReplaceAll(body, "one AddStock per line, applied independently in order", "repeated")
	return strings.ReplaceAll(body, "; reported on that line only", "")
}

func mutedPlan(t *testing.T, effects map[string]string, targets ...string) (*contract.Plan, string) {
	t.Helper()
	p := editedPlan(t, func(name, body string) string {
		body = mute(body)
		for rpc, e := range effects {
			body = regexp.MustCompile(`(?m)^(    \S+\.`+regexp.QuoteMeta(rpc)+`:\n)`).ReplaceAllString(body, "${1}        effects: "+e+"\n")
		}
		return body
	}, targets...)
	return p, strings.Join(p.Notes, "\n")
}

func assertsLiteral(p *contract.Plan, id, path string) bool {
	st, ok := p.Chain.Step(id)
	if !ok {
		return false
	}
	for _, e := range st.Expect {
		if e.Path == path && e.Equals != nil && !strings.Contains(fmt.Sprint(e.Equals), "${") {
			return true
		}
	}
	return false
}

func TestMutedContractsAssertNoNumbersWithoutEffects(t *testing.T) {
	p, _ := mutedPlan(t, nil, "CreateOrder", "ConfirmOrder", "CancelOrder", "AddStockBatch")
	for _, c := range [][2]string{{"add_stock", "qty_on_hand"}, {"create_order", "order.total_minor"},
		{"get_product_after_confirm_order", "product.qty_on_hand"}} {
		if assertsLiteral(p, c[0], c[1]) {
			t.Fatalf("with the prose muted and no effects, %s asserts no %s", c[0], c[1])
		}
	}
	if _, ok := p.Chain.Step("add_stock_batch_partial"); ok {
		t.Fatalf("with the prose muted and no effects, no per-item batch is planned")
	}
	base := map[string]string{"ProductService/CreateProduct": "{qty_on_hand: zero}", "StockService/AddStock": "{qty_on_hand: {increase: qty}}"}
	p, notes := mutedPlan(t, base, "CreateOrder", "CancelOrder")
	if want := "CancelOrder says nothing of what it gives back from CONFIRMED (add effects: {qty_on_hand: {restore: CONFIRMED}} if it does)"; !strings.Contains(notes, want) {
		t.Fatalf("want %q in:\n%s", want, notes)
	}
	if assertsLiteral(p, "get_product_after_create_order", "product.qty_on_hand") {
		t.Fatalf("without none, the read after create_order asserts no level")
	}
	for _, e := range planStep(t, p, "get_product_after_cancel_order_after_confirmed").Expect {
		if e.Path == "product.qty_on_hand" {
			t.Fatalf("without restore, the read after the cancel asserts nothing of the level: %+v", e)
		}
	}
}

func TestAnIncreaseStatedAsEffectsIsAsserted(t *testing.T) {
	p, _ := mutedPlan(t, shopEffects, "AddStock")
	wantExpect(t, planStep(t, p, "add_stock"), "qty_on_hand", 5)
	decrease := map[string]string{"ProductService/CreateProduct": "{qty_on_hand: zero}", "StockService/AddStock": "{qty_on_hand: {decrease: qty}}"}
	p, _ = mutedPlan(t, decrease, "AddStock")
	if assertsLiteral(p, "add_stock", "qty_on_hand") {
		t.Fatalf("a decrease below zero is not asserted: %+v", planStep(t, p, "add_stock").Expect)
	}
	want := "step add_stock: it takes qty_on_hand of create_product below zero, since nothing before it adds any, so no level is asserted after it"
	if gaps := strings.Join(p.GapNotes(), "\n"); !strings.Contains(gaps, want) {
		t.Fatalf("want gap %q in:\n%s", want, gaps)
	}
}

func TestAZeroStatedAsEffectsIsAsserted(t *testing.T) {
	p, notes := mutedPlan(t, shopEffects, "CreateProduct")
	wantExpect(t, planStep(t, p, "create_product"), "product.qty_on_hand", 0)
	if !strings.Contains(notes, `"effects: {qty_on_hand: zero}"`) {
		t.Fatalf("the note quotes the effect it read:\n%s", notes)
	}
}

func TestABatchStatedAsEffectsIsAssertedPerLine(t *testing.T) {
	p, _ := mutedPlan(t, shopEffects, "AddStockBatch")
	wantExpect(t, planStep(t, p, "add_stock_batch"), "results.0.qty_on_hand", 3)
	wantExpect(t, planStep(t, p, "add_stock_batch"), "results.1.qty_on_hand", 4)
}

func TestPerItemFailuresStatedAsEffectsPlanAPartialBatch(t *testing.T) {
	p, _ := mutedPlan(t, shopEffects, "AddStockBatch")
	partial := planStep(t, p, "add_stock_batch_partial")
	wantExpect(t, partial, "results.0.status.code", "SUCCESS")
}

func TestADecreaseOfAnotherRecordsLinesIsAsserted(t *testing.T) {
	p, _ := mutedPlan(t, shopEffects, "ConfirmOrder")
	wantExpect(t, planStep(t, p, "get_product_after_confirm_order"), "product.qty_on_hand", 5-2)
	wantExpect(t, planStep(t, p, "get_product_2_after_confirm_order"), "product.qty_on_hand", 6-3)
	if _, ok := p.Chain.Step("confirm_order_same_product_twice"); !ok {
		t.Fatalf("a decrease per line plans the same record on two lines")
	}
}

func TestNoneStatedAsEffectsAssertsTheLevelUnchanged(t *testing.T) {
	p, _ := mutedPlan(t, shopEffects, "CreateOrder")
	add := planStep(t, p, "add_stock")
	wantExpect(t, planStep(t, p, "get_product_after_create_order"), "product.qty_on_hand", bodyAt(t, add, "qty"))
}

func TestARestoreStatedAsEffectsAssertsTheLevelBack(t *testing.T) {
	p, _ := mutedPlan(t, shopEffects, "CancelOrder")
	wantExpect(t, planStep(t, p, "get_product_after_cancel_order_after_confirmed"), "product.qty_on_hand",
		"${get_product_before_confirm_order_before_cancel_order_after_confirmed.product.qty_on_hand}")
}

func TestATotalStatedAsEffectsIsAssertedAndProbedPast32Bits(t *testing.T) {
	p, _ := mutedPlan(t, shopEffects, "CreateOrder")
	wantExpect(t, planStep(t, p, "create_order"), "order.total_minor", 2*250+3*1250)
	wide := planStep(t, p, "create_order_wide_total")
	for _, e := range wide.Expect {
		if e.Path == "order.total_minor" && e.Equals != nil {
			return
		}
	}
	t.Fatalf("the wide total probe asserts the sum: %+v", wide.Expect)
}

func TestTheGapForAnUnstatedEffectPrintsTheEffectsToAdd(t *testing.T) {
	_, notes := mutedPlan(t, map[string]string{"ProductService/CreateProduct": "{qty_on_hand: zero}", "StockService/AddStock": "{qty_on_hand: {increase: qty}}"}, "ConfirmOrder")
	for _, want := range []string{
		"ConfirmOrder says nothing of qty_on_hand: add effects: {qty_on_hand: none} or {qty_on_hand: {decrease: lines.qty, of: id_order}}",
		"CreateOrder says nothing of qty_on_hand: add effects: {qty_on_hand: none} or {qty_on_hand: {increase: lines.qty}}",
	} {
		if !strings.Contains(notes, want) {
			t.Fatalf("want %q in:\n%s", want, notes)
		}
	}
}

func TestAnInvalidEffectIsRefusedAtLoad(t *testing.T) {
	cat, _ := shopDemo(t)
	for _, c := range []struct{ effect, want string }{
		{"{qty_on_hand: {increse: qty}}", `unknown key "increse" (did you mean "increase"?)`},
		{"{qty_on_hand: {increase: qtty}}", `"qtty" is not a number of the request (did you mean "qty"?)`},
		{"{qty_on_hnd: {increase: qty}}", "no request field is wired with from: to a record that answers qty_on_hnd"},
		{"{qty_on_hand: nothing}", `"nothing" is not an effect`},
		{"{qty_on_hnd: {sum: lines.qty, times: price}}", `sum: "qty_on_hnd" is not a number this rpc answers with (did you mean "qty_on_hand"?)`},
		{"{qty_on_hand: {increase: qty, restore: DONE}}", "exactly one of increase, decrease, restore or sum"},
		{"{qty_on_hand: {decrease: lines.qty, of: id_prodct}}", `of: "id_prodct" is not a request field wired with from: to another write (did you mean "id_product"?)`},
	} {
		dir := editedContracts(t, func(_, body string) string {
			return strings.Replace(body, "    shop.catalog.v1.StockService/AddStock:\n", "    shop.catalog.v1.StockService/AddStock:\n        effects: "+c.effect+"\n", 1)
		})
		_, broken, err := contract.LoadLibraryIn(dir, cat)
		if err != nil || len(broken) != 1 || !strings.Contains(broken[0].Error(), c.want) {
			t.Fatalf("%s: want one broken overlay saying %q, got %v %v", c.effect, c.want, err, broken)
		}
	}
}

func TestInitScaffoldsAnEffectsTodoOnlyWhereAnEffectCanBeStated(t *testing.T) {
	cat, _ := shopDemo(t)
	for domain, want := range map[string]map[string]string{
		"catalog": {"StockService/AddStock": "what this write does to qty_on_hand: none | {increase: qty} | {decrease: qty} |", "StockService/AddStockBatch": "what this write does to qty_on_hand: none | {increase: lines.qty} | {decrease: lines.qty} |", "ProductService/CreateProduct": ""},
		"orders":  {"OrderService/CreateOrder": "what this write does to <number>: none | {increase: lines.qty} | {decrease: lines.qty} |", "OrderService/ConfirmOrder": ""},
	} {
		raw := renderScaffold(t, domain, contract.Domains(cat.Methods())[domain], nil, cat)
		o, err := contract.LoadOverlayBytes(domain+".yaml", raw)
		if err != nil {
			t.Fatalf("a scaffold with an effects TODO loads: %v\n%s", err, raw)
		}
		for rpc, snippet := range want {
			c := o.RPCs["shop."+domain+".v1."+rpc]
			if c == nil {
				t.Fatalf("no %s in the scaffold", rpc)
			}
			if c.IsUnfilled("effects") != (snippet != "") || snippet != "" && !strings.Contains(string(raw), "TODO: "+snippet) {
				t.Fatalf("%s: want effects TODO %q:\n%s", rpc, snippet, raw)
			}
		}
		lib := contract.NewLibrary([]*contract.Overlay{o})
		without, err := contract.LoadOverlayBytes(domain+".yaml", []byte(regexp.MustCompile(`(?m)^\s+effects: .*\n`).ReplaceAllString(string(raw), "")))
		if err != nil {
			t.Fatal(err)
		}
		if a, b := contract.MeasurePhase(lib, cat, domain, contract.PhaseAll).TotalScore, contract.MeasurePhase(contract.NewLibrary([]*contract.Overlay{without}), cat, domain, contract.PhaseAll).TotalScore; a != b {
			t.Fatalf("an unfilled effects TODO costs nothing in quality: %d with it, %d without", a, b)
		}
	}
}
