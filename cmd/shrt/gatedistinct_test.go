package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestEveryChangeAPassingWriteCausesHasADistinctRowSplitByHowTheWriteWasSent(t *testing.T) {
	const create, move, get = "shrt.test.v1.ThingService/Create", "shrt.test.v1.ThingService/Move", "shrt.test.v1.ThingService/Get"
	price := gateItem{Step: "create", Call: create, Path: "thing.price", Want: "250", Got: "249"}
	echo := gateItem{Step: "get_after_create", Call: get, Path: "thing.price", Want: "250", Got: "249", Suspect: create, SuspectStep: "create"}
	refusedMove := gateItem{Step: "get_after_move_refused", Call: get, Path: "thing.level", Want: "10", Got: "-1",
		Suspect: move, SuspectStep: "move_refused", Variant: "refused (1305 TooFew)"}
	otherMove := gateItem{Step: "get_after_move_as_other", Call: get, Path: "thing.level", Want: "8", Got: "6",
		Suspect: move, SuspectStep: "move_as_other", Variant: "as other"}
	gateWorkspace(t, map[string][]gateOutcome{
		"verify cli-thing-flow": {{code: 1, side: gateSidecar{Items: []gateItem{price, echo, refusedMove, otherMove},
			Sent: map[string]string{"create": ` sent {"price":"250"}`, "move_refused": ` sent {"id":"t1"}`, "move_as_other": ` as other sent {"id":"t2"}`}}}},
	})
	out, _ := runGateOut(t, "-v")
	for _, want := range []string{
		"    thing.level after Move refused (1305 TooFew) at 1 step(s) (get_after_move_refused); e.g. want=10 got=-1\n",
		"    thing.level at 1 step(s) (get_after_move_as_other); e.g. want=8 got=6\n",
		"  ThingService/Move refused (1305 TooFew): passed itself, but steps after it failed or changed; e.g. cli-thing-flow move_refused\n",
		"  ThingService/Move: passed itself",
		"  ThingService/Create thing.price value: 1 step(s) in 1 chain(s); e.g. cli-thing-flow create thing.price want=250 got=249\n",
		"  ThingService/Move refused (1305 TooFew) -> Get thing.level value: 1 step(s) in 1 chain(s); e.g. cli-thing-flow get_after_move_refused thing.level want=10 got=-1; move_refused sent {\"id\":\"t1\"}\n",
		"  ThingService/Move -> Get thing.level value: 1 step(s) in 1 chain(s); e.g. cli-thing-flow get_after_move_as_other thing.level want=8 got=6; move_as_other as other sent {\"id\":\"t2\"}\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "-> Get thing.price") {
		t.Errorf("a read answering the root's changed field folds under the root's row:\n%s", out)
	}
}

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
		"  ThingService/Create thing.parts order: 1 step(s) in 1 chain(s); e.g. cli-thing-flow create thing.parts same items in another order\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, " value: ") {
		t.Errorf("a permutation is no value change:\n%s", out)
	}
}

func TestTheSuspectWriteCarriesItsProfileAndRefusalUnlessItChangedItself(t *testing.T) {
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
		"get_product_after_confirm_order_short":    "refused (1305 Short)",
		"get_product_after_confirm_order_as_clerk": "as clerk",
	} {
		if it := a.item(gateItem{Step: step, Call: shopGet, Path: "product.qty_on_hand"}); it.Variant != want {
			t.Errorf("%s: want variant %q, got %+v", step, want, it)
		}
	}
	rec.Steps[4] = shopStep("confirm_order_as_clerk", shopConfirm, `{"order":{"id_order":"o2","n":"1"},`+shopOK+`}`, "create_order").failing("order.n", "2", "1").StepRecord
	rec.Steps[4].AuthProfile = "clerk"
	if it := runAttribution(nil, rec).item(gateItem{Step: "get_product_after_confirm_order_as_clerk", Call: shopGet, Path: "product.qty_on_hand"}); it.Variant != "as clerk" {
		t.Errorf("a write that changed itself is the root under its own rpc and profile, got %+v", it)
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
		{Step: "move_as_other", Call: move, Path: "status.code", Rule: "not_equal", Want: "SUCCESS", Got: "SUCCESS", Failed: true, Variant: "as other"},
		{Step: "get_after_move_as_other", Call: get, Path: "thing.level", Want: "0", Got: "10", Failed: true,
			Suspect: move, SuspectStep: "move_as_other", Variant: "as other"},
	}
	gateWorkspace(t, map[string][]gateOutcome{"verify cli-thing-flow": {{code: 1, side: gateSidecar{Items: items}}}})
	out, _ := runGateOut(t, "-v")
	for _, want := range []string{
		"    status.code at 2 step(s) (move_zero, move_as_other); e.g. want≠SUCCESS got=SUCCESS\n",
		"  ThingService/Move: 2 step(s) in 1 chain(s), paths status.code; e.g. cli-thing-flow move_zero status.code",
		"  ThingService/Move status.code value: 2 step(s) in 1 chain(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "passed itself") || strings.Contains(out, "Move as other") {
		t.Errorf("a write whose own expectations failed did not pass itself, and failing under both profiles it is one change:\n%s", out)
	}
}

func TestAWriteFailingOnlyUnderOneProfileKeepsItsProfile(t *testing.T) {
	const move, get = "shrt.test.v1.ThingService/Move", "shrt.test.v1.ThingService/Get"
	items := []gateItem{
		{Step: "get_after_move_as_other", Call: get, Path: "thing.level", Want: "8", Got: "6", Suspect: move, SuspectStep: "move_as_other", Variant: "as other"},
	}
	gateWorkspace(t, map[string][]gateOutcome{"verify cli-thing-flow": {{code: 1, side: gateSidecar{Items: items}}}})
	out, _ := runGateOut(t, "-v")
	if !strings.Contains(out, "  ThingService/Move as other: passed itself") {
		t.Errorf("a write whose steps after it failed only under one profile names the profile:\n%s", out)
	}
}
