package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestContractPlanNotesPrintsGapsThenOneLinePerProbeGroup(t *testing.T) {
	dir := shopWorkspace(t, shopConfig)
	restore := chdir(t, dir)
	defer restore()
	writeFile(t, filepath.Join(dir, ".shrt", "contracts", "shop.yaml"), `apiVersion: shrt/contract/v1
domain: shop
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        fields:
            sku: {value: "sku-${vars.tag}"}
        status: draft
    shop.catalog.v1.ProductService/GetProduct:
        summary: reads a product
        fields:
            id_product: {from: shop.catalog.v1.ProductService/CreateProduct->product.id_product}
        failures:
            - code: 1204
              reason: ProductNotFound
              when: no product has this id
        status: draft
`)
	var err error
	full := captureStdout(t, func() { err = contractPlan([]string{"GetProduct", "-notes", "-v"}) })
	if err != nil {
		t.Fatalf("plan -notes -v: %v\n%s", err, full)
	}
	if !strings.Contains(full, "step ids: ") || !strings.Contains(full, "\nnote: ") {
		t.Fatalf("-notes -v prints every note in full and every step id:\n%s", full)
	}
	out := captureStdout(t, func() { err = contractPlan([]string{"GetProduct", "-notes"}) })
	if err != nil {
		t.Fatalf("plan -notes: %v\n%s", err, out)
	}
	if strings.Contains(out, "step ids: ") || strings.Contains(out, "\nnote: ") || len(out) >= len(full) {
		t.Fatalf("-notes alone prints no step ids and no note in full:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	seenGroup := false
	for _, line := range lines[2:] {
		switch {
		case strings.HasPrefix(line, "gap: "):
			if seenGroup {
				t.Fatalf("gap lines come before the probe groups:\n%s", out)
			}
		case strings.HasPrefix(line, "unknown id (1 step): "):
			seenGroup = true
		case strings.HasPrefix(line, "setup "), strings.HasPrefix(line, "target "):
			t.Fatalf("only probe groups get a line:\n%s", out)
		}
	}
	if !seenGroup || !strings.Contains(out, "-notes -v") {
		t.Fatalf("-notes prints one line per probe group and says how to see every note:\n%s", out)
	}
}
