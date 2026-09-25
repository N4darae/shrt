package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitGuideUsesTheDetectedEnvelope(t *testing.T) {
	dir := shopWorkspace(t, "")
	restore := chdir(t, dir)
	defer restore()
	out := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	if !strings.Contains(out, "    envelope_path: status.code ") {
		t.Fatalf("init detected the verdict at status.code, so its conventions guide must show it:\n%s", out)
	}
	if !strings.Contains(out, "    item_envelope_path: results[].status.code ") {
		t.Fatalf("the item example must be built from the detected envelope:\n%s", out)
	}
	if strings.Contains(out, "envelope_path: error.code") || strings.Contains(out, "results[].error.code") {
		t.Fatalf("the guide must not show the default error.code next to a detected status.code:\n%s", out)
	}
}

func TestInitNextDoesNotTellYouToSetTheBaseURLYouJustGave(t *testing.T) {
	dir := shopWorkspace(t, "")
	restore := chdir(t, dir)
	defer restore()
	out := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false", "-base-url", "http://backend.test:9000"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	if strings.Contains(out, "set target.base_url") {
		t.Fatalf("-base-url was given, so next: must not tell the adopter to set it:\n%s", out)
	}
	if !strings.Contains(out, "check target.base_url") || !strings.Contains(out, "http://backend.test:9000") {
		t.Fatalf("next: must still name where the runs go, as a check:\n%s", out)
	}

	out = captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false", "-base-url", "http://other.test:1"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	if !strings.Contains(out, "-base-url http://other.test:1 was NOT applied") || !strings.Contains(out, "http://backend.test:9000") {
		t.Fatalf("a -base-url ignored because the config already exists must be said so:\n%s", out)
	}
}

func TestInitNextStillSaysToSetTheBaseURLWhenNotGiven(t *testing.T) {
	dir := shopWorkspace(t, "")
	restore := chdir(t, dir)
	defer restore()
	out := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	if !strings.Contains(out, "set target.base_url") || !strings.Contains(out, "http://127.0.0.1:8080 now") {
		t.Fatalf("without -base-url the default is in force and next: must say to set it:\n%s", out)
	}
}

const cancelFlowContracts = `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [NONE]
        status: draft
    shop.orders.v1.OrderService/ConfirmOrder:
        summary: confirms an order
        required: [id_order]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/CreateOrder->order.id_order
        status: draft
    shop.orders.v1.OrderService/FetchOrder:
        summary: reads an order
        required: [id_order]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/ConfirmOrder->order.id_order
        status: draft
    shop.orders.v1.OrderService/CancelOrder:
        summary: cancels an order
        required: [id_order]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/CreateOrder->order.id_order
        aliases:
            confirmed:
                fields:
                    id_order:
                        from: shop.orders.v1.OrderService/ConfirmOrder->order.id_order
        status: draft
`

func TestContractPlanTakesAnAliasAndSeveralTargets(t *testing.T) {
	dir := shopWorkspace(t, shopConfig)
	writeFile(t, filepath.Join(dir, ".shrt", "contracts", "orders.yaml"), cancelFlowContracts)
	restore := chdir(t, dir)
	defer restore()

	var err error
	planned := func(args ...string) string {
		t.Helper()
		out := captureStdout(t, func() { err = contractPlan(append(args, "-write", "-force")) })
		if err != nil {
			t.Fatalf("plan %v: %v", args, err)
		}
		path := filepath.Join(dir, ".shrt", "chains", strings.Fields(strings.TrimPrefix(out, "wrote .shrt/chains/"))[0])
		raw, rerr := os.ReadFile(strings.TrimSuffix(path, ":"))
		if rerr != nil {
			t.Fatalf("plan %v wrote no chain: %v\n%s", args, rerr, out)
		}
		return out + string(raw)
	}
	out := planned("CancelOrder@confirmed")
	if !strings.Contains(out, "order CreateOrder -> ConfirmOrder -> CancelOrder@confirmed") ||
		!strings.Contains(out, "name: orders-cancelorder-confirmed") ||
		!strings.Contains(out, "id_order: ${confirm_order.order.id_order}") {
		t.Fatalf("the aliased target must be planned with its overrides:\n%s", out)
	}

	out = planned("ConfirmOrder", "FetchOrder", "CancelOrder@confirmed")
	if !strings.Contains(out, "order CreateOrder -> ConfirmOrder -> FetchOrder -> CancelOrder@confirmed\n") {
		t.Fatalf("several targets must compose one deduplicated chain in dependency order:\n%s", out)
	}
	if !strings.Contains(out, "name: orders-confirmorder-fetchorder-cancelorder-confirmed") {
		t.Fatalf("the default name must name every target:\n%s", out)
	}
	out = captureStdout(t, func() { err = contractPlan([]string{"CancelOrder@confirmed"}) })
	if err != nil || !strings.Contains(out, "order: CreateOrder -> ConfirmOrder -> CancelOrder@confirmed\n") ||
		!strings.Contains(out, " steps: 2 setup, 1 target") || strings.Contains(out, "create_order,") || strings.Contains(out, "apiVersion:") {
		t.Fatalf("without -write the plan prints its order and step count per group, not the ids or the chain: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = contractPlan([]string{"CancelOrder@confirmed", "-v"}) })
	if err != nil || !strings.Contains(out, "step ids: create_order, confirm_order, ") {
		t.Fatalf("-v prints every step id: %v\n%s", err, out)
	}
}
