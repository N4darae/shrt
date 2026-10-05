package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
)

const shelfChain = `apiVersion: shrt/v1
name: shelf
volatile:
    - '**.sku'
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: shelf-${vars.tag}-a
        price_minor: "250"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "5"
      expect:
        - path: qty_on_hand
          equals: "5"
    - id: get_product
      call: ProductService/GetProduct
      body:
        id_product: ${create_product.product.id_product}
      expect:
        - path: product.qty_on_hand
          equals: "5"
`

func TestGoldenGateRepro(t *testing.T) {
	shop := newFakeShop()
	shop.stockInProduct = true
	chdirToFakeShop(t, shop)
	inProcessGate(t)
	writeFile(t, ".shrt/chains/shelf.yaml", shelfChain)
	for _, args := range [][]string{{"run", "shelf", "-var", "tag=tfirst001"}, {"confirm", "shelf", "-note", "shelf"}, {"confirm", "shelf", "-approve", "-by", "alice@example.test"}} {
		if out, code := shrtOut(t, args[0], args[1:]...); code != 0 {
			t.Fatalf("%v: exit %d\n%s", args, code, out)
		}
	}
	var golden strings.Builder
	for _, c := range []struct {
		name        string
		lost, stale bool
	}{
		{"AddStock answers 5 and stores nothing: ListProducts reads 0 too, so the write", true, false},
		{"GetProduct reads one short: ListProducts reads the 5 AddStock answered, so the read", false, true},
	} {
		shop.addStockLostBug, shop.stockReadBug = c.lost, c.stale
		out, code := shrtOut(t, "gate", "-repro", "-no-session-check", "-hollow-baseline", "")
		fmt.Fprintf(&golden, "# %s\n$ shrt gate -repro  [exit %d]\n%s\n", c.name, code, out)
		want := map[bool]string{true: "settled on the write add_stock: ", false: "settled on the read get_product: "}[c.lost]
		if code != 1 || !strings.Contains(out, want) || !strings.Contains(out, "repro: shrt run .shrt/scratch/shelf-slice-get_product.yaml -keep-going (3 of 3 steps, reproduced 3/3)") || strings.Contains(out, "unclear") {
			t.Errorf("%s: the gate settles the unclear row with ListProducts and verifies a one-line repro, got %d:\n%s", c.name, code, out)
		}
		run, _ := shrtOut(t, "run", ".shrt/scratch/shelf-slice-get_product.yaml", "-keep-going")
		fmt.Fprintf(&golden, "$ shrt run .shrt/scratch/shelf-slice-get_product.yaml -keep-going\n%s\n", run)
		if strings.Contains(run, "unclear") {
			t.Errorf("%s: the repro keeps the read that settled the row, so run names one suspect:\n%s", c.name, run)
		}
		if !strings.Contains(out, "masks: none of 5 masked values") {
			t.Errorf("%s: a sku differing by the run tag under a volatile path is no mask that hid a change:\n%s", c.name, out)
		}
	}
	checkGolden(t, "gate-repro.txt", golden.String())
}

const tillChain = `apiVersion: shrt/v1
name: till
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: till-${vars.tag}
        price_minor: "250"
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "5"
    - id: create_order
      call: OrderService/CreateOrder
      body:
        idempotency_key: ${uuid}
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "2"
    - id: confirm
      call: OrderService/ConfirmOrder
      body:
        id_order: ${create_order.order.id_order}
      expect:
        - path: order.total_minor
          equals: "500"
    - id: fetch
      call: OrderService/FetchOrder
      body:
        id_order: ${create_order.order.id_order}
      expect:
        - path: order.total_minor
          equals: "500"
`

func TestAWriteAnsweredOtherThanStoredKeepsItsReadBackInTheRepro(t *testing.T) {
	shop := newFakeShop()
	chdirToFakeShop(t, shop)
	inProcessGate(t)
	writeFile(t, ".shrt/chains/till.yaml", tillChain)
	for _, args := range [][]string{{"run", "till", "-var", "tag=tfirst001"}, {"confirm", "till", "-note", "till"}, {"confirm", "till", "-approve", "-by", "alice@example.test"}} {
		if out, code := shrtOut(t, args[0], args[1:]...); code != 0 {
			t.Fatalf("%v: exit %d\n%s", args, code, out)
		}
	}
	shop.confirmTotalBug = true
	out, _ := shrtOut(t, "gate", "-repro", "-no-session-check", "-hollow-baseline", "")
	want := "repro: shrt run .shrt/scratch/till-slice-confirm.yaml -keep-going (5 of 5 steps, reproduced 3/3)"
	if !strings.Contains(out, "answered order.total_minor=0, but FetchOrder read 500") || !strings.Contains(out, want) {
		t.Fatalf("the repro of a write the read-back contradicts keeps that read:\n%s", out)
	}
	slice := string(mustRead(t, ".shrt/scratch/till-slice-confirm.yaml"))
	if !strings.Contains(slice, "- id: fetch") || !strings.Contains(slice, "Then fetch reads order.total_minor back and expects what confirm answered, 0") {
		t.Fatalf("the slice ends with the read-back expecting what the write answered:\n%s", slice)
	}
	if out, code := shrtOut(t, "run", ".shrt/scratch/till-slice-confirm.yaml", "-keep-going"); code != 1 || !strings.Contains(out, "FAIL order.total_minor want=0 (as confirm answered) got=500") {
		t.Fatalf("run with -keep-going, the slice shows the stored value beside the answer, got %d:\n%s", code, out)
	}
}

func TestARunLineSaysWhereAReferencedWantCameFrom(t *testing.T) {
	c := &chain.Chain{Steps: []*chain.Step{{ID: "fetch", Expect: []chain.Expectation{
		{Path: "order.status", Equals: "${vars.confirm_answered}"}, {Path: "order.id_order", Equals: "o-${vars.tag}"}, {Path: "order.total_minor", Equals: "500"}}}}}
	for path, want := range map[string]string{"order.status": "as confirm answered", "order.id_order": "", "order.total_minor": "", "order.lines": ""} {
		if got := wantRef(c, "fetch", path); got != want {
			t.Errorf("%s: got %q, want %q", path, got, want)
		}
	}
	if got := wantRef(c, "gone", "order.status"); got != "" {
		t.Errorf("a step the chain does not have names no reference: %q", got)
	}
}

func TestTheGateListsAMaskedValueThatDifferedBeyondTagsIdsAndTimestamps(t *testing.T) {
	report := func(changes ...diff.Change) gateOutcome {
		raw, _ := json.Marshal(map[string]any{"run": map[string]any{"vars": map[string]any{"tag": "tnew00001"}}, "diff": diff.Report{VolatileValues: changes}})
		return gateOutcome{stdout: string(raw)}
	}
	gateWorkspace(t, map[string][]gateOutcome{"verify cli-thing-flow": {report(
		diff.Change{Step: "make", Path: "thing.created_at", Kind: diff.KindChanged, Want: "2026-09-28T18:38:33Z", Got: "2026-10-04T17:50:13Z"},
		diff.Change{Step: "make", Path: "thing.name", Kind: diff.KindChanged, Want: "n-told0001-x", Got: "n-tnew00001-x"},
		diff.Change{Step: "make", Path: "thing.total", Kind: diff.KindChanged, Want: "600", Got: "400"},
		diff.Change{Step: "list_all", Path: "things.3.name", Kind: diff.KindChanged, Want: "a", Got: "b", Mask: "things"},
		diff.Change{Step: "list_all", Path: "things", Kind: diff.KindLength, Want: 5, Got: 9, Mask: "things"},
	)}})
	out := gateMasks(context.Background(), []*gateChain{{name: "cli-thing-flow", spot: true}, {name: "cli-unique"}})
	if out != "masks: 1 masked value differs beyond run tags, ids, timestamps:\n"+
		"  cli-thing-flow make thing.total (600 -> 400)\n  2 in whole volatile lists, not compared: cli-thing-flow list_all" {
		t.Fatalf("a timestamp and a run tag are what a mask is for, a total is not, and an unscoped list is counted apart:\n%s", out)
	}
}

const restockChain = `apiVersion: shrt/v1
name: %s
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: %s-${vars.tag}
        price_minor: "250"
    - id: stock_batch
      call: StockService/AddStockBatch
      body:
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "2"
            - id_product: ${create_product.product.id_product}
              qty: "3"
      expect:
        - path: results.1.qty_on_hand
          equals: "5"
%s    - id: create_order
      call: OrderService/CreateOrder
      body:
        idempotency_key: ${uuid}
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "2"
    - id: confirm
      call: OrderService/ConfirmOrder
      body:
        id_order: ${create_order.order.id_order}
    - id: get_product
      call: ProductService/GetProduct
      body:
        id_product: ${%s.id_product}
      expect:
        - path: product.qty_on_hand
          equals: "3"
`

func TestGateReproSettlesAnUnclearPairOfWritesOnACounter(t *testing.T) {
	shop := newFakeShop()
	shop.stockInProduct = true
	chdirToFakeShop(t, shop)
	inProcessGate(t)
	writeFile(t, ".shrt/contracts/shop.yaml", stockEffects)
	mid := "    - id: get_mid\n      call: ProductService/GetProduct\n      body:\n        id_product: ${create_product.product.id_product}\n"
	for name, c := range map[string][2]string{"restock": {"", "create_product.product"}, "restock-mid": {mid, "create_product.product"}, "restock-late": {"", "create_order.order.lines.0"}} {
		writeFile(t, ".shrt/chains/"+name+".yaml", fmt.Sprintf(restockChain, name, name, c[0], c[1]))
		for _, args := range [][]string{{"run", name, "-var", "tag=tfirst001"}, {"confirm", name, "-note", name}, {"confirm", name, "-approve", "-by", "alice@example.test"}} {
			if out, code := shrtOut(t, args[0], args[1:]...); code != 0 && c[0] == "" {
				t.Fatalf("%v: exit %d\n%s", args, code, out)
			}
			if c[0] != "" {
				break
			}
		}
	}
	for _, c := range []struct {
		name          string
		batch, extra  bool
		want, without []string
	}{
		{"the batch stores only its first line: a read right after it tells, and the confirm took 2 as approved", true, false, []string{
			"settled on the write stock_batch in restock: GetProduct read qty_on_hand=2 after it where it answered 5\n",
			"settled on the write stock_batch in restock-mid: get_mid read qty_on_hand=2 after it where it answered 5\n",
		}, []string{"in restock-late"}},
		{"the confirm takes one more: the batch stored what it answered", false, true, []string{
			"settled on the write confirm in restock: GetProduct read qty_on_hand=5 after stock_batch, as it answered; confirm fell 3 (approved: fell 2)\n",
			"settled on the write confirm in restock-mid: get_mid read qty_on_hand=5 after stock_batch, as it answered; confirm fell 3 (expected: fell 2)\n",
		}, []string{"in restock-late"}},
		{"both: the reads cannot tell, so it stays unclear", true, true, []string{
			"not settled: stock_batch or confirm in restock: GetProduct read qty_on_hand=2 after stock_batch where it answered 5; confirm fell 3 (approved: fell 2)\n",
		}, []string{"in restock-late"}},
	} {
		shop.batchFirstLineBug, shop.confirmExtraUnit = c.batch, c.extra
		out, code := shrtOut(t, "gate", "-repro", "-no-session-check", "-hollow-baseline", "")
		_, block, _ := strings.Cut(out, "failing step")
		_, block, _ = strings.Cut(block, "\n")
		for _, want := range c.want {
			if code != 1 || !strings.Contains(block, want) {
				t.Errorf("%s: want %q, got %d:\n%s", c.name, want, code, out)
			}
		}
		for _, not := range c.without {
			if strings.Contains(block, not) {
				t.Errorf("%s: no settle line %q:\n%s", c.name, not, out)
			}
		}
	}
}

const shelfCheckChain = `apiVersion: shrt/v1
name: shelfcheck
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: shelf-${vars.tag}
        price_minor: "900"
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "3"
      expect:
        - path: qty_on_hand
          equals: "3"
    - id: list
      call: ProductService/ListProducts
      body:
        sku_prefix: shelf-${vars.tag}
      expect:
        - path: products.0.qty_on_hand
          equals: "3"
    - id: create_order
      call: OrderService/CreateOrder
      body:
        idempotency_key: ${uuid}
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "3"
    - id: confirm
      call: OrderService/ConfirmOrder
      body:
        id_order: ${create_order.order.id_order}
    - id: read_back
      call: ProductService/GetProduct
      body:
        id_product: ${create_product.product.id_product}
      expect:
        - path: product.qty_on_hand
          equals: "0"
`

func TestARunOfTheGatesReproNamesTheSuspectTheGateRowNamed(t *testing.T) {
	shop := newFakeShop()
	shop.stockInProduct = true
	chdirToFakeShop(t, shop)
	inProcessGate(t)
	writeFile(t, ".shrt/contracts/shop.yaml", stockEffects)
	writeFile(t, ".shrt/chains/shelfcheck.yaml", shelfCheckChain)
	for _, args := range [][]string{{"run", "shelfcheck", "-var", "tag=tfirst001"}, {"confirm", "shelfcheck", "-note", "shelf"}, {"confirm", "shelfcheck", "-approve", "-by", "alice@example.test"}} {
		if out, code := shrtOut(t, args[0], args[1:]...); code != 0 {
			t.Fatalf("%v: exit %d\n%s", args, code, out)
		}
	}
	shop.confirmExtraUnit = true
	out, _ := shrtOut(t, "gate", "-repro", "-no-session-check", "-hollow-baseline", "")
	if !strings.Contains(out, "suspect write confirm (ConfirmOrder)") || !strings.Contains(out, "repro: shrt run .shrt/scratch/shelfcheck-slice-read_back.yaml (") {
		t.Fatalf("the list read 3 after add_stock, so the gate names the confirm:\n%s", out)
	}
	if slice := string(mustRead(t, ".shrt/scratch/shelfcheck-slice-read_back.yaml")); !strings.Contains(slice, "- id: list\n") {
		t.Fatalf("the repro keeps the read that cleared add_stock, which the read-back does not need to fail:\n%s", slice)
	}
	if out, _ := shrtOut(t, "run", ".shrt/scratch/shelfcheck-slice-read_back.yaml", "-repeat", "3"); !strings.Contains(out, "suspect write confirm (ConfirmOrder)") || strings.Contains(out, "unclear") {
		t.Fatalf("run on the gate's repro names the gate row's suspect, not add_stock or confirm:\n%s", out)
	}
}

const stockroomChain = `apiVersion: shrt/v1
name: stockroom
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: room-${vars.tag}
        price_minor: "250"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: create_order
      call: OrderService/CreateOrder
      body:
        idempotency_key: room-${vars.tag}
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "5"
    - id: confirm_short
      call: OrderService/ConfirmOrder
      body:
        id_order: ${create_order.order.id_order}
      expect:
        - path: status.code
          equals: REJECTED
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "10"
    - id: get_product
      call: ProductService/GetProduct
      body:
        id_product: ${create_product.product.id_product}
      expect:
        - path: product.qty_on_hand
          equals: "10"
`

func TestSliceMinimizeDropsWhatTheStepFailsWithoutAndKeepsWhatItReads(t *testing.T) {
	shop := newFakeShop()
	shop.stockInProduct = true
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/chains/stockroom.yaml", stockroomChain)
	for _, args := range [][]string{{"run", "stockroom", "-var", "tag=tfirst001"}, {"confirm", "stockroom", "-note", "room"}, {"confirm", "stockroom", "-approve", "-by", "alice@example.test"}} {
		if out, code := shrtOut(t, args[0], args[1:]...); code != 0 {
			t.Fatalf("%v: exit %d\n%s", args, code, out)
		}
	}
	shop.stockReadBug = true
	slcRunAny("stockroom", "-var", "tag=tsecond01")
	saved := minimizeRuns
	t.Cleanup(func() { minimizeRuns = saved })
	for _, c := range []struct {
		runs      int
		tag, more string
	}{{1, "tcapped01", "Kept untried, past the -minimize cap of runs: add_stock.\n"}, {8, "tfull0001", ""}} {
		minimizeRuns = c.runs
		out, code := slcSlice(t, false, "stockroom", "-step", "get_product", "-minimize", "-var", "tag="+c.tag, "-write", "room-min")
		sl, ids := slcSteps(t, ".shrt/scratch/room-min.yaml")
		if code != 0 || ids != "create_product,add_stock,get_product" || !strings.Contains(out, "kept 3 of 5 steps, dropped 2 (create_order by -minimize)") ||
			!regexp.MustCompile(`1 dropped write step\(s\) refused in run \S+: confirm_short\.\n`).MatchString(sl.Description) ||
			!strings.Contains(sl.Description, "Dropped by -minimize, as get_product failed the same way in a run without each: create_order.\n") ||
			strings.Contains(sl.Description, "Kept untried") != (c.more != "") || !strings.Contains(sl.Description, c.more) {
			t.Fatalf("cap %d: the read product stays, the write refused in both runs goes unrun, the order goes after a run without it, and the stock stays "+
				"since the read differs without it, or is left untried past the cap, got %d:\n%s\n%s", c.runs, code, out, sl.Description)
		}
	}
}

func TestGateReproFirmsUpARowsTriggerThatRestsOnOneCall(t *testing.T) {
	order := func(n int) string {
		return fmt.Sprintf("    - id: order_%d\n      call: OrderService/CreateOrder\n      body:\n        idempotency_key: sizes-%d-${vars.tag}\n        lines:\n%s"+
			"      expect:\n        - path: status.code\n          equals: SUCCESS\n", n, n, strings.Repeat("            - id_product: ${create_product.product.id_product}\n              qty: \"1\"\n", n))
	}
	for _, c := range []struct {
		refuse func(int) bool
		want   string
	}{
		{func(n int) bool { return n >= 3 }, "    trigger: fails with 3+ lines (2 calls: 3, 4 lines); passes with up to 2 lines (3 calls: 1, 2 lines) [firmed by -repro]\n    repro: "},
		{func(n int) bool { return n == 3 }, "    trigger: none: sent again with 4 lines, the call passed [firmed by -repro]\n    repro: "},
	} {
		shop := newFakeShop()
		chdirToFakeShop(t, shop)
		inProcessGate(t)
		writeFile(t, ".shrt/chains/sizes.yaml", "apiVersion: shrt/v1\nname: sizes\nsteps:\n    - id: create_product\n      call: ProductService/CreateProduct\n"+
			"      body:\n        sku: sizes-${vars.tag}\n        price_minor: \"250\"\n"+order(1)+order(2)+order(3))
		for _, args := range [][]string{{"run", "sizes", "-var", "tag=tfirst001"}, {"confirm", "sizes", "-note", "sizes"}, {"confirm", "sizes", "-approve", "-by", "alice@example.test"}} {
			if out, code := shrtOut(t, args[0], args[1:]...); code != 0 {
				t.Fatalf("%v: exit %d\n%s", args, code, out)
			}
		}
		shop.refuseOrder = c.refuse
		out, _ := shrtOut(t, "gate", "-repro", "-no-session-check", "-hollow-baseline", "")
		if strings.Contains(out, "    trigger: fails with 3+ lines (1 call); passes with up to 2 lines (2 calls: 1, 2 lines)\n") || !strings.Contains(out, c.want) {
			t.Fatalf("under -repro a 3-line order refused beside a 1- and 2-line one is sent with 4 and 2 lines, and the row's trigger says what they showed:\n%s", out)
		}
	}
}
