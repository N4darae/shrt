package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAReorderedListIsOneOrderChangeNotAValueChangePerField(t *testing.T) {
	const create = "shrt.test.v1.ThingService/Create"
	items := []gateItem{
		{Step: "create", Call: create, Path: "thing.parts.0.n", Want: "2", Got: "3", Kind: "order", Class: "order changed"},
		{Step: "create", Call: create, Path: "thing.parts.0.id", Want: "p1", Got: "p2", Kind: "order", Class: "order changed"},
	}
	gateWorkspace(t, map[string][]gateOutcome{"verify cli-thing-flow": {{code: 1, side: gateSidecar{Items: items}}}})
	out, _ := runGateOut(t, "-v")
	for _, want := range []string{
		"order changed: create (ThingService/Create) thing.parts same items in another order\n",
		"    thing.parts at 1 step(s) (create); e.g. same items in another order\n",
		"  ThingService/Create thing.parts: 1 step(s) in 1 chain(s); e.g. cli-thing-flow create thing.parts same items in another order\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "want=") {
		t.Errorf("a permutation is no value change:\n%s", out)
	}
}

func TestTheSuspectWriteCarriesItsProfile(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	product := func(qty string) string {
		return `{"product":{"id_product":"p1","qty_on_hand":"` + qty + `"},` + shopOK + `}`
	}
	refused := shopStep("confirm_order_short", shopConfirm, `{"status":{"code":"REJECTED","details":[{"app_code":1305,"reason":"Short"}]}}`, "create_order")
	clerk := shopStep("confirm_order_as_clerk", shopConfirm, `{"order":{"id_order":"o2"},`+shopOK+`}`, "create_order")
	clerk.AuthProfile = "clerk"
	rec := shopRecord(
		shopStep("create_product", shopCreate, product("10")),
		shopStep("create_order", shopOrder, `{"order":{"id_order":"o1"},`+shopOK+`}`, "create_product"),
		refused,
		shopStep("get_product_after_confirm_order_short", shopGet, product("-1"), "create_product").failing("product.qty_on_hand", "10", "-1"),
		clerk,
		shopStep("get_product_after_confirm_order_as_clerk", shopGet, product("6"), "create_product").failing("product.qty_on_hand", "8", "6"),
	)
	a := runAttribution(nil, rec)
	for step, want := range map[string]string{
		"get_product_after_confirm_order_short":    "suspect write confirm_order_short (OrderService/ConfirmOrder)",
		"get_product_after_confirm_order_as_clerk": "suspect write confirm_order_as_clerk (OrderService/ConfirmOrder) as clerk",
	} {
		if it := a.item(gateItem{Step: step, Call: shopGet, Path: "product.qty_on_hand"}); it.Reason.String() != want {
			t.Errorf("%s: want %q, got %+v", step, want, it)
		}
	}
	rec.Steps[4] = shopStep("confirm_order_as_clerk", shopConfirm, `{"order":{"id_order":"o2","n":"1"},`+shopOK+`}`, "create_order").failing("order.n", "2", "1").StepRecord
	rec.Steps[4].AuthProfile = "clerk"
	if it := runAttribution(nil, rec).item(gateItem{Step: "get_product_after_confirm_order_as_clerk", Call: shopGet, Path: "product.qty_on_hand"}); it.Reason.Profile != "clerk" || it.suspect() != "confirm_order_as_clerk" {
		t.Errorf("a write that changed itself stays the suspect, with its profile, got %+v", it)
	}
}

func TestAReorderedListInsideAnotherIsNamedByItsOwnPath(t *testing.T) {
	for path, want := range map[string]string{"order.lines.0.qty": "order.lines", "orders.0.lines.1.qty": "orders[].lines", "messages.0.order.lines.0.id": "messages[].order.lines"} {
		if got := listOf(path); got != want {
			t.Errorf("%s: got %q, want %q", path, got, want)
		}
	}
}

func TestAWriteThatFailedItsOwnExpectationsUnderTwoProfilesIsOneChange(t *testing.T) {
	const move, get = "shrt.test.v1.ThingService/Move", "shrt.test.v1.ThingService/Get"
	items := []gateItem{
		{Step: "move_zero", Call: move, Path: "status.code", Rule: "not_equal", Want: "SUCCESS", Got: "SUCCESS", Failed: true},
		{Step: "move_as_other", Call: move, Path: "status.code", Rule: "not_equal", Want: "SUCCESS", Got: "SUCCESS", Failed: true},
		{Step: "get_after_move_as_other", Call: get, Path: "thing.level", Want: "0", Got: "10", Failed: true,
			Reason: reason{Kind: reasonWrite, Step: "move_as_other", RPC: move, Profile: "other"}},
	}
	gateWorkspace(t, map[string][]gateOutcome{"verify cli-thing-flow": {{code: 1, side: gateSidecar{Items: items}}}})
	out, _ := runGateOut(t, "-v")
	for _, want := range []string{
		"    status.code at 2 step(s) (move_zero, move_as_other); e.g. want≠SUCCESS got=SUCCESS\n",
		"  ThingService/Move status.code, thing.level: 3 step(s) in 1 chain(s); e.g. cli-thing-flow get_after_move_as_other; write move_as_other as other\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "  ThingService/Move") != 1 {
		t.Errorf("one line for the rpc whatever the profile:\n%s", out)
	}
}
