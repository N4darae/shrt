package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

type gateCase struct {
	name    string
	chains  func() []*gateChain
	verbose bool
}

func gateCases() []gateCase {
	const create, confirm = "x.v1.OrderService/CreateOrder", "x.v1.OrderService/ConfirmOrder"
	status := func(step string) gateItem {
		return gateItem{Step: step, Call: create, Path: "order.status", Want: "CONFIRMED", Got: "PENDING", Failed: true, Reason: reason{Kind: reasonWrite, Step: step, RPC: create}}
	}
	total := gateItem{Step: "create", Call: create, Path: "order.total_minor", Want: "600", Got: "400", Failed: true, Reason: reason{Kind: reasonWrite, Step: "create", RPC: create}}
	confirmed := gateItem{Step: "confirm", Call: confirm, Path: "order.status", Want: "CONFIRMED", Got: "PENDING", Failed: true, Reason: reason{Kind: reasonWrite, Step: "confirm", RPC: confirm}}
	listItems := func() []gateItem {
		set := reason{Kind: reasonSet, Step: "list", RPC: "x.v1.S/List", Path: "list"}
		var items []gateItem
		for _, p := range []string{"list.0.a", "list.0.b", "list"} {
			items = append(items, gateItem{Step: "list", Call: "x.v1.S/List", Path: p, Want: "1", Got: "2", Reason: set})
		}
		knock := reason{Kind: reasonKnockOn, Step: "make", RPC: "x.v1.S/Make"}
		return append(items,
			gateItem{Step: "list_2", Call: "x.v1.S/List", Path: "list", Want: "1", Got: "2", Reason: reason{Kind: reasonOrder, Step: "list_2", RPC: "x.v1.S/List"}},
			gateItem{Step: "find", Call: "x.v1.S/Find", Path: "n", Want: "1", Got: "2", Reason: reason{Kind: reasonError, Step: "find", RPC: "x.v1.S/Find", Got: "internal"}},
			gateItem{Step: "make", Call: "x.v1.S/Make", Path: "n", Want: "1", Got: "2", Reason: reason{Kind: reasonWrite, Step: "make", RPC: "x.v1.S/Make"}},
			gateItem{Step: "get", Call: "x.v1.S/Get", Path: "status", Want: "passed", Got: "failed", Reason: knock},
			gateItem{Step: "get_2", Call: "x.v1.S/Get", Path: "status", Want: "passed", Got: "failed", Reason: knock},
		)
	}
	stored := func(name string) *gateChain {
		return &gateChain{name: name, failed: true, items: []gateItem{{Step: "get", Call: "x.v1.S/Get", Path: "thing.state", Want: "DONE", Got: "OPEN", Failed: true,
			Reason: reason{Kind: reasonStored, Step: "w", RPC: "x.v1.S/Confirm", Path: "thing.state", Want: "DONE", Got: "OPEN", ReadRPC: "Get"}}}}
	}
	sent := func(name string) *gateChain {
		return &gateChain{name: name, failed: true, sent: map[string]string{"get": " sent {}", "w": " sent {\"w\":1}"},
			items: []gateItem{{Step: "get", Call: "x.v1.S/Get", Path: "thing.state", Want: "a", Got: "b", Failed: true, Reason: reason{Kind: reasonWrite, Step: "w", RPC: "x.v1.S/Move"}}}}
	}
	const add, thingCreate, get = "shrt.test.v1.ThingService/Add", "shrt.test.v1.ThingService/Create", "shrt.test.v1.ThingService/Get"
	failedAdd := func(step string) gateItem {
		return gateItem{Step: step, Call: add, Path: "status.code", Want: "REJECTED", Got: "SUCCESS", Failed: true, Reason: reason{Kind: reasonWrite, Step: step, RPC: add}}
	}
	laterWrites := func() []*gateChain {
		chain.SetEnvelope("status.code", "SUCCESS")
		defer chain.SetEnvelope("", "")
		chainRec := shopRecord(
			shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`).failing("product.created_at", "t", nil),
			shopStep("add_negative", shopAdd, `{"qty_on_hand":"0",`+shopOK+`}`, "create").failing("status.code", "REJECTED", "SUCCESS"),
			shopStep("add_large", shopAdd, `{"qty_on_hand":"1250",`+shopOK+`}`, "create").failing("qty_on_hand", "1251", "1250"),
			shopStep("add_larger", shopAdd, `{"qty_on_hand":"13595",`+shopOK+`}`, "create").failing("qty_on_hand", "13596", "13595"),
		)
		a := runAttribution(nil, chainRec)
		g := &gateChain{name: "c", failed: true}
		for _, st := range []string{"add_negative", "add_large", "add_larger"} {
			path := "qty_on_hand"
			if st == "add_negative" {
				path = "status.code"
			}
			g.items = append(g.items, a.item(gateItem{Step: st, Call: shopAdd, Path: path, Failed: true}))
		}
		return []*gateChain{g}
	}
	asRun := func() []*gateChain {
		chain.SetEnvelope("status.code", "SUCCESS")
		defer chain.SetEnvelope("", "")
		var out []*gateChain
		for _, c := range []struct {
			name  string
			rec   *runner.Record
			moved []diff.Change
		}{{"stock", stampRecord(true), lostStamp()}, {"clerk", heldPriceRecord(), heldPriceMoved()}, {"orders", refusedElsewhereRecord(), refusedElsewhereMoved()}} {
			a := changesAttribution(&env{cat: catalogtest.Shop()}, c.rec, c.moved)
			g := &gateChain{name: c.name, failed: true}
			for _, m := range c.moved {
				if st, ok := c.rec.Step(m.Step); ok && m.Kind != diff.KindStatus {
					g.items = append(g.items, a.item(gateItem{Step: m.Step, Call: st.Call, Path: m.Path, Want: compactValue(m.Want), Got: compactValue(m.Got), Failed: true}))
				}
			}
			out = append(out, g)
		}
		return out
	}
	stock := func(step string, r reason, pinned string) gateItem {
		return gateItem{Step: step, Call: shopGet, Path: "product.qty_on_hand", Want: "2", Got: "1", Failed: pinned == "", Pinned: pinned, Reason: r}
	}
	either := reason{Kind: reasonUnclear, Step: "confirm", RPC: shopConfirm, Or: []reason{{Step: "confirm", RPC: shopConfirm}, {Step: "cancel", RPC: shopCancel}}}
	return []gateCase{
		{name: "a read unclear between two writes names both and is grouped once, under the earlier", chains: func() []*gateChain {
			return []*gateChain{
				{name: "alone", failed: true, items: []gateItem{stock("get_b", either, "")}},
				{name: "flow", failed: true, items: []gateItem{stock("get_b", reason{Kind: reasonWrite, Step: "confirm", RPC: shopConfirm}, "")}},
				{name: "flow-slice-get_b", failed: true, keptRed: runner.KeptRedNotAsPinned, items: []gateItem{stock("get_b", either, "2")}},
			}
		}},
		{name: "an unclear row whose writes include an earlier row's suspect for the field is that fault, named with its rpc", chains: func() []*gateChain {
			return []*gateChain{
				{name: "cancels", failed: true, items: []gateItem{stock("get_b", reason{Kind: reasonWrite, Step: "cancel", RPC: shopCancel}, "")}},
				{name: "cancels-slice-get_b", failed: true, keptRed: runner.KeptRedNotAsPinned, items: []gateItem{stock("get_b", either, "2")}},
			}
		}},
		{name: "an unclear row is labelled with the path its read changed, not its first write's own field", chains: func() []*gateChain {
			total := gateItem{Step: "order_short", Call: shopOrder, Path: "order.total_minor", Want: "160", Got: "130", Failed: true, Reason: reason{Kind: reasonWrite, Step: "order_short", RPC: shopOrder}}
			status := gateItem{Step: "confirm", Call: shopConfirm, Path: "order.status", Want: "CONFIRMED", Got: "PENDING", Failed: true, Reason: reason{Kind: reasonWrite, Step: "confirm", RPC: shopConfirm}}
			short := reason{Kind: reasonUnclear, Step: "order_short", RPC: shopOrder, Or: []reason{{Step: "order_short", RPC: shopOrder}, {Step: "confirm", RPC: shopConfirm}}}
			return []*gateChain{
				{name: "guard", failed: true, items: []gateItem{total, status, stock("get_b", short, "")}},
				{name: "guard-slice", failed: true, keptRed: runner.KeptRedNotAsPinned, items: []gateItem{stock("get_b", reason{Kind: reasonWrite, Step: "confirm", RPC: shopConfirm}, "2")}},
			}
		}},
		{name: "one suspect rpc is one row naming each field it changed, wherever a read shows it", chains: func() []*gateChain {
			const createCustomer, list = "shop.customers.v1.CustomerService/CreateCustomer", "shop.catalog.v1.ProductService/ListProducts"
			confirmBy := func(step string) reason {
				return reason{Kind: reasonWrite, Step: step, RPC: shopConfirm, Profile: "clerk"}
			}
			own := func(step, path, want, got string) gateItem {
				return gateItem{Step: step, Call: createCustomer, Path: path, Want: want, Got: got, Failed: true, Reason: reason{Kind: reasonWrite, Step: step, RPC: createCustomer}}
			}
			return []*gateChain{
				{name: "clerk", failed: true, items: []gateItem{{Step: "admin_read_back", Call: shopGet, Path: "product.qty_on_hand", Want: "0", Got: "-3", Failed: true, Reason: confirmBy("clerk_confirm")}}},
				{name: "explore", failed: true, items: []gateItem{
					{Step: "a_after_two", Call: shopGet, Path: "product.qty_on_hand", Want: "7", Got: "4", Failed: true, Reason: confirmBy("confirm_two")},
					{Step: "list_prefix", Call: list, Path: "products.1.qty_on_hand", Want: "7", Got: "4", Failed: true, Reason: confirmBy("confirm_two")},
				}},
				{name: "customers", failed: true, items: []gateItem{own("create_long", "customer.name", "Customer t1-abcdefghijklmnopqrstuvwxyz", "Customer t1-a"), own("create_unicode", "code", "<none>", "internal")}},
			}
		}},
		{name: "two reads of one write's field in other shapes name that write once", chains: func() []*gateChain {
			byConfirm := reason{Kind: reasonWrite, Step: "confirm_two", RPC: shopConfirm, Profile: "clerk"}
			return []*gateChain{{name: "explore", failed: true, items: []gateItem{
				{Step: "a_after_two", Call: shopGet, Path: "product.qty_on_hand", Want: "7", Got: "4", Failed: true, Reason: byConfirm},
				{Step: "list_prefix", Call: shopList, Path: "products.1.qty_on_hand", Want: "7", Got: "4", Failed: true, Reason: byConfirm},
			}}}
		}},
		{name: "an also suspect write names the field it changed", chains: func() []*gateChain {
			total := gateItem{Step: "order_short", Call: shopOrder, Path: "order.total_minor", Want: "160", Got: "130", Failed: true, Reason: reason{Kind: reasonWrite, Step: "order_short", RPC: shopOrder}}
			return []*gateChain{{name: "guard-slice", failed: true, keptRed: runner.KeptRedNotAsPinned, items: []gateItem{
				stock("get_b", reason{Kind: reasonWrite, Step: "stock_batch", RPC: shopBatch}, "2"), total,
			}}}
		}},
		{name: "a role is named only when no other role fails the same way", chains: func() []*gateChain {
			confirmAs := func(step, profile string) gateItem {
				return gateItem{Step: step, Call: shopConfirm, Path: "order.status", Want: "CONFIRMED", Got: "PENDING", Failed: true, Reason: reason{Kind: reasonWrite, Step: step, RPC: shopConfirm, Profile: profile}}
			}
			return []*gateChain{
				{name: "clerk-vs-admin", failed: true, items: []gateItem{confirmAs("clerk_confirm", "clerk")}},
				{name: "lifecycle", failed: true, items: []gateItem{confirmAs("confirm", "")}},
				{name: "clerk-stock", failed: true, items: []gateItem{stock("get_b", reason{Kind: reasonWrite, Step: "clerk_confirm_big", RPC: shopConfirm, Profile: "clerk"}, "")}},
			}
		}},
		{name: "an unclear between two writes no row settles is grouped under both", chains: func() []*gateChain {
			return []*gateChain{{name: "alone", failed: true, items: []gateItem{stock("get_b", either, "")}}}
		}},
		{name: "an unclear write or read no row settles is grouped under both, a decisive row apart", chains: func() []*gateChain {
			name := func(step string, r reason) gateItem {
				return gateItem{Step: step, Call: "x.v1.S/GetCustomer", Path: "customer.name", Want: "Ann", Got: "ann@example.test", Failed: true, Reason: r}
			}
			unclear := reason{Kind: reasonUnclear, Step: "create", RPC: "x.v1.S/CreateCustomer", Read: "get", ReadRPC: "x.v1.S/GetCustomer", Path: "customer.name", Want: "Ann", Got: "ann@example.test"}
			return []*gateChain{
				{name: "customers", failed: true, items: []gateItem{name("get", unclear)}},
				{name: "lookup", failed: true, items: []gateItem{name("get", unclear)}},
				{name: "orders", failed: true, items: []gateItem{stock("get_b", reason{Kind: reasonWrite, Step: "confirm", RPC: shopConfirm}, "")}},
			}
		}},
		{name: "a changed read is filed under the write since the last read that matched, the other profile, or the read", chains: asRun},
		{name: "one line per suspect rpc, knock-ons folded under the write", chains: func() []*gateChain { return []*gateChain{{name: "one", items: listItems()}} }},
		{name: "-v counts the knock-on steps", verbose: true, chains: func() []*gateChain { return []*gateChain{{name: "one", items: listItems()}} }},
		{name: "the example carries its reason", chains: func() []*gateChain { return []*gateChain{stored("one"), stored("two")} }},
		{name: "the example with a reason is preferred", chains: func() []*gateChain {
			unclear := reason{Kind: reasonUnclear, Step: "w", RPC: "x.v1.S/Batch", Read: "get", ReadRPC: "x.v1.S/Get", Path: "results.2.qty_on_hand", Want: "12", Got: "6"}
			return []*gateChain{{name: "one", items: []gateItem{
				{Step: "later", Call: "x.v1.S/Batch", Path: "results.1.qty_on_hand", Want: "18", Got: "12"},
				{Step: "get", Call: "x.v1.S/Get", Path: "product.qty_on_hand", Want: "12", Got: "6", Reason: unclear},
			}}}
		}},
		{name: "a row's example names its rpc for sure before one unclear between it and another", chains: func() []*gateChain {
			unclear := reason{Kind: reasonUnclear, Step: "batch", RPC: shopBatch, Read: "get", ReadRPC: shopGet, Path: "results.1.qty_on_hand", Want: "12", Got: "6"}
			return []*gateChain{
				{name: "batches", failed: true, items: []gateItem{stock("get", unclear, "")}},
				{name: "guard-slice", failed: true, keptRed: runner.KeptRedNotAsPinned, items: []gateItem{stock("get_b", reason{Kind: reasonWrite, Step: "stock_batch", RPC: shopBatch}, "2")}},
			}
		}},
		{name: "knock-on changes on the same record fold into the root write", chains: laterWrites},
		{name: "a chain with a root the earlier chain lacks names that root, not the same fault", chains: func() []*gateChain {
			return []*gateChain{
				{name: "replay", failed: true, items: []gateItem{status("replay")}},
				{name: "orders", failed: true, items: []gateItem{status("replay_2"), total}},
				{name: "lists", failed: true, items: []gateItem{total}},
			}
		}},
		{name: "a fault a chain with a safe spot shows is anchored there, though a chain without one shows it first", chains: func() []*gateChain {
			return []*gateChain{
				{name: "explore", failed: true, items: []gateItem{total}},
				{name: "orders", failed: true, spot: true, items: []gateItem{total}},
				{name: "lists", failed: true, spot: true, items: []gateItem{total}},
			}
		}},
		{name: "a chain with a fault the anchor's line does not name names it, though the anchor has it too", chains: func() []*gateChain {
			batch := stock("stock_b", reason{Kind: reasonWrite, Step: "stock_batch", RPC: shopBatch}, "")
			return []*gateChain{
				{name: "explore", failed: true, spot: true, items: []gateItem{total, confirmed, batch}},
				{name: "guard", failed: true, spot: true, items: []gateItem{total, batch}},
				{name: "orders", failed: true, spot: true, items: []gateItem{total, confirmed}},
			}
		}},
		{name: "a slice failing as its parent folds into the parent's line", chains: func() []*gateChain {
			return []*gateChain{
				{name: "orders", failed: true, items: []gateItem{confirmed}},
				{name: "orders-slice-list", failed: true, items: []gateItem{confirmed}},
				{name: "orders-slice-held", failed: true, pinsHeld: true, pins: "list ListOrders orders, get GetProduct product.qty_on_hand; pinned 2026-09-28", items: []gateItem{confirmed}},
				{name: "orders-slice-stock", failed: true, pinsHeld: true, pins: "stock GetProduct product.qty_on_hand; pinned 2026-09-30", items: []gateItem{confirmed}},
				{name: "orders-slice-other", failed: true, pinsHeld: true, items: []gateItem{{Step: "list", Call: "x.v1.OrderService/ListOrders", Path: "orders", Want: "2", Got: "1", Failed: true}}},
				{name: "orders-slice-more", failed: true, pinsHeld: true, items: []gateItem{confirmed, total}},
				{name: "orders-slice-pin", failed: true, items: []gateItem{{Step: "confirm", Call: confirm, Path: "order.status", Want: "PENDING", Got: "PENDING", Pinned: "CONFIRMED"}}},
			}
		}},
		{name: "a failed first change leads over a drift", chains: func() []*gateChain {
			return []*gateChain{
				{name: "a", failed: true, items: []gateItem{failedAdd("add")}},
				{name: "b", failed: true, items: []gateItem{failedAdd("add_as_clerk"), failedAdd("add_again"), {Step: "create", Call: thingCreate, Path: "thing.name", Want: "long name", Got: "long"}}},
			}
		}},
		{name: "a moved pin with no suspect does not point above", chains: func() []*gateChain {
			return []*gateChain{
				{name: "a", failed: true, items: []gateItem{{Step: "get", Call: get, Path: "thing.level", Want: "5", Got: "8"}}},
				{name: "b", failed: true, items: []gateItem{{Step: "get_pinned", Call: get, Path: "thing.level", Want: "4", Got: "2", Pinned: "-1"}}},
			}
		}},
		{name: "-v shows the suspect's request and the same fault in a later chain", verbose: true, chains: func() []*gateChain { return []*gateChain{sent("one"), sent("two")} }},
		{name: "-v prints verify's lines, a change at more steps or items once", verbose: true, chains: func() []*gateChain {
			return []*gateChain{{name: "orders", failed: true, items: []gateItem{total}, shown: []string{
				"[create] changed    order.total_minor want=600 got=400",
				"[confirm] changed    order.total_minor want=600 got=400",
				"[fetch] changed    order.total_minor want=600 got=400",
				"[list] changed    orders.0.total_minor want=600 got=400",
				"[list] changed    orders.1.total_minor want=900 got=700",
				"[get] type       customer want=null <nil> got=object {\"name\":\"\"}",
				"[get] length     status.details want=1 item(s) got=0 item(s)",
				"[later..last] not_reached status 2 step(s) want=passed, not sent (it reads step \"get\", which did not pass)",
			}}}
		}},
		{name: "-v lists each changed path once", verbose: true, chains: func() []*gateChain {
			return []*gateChain{{name: "one", failed: true, items: []gateItem{
				{Step: "make", Path: "thing.n", Want: "1", Got: "2"},
				{Step: "get", Path: "thing.n", Want: "1", Got: "2"},
				{Step: "get", Path: "thing.n", Want: "1", Got: "2"},
				{Step: "list", Path: "things.3.n", Want: "1", Got: "2"},
				{Step: "later", Path: "status", Want: "passed", Got: "failed", Reason: reason{Kind: reasonKnockOn, Step: "make", RPC: "x.v1.S/Make"}},
				{Step: "put", Path: "(error)", Got: "unavailable: busy"},
				{Step: "put", Path: "code", Want: "<none>", Got: "unavailable"},
				{Step: "put_2", Path: "(error)", Got: "unavailable: busy"},
				{Step: "put_2", Path: "code", Want: "<none>", Got: "unavailable"},
			}}}
		}},
	}
}

func renderGateCase(t *testing.T, c gateCase) string {
	t.Helper()
	chains := c.chains()
	settleGate(chains)
	return captureStdout(t, func() {
		fmt.Printf("# %s\n", c.name)
		for _, g := range chains {
			if g.echoOf != "" {
				continue
			}
			fmt.Println(g.line(0))
			if c.verbose {
				g.printChanges(nil)
			}
		}
		printGateGroups(nil, chains, c.verbose)
	})
}

func TestTheGateSettlesEachChainsLead(t *testing.T) {
	const b = "a chain with a root the earlier chain lacks names that root, not the same fault"
	cases := map[string]gateCase{}
	for _, c := range gateCases() {
		cases[c.name] = c
	}
	settled := func(name string) []*gateChain {
		chains := cases[name].chains()
		settleGate(chains)
		return chains
	}
	sameAs := func(g *gateChain) string {
		if _, as, ok := strings.Cut(g.first, "; "+sameFault); ok {
			as, _, _ = strings.Cut(as, " (")
			return as
		}
		return ""
	}
	for _, c := range []struct {
		name          string
		firstAt, same []string
	}{
		{"a chain with a root the earlier chain lacks names that root, not the same fault", []string{"replay order.status", "replay_2 order.status", "create order.total_minor"}, []string{"", "", ""}},
		{"a failed first change leads over a drift", []string{"add status.code", "add_as_clerk status.code"}, []string{"", "a"}},
		{"-v shows the suspect's request and the same fault in a later chain", []string{"get thing.state", "get thing.state"}, []string{"", "one"}},
		{"a read unclear between two writes names both and is grouped once, under the earlier", []string{"get_b product.qty_on_hand", "get_b product.qty_on_hand", "get_b product.qty_on_hand"}, []string{"", "", "flow"}},
		{"an unclear row whose writes include an earlier row's suspect for the field is that fault, named with its rpc", []string{"get_b product.qty_on_hand", "get_b product.qty_on_hand"}, []string{"", "cancels"}},
		{"a fault a chain with a safe spot shows is anchored there, though a chain without one shows it first", []string{"create order.total_minor", "create order.total_minor", "create order.total_minor"}, []string{"orders", "", "orders"}},
		{"a chain with a fault the anchor's line does not name names it, though the anchor has it too", []string{"create order.total_minor", "create order.total_minor", "create order.total_minor"}, []string{"", "", "explore"}},
	} {
		chains := settled(c.name)
		for i, g := range chains {
			if g.firstAt != c.firstAt[i] || sameAs(g) != c.same[i] {
				t.Errorf("%s: %s leads with %q same as %q, want %q same as %q", c.name, g.name, g.firstAt, sameAs(g), c.firstAt[i], c.same[i])
			}
		}
	}
	if chains := settled(b); !strings.HasSuffix(chains[1].first, "; suspect the write; also suspect write create (OrderService/CreateOrder) at order.total_minor") {
		t.Errorf("%s: got %q", b, chains[1].first)
	}
	if chains := settled("a chain with a fault the anchor's line does not name names it, though the anchor has it too"); !strings.HasSuffix(chains[1].first, "; suspect the write; also suspect write stock_batch (StockService/AddStockBatch) at product.qty_on_hand") {
		t.Errorf("a fault the anchor's line leaves out is named on the line: got %q", chains[1].first)
	}
	if out := renderGateCase(t, cases["a fault a chain with a safe spot shows is anchored there, though a chain without one shows it first"]); !strings.Contains(out, "  OrderService/CreateOrder order.total_minor: 3 step(s) in 3 chain(s); e.g. orders create\n") {
		t.Errorf("the row's example is a chain with a safe spot:\n%s", out)
	}
	chains := settled("a slice failing as its parent folds into the parent's line")
	if got := []string{chains[1].echoOf, chains[2].echoOf, chains[3].echoOf, chains[4].echoOf, chains[5].echoOf, chains[6].echoOf}; strings.Join(got, ",") != "orders,orders,orders,,," {
		t.Errorf("a slice whose first change is its parent's folds, a kept-red one only when its pins held and its parent fails every way it does: %q", got)
	}
	if line := chains[0].line(6); !strings.HasSuffix(line, " (+1 slice(s) fail the same: orders-slice-list) (+2 kept-red slice(s), every pin held, pinned 2026-09-28, 2026-09-30: list ListOrders orders, stock GetProduct product.qty_on_hand)") {
		t.Errorf("a kept-red slice folds as its first pin, saying every pin held and when it was pinned: %s", line)
	}
	chains = settled("a moved pin with no suspect does not point above")
	if chains[1].class != "not as pinned" || sameAs(chains[1]) != "" {
		t.Errorf("a moved pin is not as pinned and names no other chain: %q %q", chains[1].class, chains[1].first)
	}
	for name, want := range map[string]string{
		"an unclear row whose writes include an earlier row's suspect for the field is that fault, named with its rpc": "  OrderService/CancelOrder product.qty_on_hand: 2 step(s) in 2 chain(s)",
		"-v shows the suspect's request and the same fault in a later chain":                                           "; same fault as one (Move)\n",
	} {
		if out := renderGateCase(t, cases[name]); !strings.Contains(out, want) {
			t.Errorf("%s: want %q in:\n%s", name, want, out)
		}
	}
	out := renderGateCase(t, cases["knock-on changes on the same record fold into the root write"])
	if strings.Count(out, "\n  ") != 1 || !strings.Contains(out, "StockService/AddStock status.code, qty_on_hand: 3 step(s)") {
		t.Errorf("knock-on changes on the same record fold into the root write:\n%s", out)
	}
	out = renderGateCase(t, cases["one line per suspect rpc, knock-ons folded under the write"])
	if strings.Count(out, "  S/List list:") != 1 || strings.Contains(out, "S/Get:") {
		t.Errorf("one line per suspect rpc, knock-on steps under the write:\n%s", out)
	}
}

func TestTheHeadlineLeadsWithTheFirstFailingChange(t *testing.T) {
	transport := &diff.Report{Changes: []diff.Change{
		{Step: "get", Path: "status", Kind: diff.KindStatus, Want: runner.StatusPassed, Got: runner.StatusError},
		{Step: "get", Path: "customer", Kind: diff.KindType, Want: nil, Got: map[string]any{}},
		{Step: "list", Path: "total", Kind: diff.KindChanged, Want: 1, Got: 2},
	}}
	if first, steps := firstChange(transport, nil); first == nil || first.Kind != diff.KindStatus || steps != 2 {
		t.Errorf("a transport error leads, got %+v over %d step(s)", first, steps)
	}
	transport.Changes[0].Got = runner.StatusFailed
	if first, _ := firstChange(transport, nil); first.Path != "customer" {
		t.Errorf("a plain failure yields to the step's first change, got %+v", first)
	}
	drift := &diff.Report{Changes: []diff.Change{
		{Step: "login", Path: "expires_at", Kind: diff.KindChanged, Want: 1, Got: 1000},
		{Step: "add", Path: "qty_on_hand", Kind: diff.KindChanged, Want: 0, Got: 9},
		{Step: "add", Path: "status.code", Kind: diff.KindStatus, Want: runner.StatusPassed, Got: runner.StatusFailed},
	}}
	rec := &runner.Record{Steps: []*runner.StepRecord{{ID: "login", Status: runner.StatusPassed}, {ID: "add", Status: runner.StatusFailed}}}
	if first, steps := firstChange(drift, rec); first == nil || first.Step != "add" || first.Path != "qty_on_hand" || steps != 2 {
		t.Errorf("the failing step leads over an earlier drift, got %+v over %d step(s)", first, steps)
	}
	if first, _ := firstChange(drift, nil); first.Step != "login" {
		t.Errorf("without a record the first change leads, got %+v", first)
	}
	observed := shopRecord(
		shopStep("create_order", shopOrder, `{"order":{"id_order":"o1","total_minor":"7"}}`),
		shopStep("fetch_order", shopFetch, `{"order":{"id_order":"o1","total_minor":"7"}}`, "create_order").failing("order.total_minor", "9", "7"),
	)
	write := &diff.Report{Changes: []diff.Change{
		{Step: "create_order", Path: "order.total_minor", Kind: diff.KindChanged, Want: "9", Got: "7"},
		{Step: "fetch_order", Path: "order.total_minor", Kind: diff.KindChanged, Want: "9", Got: "7"},
	}}
	if c, _ := firstChange(write, observed); c == nil || c.Step != "create_order" {
		t.Errorf("the changed write a failing read observes leads, got %+v", c)
	}
}
