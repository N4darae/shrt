package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestContractQualityRefusesALibraryThatDoesNotLint(t *testing.T) {
	defer shopStatusWorkspace(t)()
	writeFile(t, filepath.Join(".shrt", "contracts", "orders.yaml"), `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [NONE]
        status: draft
    shop.orders.v1.OrderService/ConfirmOrder:
        summary: confirms an order
        required: [NONE]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/CreateOrder->order.no_such_field
        status: draft
`)
	var lintErr error
	captureStdout(t, func() { lintErr = contractLint(nil) })
	if lintErr == nil || !strings.Contains(lintErr.Error(), "contract error(s)") {
		t.Fatalf("precondition: the from: names a field CreateOrder's response lacks, a contract error: %v", lintErr)
	}
	for _, args := range [][]string{nil, {"-json"}} {
		var err error
		out := captureStdout(t, func() { err = contractQuality(args) })
		if strings.Contains(out, "no rpc in this library has a measurable gap") {
			t.Fatalf("quality %v: contract lint reports an error, so the library has a gap quality must not deny:\n%s", args, out)
		}
		if err == nil || !strings.Contains(err.Error(), "contract error") {
			t.Fatalf("quality %v: refuses to score a library with contract errors, naming them: %v\n%s", args, err, out)
		}
	}
}
