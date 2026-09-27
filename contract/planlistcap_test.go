package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestPlanListsTwelveFixturesSoAResultCapShows(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "ListProducts")
	probe := planStep(t, p, "list_products_12_products")
	for _, id := range []string{"create_product", "create_product_4", "create_product_5", "create_product_12"} {
		if !strings.Contains(text, "id_product: ${"+id+".product.id_product}\n") {
			t.Fatalf("the probe asserts %s is listed:\n%s", id, text)
		}
	}
	wantExists(t, probe, "products.11", true)
	wantExists(t, probe, "products.12", false)
	if _, ok := p.Chain.Step("create_product_13"); ok {
		t.Fatalf("the probe plants 12 fixtures, reusing the 4 the plan has:\n%s", text)
	}
	if sku := bodyAt(t, planStep(t, p, "create_product_12"), "sku"); !strings.HasPrefix(sku, "sku-${vars.tag}-") {
		t.Fatalf("each extra fixture stays under the listed prefix, got %s", sku)
	}
	if !strings.Contains(notes, "states no limit") {
		t.Fatalf("a note says why the probe is there:\n%s", notes)
	}
}

func TestPlanListsOneMoreThanAStatedLimit(t *testing.T) {
	cat, _ := shopDemo(t)
	dir := t.TempDir()
	src := filepath.Join("testdata", "shopdemo", "contracts")
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(string(raw), "sorted by sku ascending;", "sorted by sku ascending, at most 6;", 1)
		if err := os.WriteFile(filepath.Join(dir, e.Name()), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lib, broken, err := contract.LoadLibraryIn(dir, cat)
	if err != nil || len(broken) > 0 {
		t.Fatalf("load: %v %v", err, broken)
	}
	p, err := contract.BuildPlanFor([]string{"ListProducts"}, lib, cat, "shopdemo")
	if err != nil {
		t.Fatal(err)
	}
	probe := planStep(t, p, "list_products_7_products")
	wantExists(t, probe, "products.5", true)
	wantExists(t, probe, "products.6", false)
	if _, ok := p.Chain.Step("create_product_8"); ok {
		t.Fatal("a stated limit of 6 needs 7 fixtures, no more")
	}
}
