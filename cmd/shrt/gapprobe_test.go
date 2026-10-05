package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func gapShop(t *testing.T, refuse func(state string, lines int) bool) {
	t.Helper()
	shop, names, created := newFakeShop(), map[string]any{}, map[string]string{}
	shop.stockInProduct = true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		shop.mu.Lock()
		order := shop.orders[fmt.Sprint(body["id_order"])]
		state, lines := shop.states[fmt.Sprint(body["id_order"])], []any{}
		if order != nil {
			lines, _ = order["lines"].([]any)
		}
		shop.mu.Unlock()
		code, out := 200, map[string]any{"status": rejected("order is already cancelled"), "order": order}
		switch {
		case strings.HasSuffix(r.URL.Path, "/ConfirmOrder") && order != nil && state != "PENDING":
			out["status"] = rejected("order is not pending")
		case !strings.HasSuffix(r.URL.Path, "/CancelOrder") || !refuse(state, len(lines)):
			code, out = shop.handle(r.URL.Path, body)
		}
		shop.mu.Lock()
		defer shop.mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/CancelOrder") && state == "CONFIRMED" && shop.states[fmt.Sprint(body["id_order"])] == "CANCELLED" {
			for _, l := range lines {
				line, _ := l.(map[string]any)
				shop.stock[fmt.Sprint(line["id_product"])] += num64(line["qty"])
			}
		}
		if p, ok := out["product"].(map[string]any); ok {
			if strings.HasSuffix(r.URL.Path, "/CreateProduct") {
				names[fmt.Sprint(p["id_product"])] = body["name"]
				created[fmt.Sprint(p["id_product"])] = fmt.Sprint(time.Now().Unix())
			}
			p["name"], p["created_at"] = names[fmt.Sprint(p["id_product"])], created[fmt.Sprint(p["id_product"])]
		}
		if o, ok := out["order"].(map[string]any); ok {
			o["status"] = "ORDER_STATUS_" + shop.states[fmt.Sprint(o["id_order"])]
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	writeFile(t, ".shrt/chains/cancel-confirmed.yaml", strings.NewReplacer("a@example.test", "a-${vars.tag}@example.test", "S-1", "S-${vars.tag}",
		"    - id: create_order\n", "    - id: add_stock\n      call: shop.catalog.v1.StockService/AddStock\n      body:\n          id_product: ${create_product.product.id_product}\n          qty: \"5\"\n    - id: create_order\n").Replace(confirmedCancelOnly))
	writeFile(t, ".shrt/config.yaml", strings.Replace(shopConfig, "http://127.0.0.1:8080", srv.URL, 1)+"conventions:\n    envelope_path: status.code\n    envelope_ok: SUCCESS\n")
	inProcessGate(t)
}

func TestGateReproPlansEachGapIntoScratchAndRowsWhatFailsThere(t *testing.T) {
	defer stateGapWorkspace(t)()
	gapShop(t, func(state string, lines int) bool { return state == "PENDING" && lines >= 3 })
	out, code := runGateOut(t, "-repro")
	pending := "  CancelOrder on a PENDING order: no chain calls it so; its plan sends 1, 2 or 3 lines\n" +
		"    CancelOrder status.code, order.status: 2 steps in 1 chain, e.g. orderservice-cancelorder-gaps cancel_order_3_lines_from_pending\n" +
		"      status.code want=SUCCESS got=REJECTED\n" +
		"    trigger: fails with 3+ lines (2 calls: 3, 4 lines); passes with up to 2 lines (3 calls: 1, 2 lines)\n" +
		"    repro: shrt run .shrt/scratch/orderservice-cancelorder-gaps-slice-cancel_order_3_lines_from_pending.yaml (6 of 62 steps, reproduced 3/3)\n"
	if code != 0 || !strings.Contains(out, "gaps: 3 states no chain calls a gated write from, 3 probed against the contract:\n") || !strings.Contains(out, pending) ||
		!strings.Contains(out, "  CancelOrder on a CONFIRMED order: no chain sends 2 or 3 lines (cancel-confirmed sends 1)\n    passes: ") || strings.Count(out, "\n    passes: ") != 2 ||
		!strings.HasSuffix(out, "gate: PASS: 1 chain, 1 gap probe failed\n") {
		t.Fatalf("the gate plans each gap into .shrt/scratch/, runs it, and rows the 3-line PENDING cancel it refuses with a trigger and a verified repro, got %d:\n%s", code, out)
	}
	slice, err := os.ReadFile(".shrt/scratch/orderservice-cancelorder-gaps-slice-cancel_order_3_lines_from_pending.yaml")
	if err != nil || strings.Contains(string(slice), "id: cancel_order_1_lines\n") || strings.Contains(string(slice), "id: confirm_order") ||
		strings.Contains(string(slice), "call: shop.catalog.v1.StockService/AddStock") || strings.Contains(string(slice), "id: create_order_1_lines") ||
		!strings.Contains(string(slice), "cancel_order_3_lines_from_pending failed the same way in a run without each: add_stock_for_items, ") {
		t.Fatalf("the repro keeps the gap's own call and what it reads, no cancel or confirm of another state, and no stock or order the cancel does without (%v):\n%s", err, slice)
	}
	if chains, _ := os.ReadDir(".shrt/chains"); len(chains) != 1 {
		t.Fatalf("the probe writes nothing into the chains directory: %v", chains)
	}
}

func TestGateReproSaysTheCallAroundAGapsBoundaryContradictsItsSplit(t *testing.T) {
	defer stateGapWorkspace(t)()
	gapShop(t, func(state string, lines int) bool { return state == "PENDING" && lines == 3 })
	out, _ := runGateOut(t, "-repro")
	if !strings.Contains(out, "e.g. orderservice-cancelorder-gaps cancel_order_3_lines_from_pending\n      status.code want=SUCCESS got=REJECTED\n"+
		"    trigger: none: sent again with 4 lines, the call passed\n    repro: ") || strings.Count(out, "trigger:") != 1 {
		t.Fatalf("a 4-line PENDING cancel that passes contradicts 3+ lines, so the row says so in place of a trigger:\n%s", out)
	}
}

func TestGateReproSaysAGapPassesAndProbesOnlyAsManyAsItsCap(t *testing.T) {
	defer stateGapWorkspace(t)()
	gapShop(t, func(string, int) bool { return false })
	saved := gapProbes
	t.Cleanup(func() { gapProbes = saved })
	gapProbes = 1
	out, code := runGateOut(t, "-repro")
	if code != 0 || !strings.Contains(out, "gaps: 3 states no chain calls a gated write from, 1 probed against the contract:\n") ||
		!strings.Contains(out, "(cancel-confirmed sends 1)\n    passes: 5 calls from that state (shrt run .shrt/scratch/orderservice-cancelorder-gaps.yaml)\n") ||
		!strings.Contains(out, "its plan sends 1, 2 or 3 lines\n    not probed: shrt contract plan CancelOrder -write orderservice-cancelorder-gaps.yaml (into .shrt/scratch/)\n") ||
		strings.Count(out, "    not probed: ") != 2 || !strings.HasSuffix(out, "gate: PASS: 1 chain\n") {
		t.Fatalf("a gap whose calls pass says so, and the gaps past the cap name the command that plans them into .shrt/scratch/, got %d:\n%s", code, out)
	}
}

func TestContractPlanRefusesToOverwriteAChainWithASafeSpotOrAKeptRedSlice(t *testing.T) {
	defer stateGapWorkspace(t)()
	var err error
	plan := func(args ...string) string {
		t.Helper()
		return captureStdout(t, func() { err = contractPlan(append([]string{"CancelOrder"}, args...)) })
	}
	if plan("-write"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/safespots/orders-cancelorder.json", "{}\n")
	writeFile(t, ".shrt/chains/orders-cancelorder-slice-get_product.yaml", "apiVersion: shrt/v1\nname: orders-cancelorder-slice-get_product\n"+
		"description: 'Slice of orders-cancelorder reproducing step get_product: 3 of 9 steps.'\nsteps:\n    - id: get_product\n"+
		"      call: shop.catalog.v1.ProductService/GetProduct\n      expect:\n          - path: product.qty_on_hand\n            equals: \"10\"\n"+
		"kept_red:\n    - step: get_product\n      path: product.qty_on_hand\n      got: \"8\"\n")
	before := string(mustRead(t, ".shrt/chains/orders-cancelorder.yaml"))
	writeFile(t, ".shrt/chains/orders-cancelorder.yaml", before+"# approved\n")
	plan("-write", "-force")
	if err == nil || !strings.Contains(err.Error(), ".shrt/chains/orders-cancelorder.yaml has the approved safe spot .shrt/safespots/orders-cancelorder.json and the kept-red slice orders-cancelorder-slice-get_product: ") ||
		!strings.Contains(err.Error(), "Write the plan beside it: shrt contract plan CancelOrder -write orders-cancelorder-plan.yaml (into .shrt/scratch/)") ||
		!strings.HasSuffix(string(mustRead(t, ".shrt/chains/orders-cancelorder.yaml")), "# approved\n") {
		t.Fatalf("-force refuses to overwrite a chain with a safe spot and names it: %v", err)
	}
	if out := plan("-write", "orders-cancelorder-plan.yaml"); err != nil || !strings.Contains(out, "wrote .shrt/scratch/orders-cancelorder-plan.yaml: ") ||
		!strings.Contains(string(mustRead(t, ".shrt/scratch/orders-cancelorder-plan.yaml")), "name: orders-cancelorder-plan\n") {
		t.Fatalf("-write <file>.yaml writes the plan to .shrt/scratch/, named after the file (%v):\n%s", err, out)
	}
	out := captureStdout(t, func() { err = contractPlan([]string{"-all", "-write", "-force"}) })
	if err != nil || !strings.Contains(out, "orders-cancelorder: ") || !strings.Contains(out, ", kept: it has the approved safe spot") ||
		!strings.HasSuffix(string(mustRead(t, ".shrt/chains/orders-cancelorder.yaml")), "# approved\n") {
		t.Fatalf("-all -force keeps a chain with a safe spot (%v):\n%s", err, out)
	}
	if plan("-write", "-force", "-force-approved"); err != nil || strings.HasSuffix(string(mustRead(t, ".shrt/chains/orders-cancelorder.yaml")), "# approved\n") {
		t.Fatalf("-force-approved overwrites it once the user said so: %v", err)
	}
}
