package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
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
	for _, want := range []string{"catalog-createproduct: ", "catalog-getproduct: ", "orders-watchorder: "} {
		if !strings.Contains(out, want) {
			t.Fatalf("one line per rpc with a contract, want %q:\n%s", want, out)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, ".shrt", "chains")); len(entries) != 0 {
		t.Fatalf("without -write nothing is written, found %d file(s)", len(entries))
	}
	out = captureStdout(t, func() { err = contractPlan([]string{"-all", "-write"}) })
	if err != nil || strings.Count(out, ", written") != 3 {
		t.Fatalf("plan -all -write writes each chain: %v\n%s", err, out)
	}
	path := filepath.Join(dir, ".shrt", "chains", "catalog-getproduct.yaml")
	writeFile(t, path, "edited")
	out = captureStdout(t, func() { err = contractPlan([]string{"-all", "-write"}) })
	if raw, _ := os.ReadFile(path); err != nil || string(raw) != "edited" || !strings.Contains(out, "3 existing chain file(s) kept") {
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

func TestContractPlanAllPrintsTheGapsPlanRPCAndNotesPrint(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join("..", "..", "contract", "testdata", "shopdemo")
	desc, err := os.ReadFile(filepath.Join(src, "descriptor.binpb"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".shrt", "descriptor.binpb"), string(desc))
	writeFile(t, filepath.Join(dir, ".shrt", "config.yaml"), shopConfig+"conventions:\n    envelope_path: status.code\n    envelope_ok: SUCCESS\n")
	for _, name := range []string{"catalog.yaml", "customers.yaml", "orders.yaml"} {
		raw, err := os.ReadFile(filepath.Join(src, "contracts", name))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(string(raw), "        needs: [shop.catalog.v1.StockService/AddStock]\n", "", 1)
		writeFile(t, filepath.Join(dir, ".shrt", "contracts", name), text)
	}
	restore := chdir(t, dir)
	defer restore()
	gaps := func(out, under string) []string {
		lines := []string{}
		in := under == ""
		for _, l := range strings.Split(out, "\n") {
			if under != "" && !strings.HasPrefix(l, " ") {
				in = strings.HasPrefix(l, under+": ")
			}
			if t := strings.TrimSpace(l); in && strings.HasPrefix(t, "gap: ") {
				lines = append(lines, t)
			}
		}
		return lines
	}
	var err1, err2, err3 error
	all := captureStdout(t, func() { err1 = contractPlan([]string{"-all"}) })
	one := captureStdout(t, func() { err2 = contractPlan([]string{"ConfirmOrder"}) })
	notes := captureStdout(t, func() { err3 = contractPlan([]string{"ConfirmOrder", "-notes"}) })
	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("plan: %v %v %v\n%s", err1, err2, err3, all)
	}
	want := gaps(one, "")
	if len(want) == 0 || !strings.Contains(strings.Join(want, "\n"), "gap: step confirm_order: no write in the chain adds a known quantity") {
		t.Fatalf("plan ConfirmOrder names the exact-stock gap:\n%s", one)
	}
	if got := gaps(all, "orders-confirmorder"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("-all prints the gaps plan ConfirmOrder prints:\n%s\n---\n%s", all, one)
	}
	if !strings.Contains(notes, "\ngap: step confirm_order: no write in the chain adds a known quantity") {
		t.Fatalf("-notes labels a gap as a gap:\n%s", notes)
	}
}

func TestAPlanGapLineIsPrintedInFull(t *testing.T) {
	long := "step list: the contracts do not say sku is compared case-sensitively, so no fixture with the prefix in another letter case was planned; " +
		strings.Repeat("say what the filter does with another letter case, ", 3) + "END"
	plan := &contract.Plan{Notes: []string{long}}
	out := captureStdout(t, func() { printFillAndGaps(plan, "  ") })
	if !strings.Contains(out, "END\n") || strings.Contains(out, "...") {
		t.Errorf("a gap says how to close it, so it is never clipped:\n%s", out)
	}
}
