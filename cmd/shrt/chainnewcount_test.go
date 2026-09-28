package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestChainNewCountsTheStepsItWrote(t *testing.T) {
	dir := shopWorkspace(t, shopConfig)
	writeFile(t, filepath.Join(dir, ".shrt", "contracts", "orders.yaml"), `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        required: [NONE]
        status: draft
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [lines]
        fields:
            lines.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
            lines.qty:
                value: "2"
        effects:
            total_minor: {sum: lines.qty, times: price_minor}
        status: draft
`)
	defer chdir(t, dir)()
	var err error
	out := captureStdout(t, func() { err = chainNew([]string{"-name", "order", "CreateProduct", "CreateOrder"}) })
	if err != nil {
		t.Fatalf("chain new: %v\n%s", err, out)
	}
	c, err := chain.LoadFile(filepath.Join(".shrt", "chains", "order.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Steps) < 3 || !strings.Contains(out, fmt.Sprintf("(%d step(s))", len(c.Steps))) {
		t.Fatalf("want the written count %d, with the steps it added:\n%s", len(c.Steps), out)
	}
}
