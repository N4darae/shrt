package main

import (
	"context"
	"strings"
	"testing"
)

func sliceCreateOrder(t *testing.T, tag string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{".shrt/scratch/probe-orders.yaml", "-step", "create_order_single", "-run", "latest", "-verify", "-var", "tag=" + tag})
	})
	return out, err
}

func TestADroppedWriteTheContractsSayTheTargetNeverReadsDoesNotBlockTheReceipt(t *testing.T) {
	shop := newFakeShop()
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/scratch/probe-orders.yaml", cancelConfirmedChain)
	_ = runRun(context.Background(), []string{".shrt/scratch/probe-orders.yaml", "-quiet", "-var", "tag=src"})

	out, err := sliceCreateOrder(t, "s1")
	if exitCodeOf(err) != 3 || !strings.Contains(out, "INCONCLUSIVE") || !strings.Contains(out, "-keep add_stock") {
		t.Fatalf("with no contract nothing says CreateOrder ignores the stock add_stock sets on its line's product, so the match stays inconclusive (exit %d):\n%s", exitCodeOf(err), out)
	}

	writeFile(t, ".shrt/contracts/orders.yaml", `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records a pending order without touching stock
        required: [NONE]
        status: draft
`)
	out, err = sliceCreateOrder(t, "s2")
	if exitCodeOf(err) != 0 || !strings.Contains(out, "verify reproduced") {
		t.Fatalf("CreateOrder's messages carry no qty_on_hand and its contract neither needs StockService nor names stock in a failure, so add_stock is not evidence against the match (exit %d):\n%s", exitCodeOf(err), out)
	}
	if !strings.Contains(out, "info: dropped write step add_stock changes qty_on_hand") {
		t.Fatalf("the verdict must say why add_stock was not kept:\n%s", out)
	}

	writeFile(t, ".shrt/contracts/orders.yaml", `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records a pending order
        required: [NONE]
        needs: [shop.catalog.v1.StockService/AddStock]
        status: draft
`)
	out, err = sliceCreateOrder(t, "s3")
	if exitCodeOf(err) != 0 || !strings.Contains(out, "contract needs shop.catalog.v1.StockService/AddStock") || strings.Contains(out, "info: dropped write step add_stock") {
		t.Fatalf("a contract that says CreateOrder needs AddStock keeps add_stock in the slice (exit %d):\n%s", exitCodeOf(err), out)
	}
}
