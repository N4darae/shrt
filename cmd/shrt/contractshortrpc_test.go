package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestContractPlanWritesShortRPCKeysAndAliases(t *testing.T) {
	dir := shopWorkspace(t, strings.ReplaceAll(shopConfig, "http://127.0.0.1:8080", "http://127.0.0.1:1"))
	t.Chdir(dir)
	path := filepath.Join(dir, ".shrt", "contracts", "catalog.yaml")
	doc := `apiVersion: shrt/contract/v1
domain: catalog
rpcs:
    ProductService/CreateProduct:
        summary: adds a product
        fields: {sku: {value: SKU-1}, price_minor: {value: "100"}}
        aliases:
            first:
                fields: {sku: {value: SKU-FIRST}}
        status: draft
    StockService/AddStock:
        summary: adds stock
        fields: {qty: {value: "5"}}
        aliases:
            first:
                fields: {id_product: {from: "ProductService/CreateProduct@first->product.id_product"}}
        status: draft
`
	writeFile(t, path, doc)
	var planErr error
	out := captureStdout(t, func() {
		planErr = contractPlan([]string{"StockService/AddStock@first", "-write"})
	})
	if planErr != nil {
		t.Fatalf("plan short-key alias without an explicit -name: %v\n%s", planErr, out)
	}
	c, err := chain.LoadFile(filepath.Join(dir, ".shrt", "chains", "catalog-addstock-first.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	stock, ok := c.Step("add_stock_first")
	if !ok || stock.Body["id_product"] != "${create_product_first.product.id_product}" || stock.Body["qty"] != "5" {
		t.Fatalf("the written chain must retain literals and producer wiring: %+v", stock)
	}
	product, ok := c.Step("create_product_first")
	if !ok || product.Body["sku"] != "SKU-FIRST" {
		t.Fatalf("the selected producer alias must be included: %+v", product)
	}
	if got := string(mustRead(t, path)); got != doc {
		t.Fatalf("planning must not rewrite the source contract:\n%s", got)
	}
}
