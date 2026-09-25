package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContractPlanAllPlansOneChainPerRPCWithAContract(t *testing.T) {
	dir := shopWorkspace(t, shopConfig)
	restore := chdir(t, dir)
	defer restore()
	writeFile(t, filepath.Join(dir, ".shrt", "contracts", "shop.yaml"), `apiVersion: shrt/contract/v1
domain: shop
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        status: draft
    shop.catalog.v1.ProductService/GetProduct:
        summary: reads a product
        status: draft
    shop.orders.v1.OrderService/WatchOrder:
        summary: streams an order
        status: draft
`)
	var err error
	out := captureStdout(t, func() { err = contractPlan([]string{"-all"}) })
	if err != nil {
		t.Fatalf("plan -all: %v\n%s", err, out)
	}
	for _, want := range []string{"catalog-createproduct: ", "catalog-getproduct: ", "WatchOrder: streaming, not planned"} {
		if !strings.Contains(out, want) {
			t.Fatalf("one line per rpc with a contract, want %q:\n%s", want, out)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, ".shrt", "chains")); len(entries) != 0 {
		t.Fatalf("without -write nothing is written, found %d file(s)", len(entries))
	}
	out = captureStdout(t, func() { err = contractPlan([]string{"-all", "-write"}) })
	if err != nil || strings.Count(out, ", written") != 2 {
		t.Fatalf("plan -all -write writes each chain: %v\n%s", err, out)
	}
	path := filepath.Join(dir, ".shrt", "chains", "catalog-getproduct.yaml")
	writeFile(t, path, "edited")
	out = captureStdout(t, func() { err = contractPlan([]string{"-all", "-write"}) })
	if raw, _ := os.ReadFile(path); err != nil || string(raw) != "edited" || !strings.Contains(out, "2 existing chain file(s) kept") {
		t.Fatalf("an existing chain is kept without -force: %v\n%s", err, out)
	}
	captureStdout(t, func() { err = contractPlan([]string{"-all", "-write", "-force"}) })
	if raw, _ := os.ReadFile(path); err != nil || string(raw) == "edited" {
		t.Fatalf("-force overwrites: %v", err)
	}
	if err := contractPlan([]string{"-all", "GetProduct"}); err == nil {
		t.Fatal("-all names no rpc")
	}
}
