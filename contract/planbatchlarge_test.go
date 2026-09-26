package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestPlanGivesABatchLineTheLargeValueItsSingleItemRpcGets(t *testing.T) {
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
		text := strings.Replace(string(raw), "                note: one AddStock per line, applied independently in order\n", "                note: at least one line\n", 1)
		if err := os.WriteFile(filepath.Join(dir, e.Name()), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lib, broken, err := contract.LoadLibraryIn(dir, cat)
	if err != nil || len(broken) > 0 {
		t.Fatalf("load: %v %v", err, broken)
	}
	p, err := contract.BuildPlanFor([]string{"AddStockBatch"}, lib, cat, "shopdemo")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := p.YAML()
	text := string(raw)
	large := planStep(t, p, "add_stock_batch_qty_large")
	if bodyAt(t, large, "lines.0.qty") != "12345" || bodyAt(t, large, "lines.1.qty") == "12345" {
		t.Fatalf("one line carries the large quantity AddStock is probed with, beside a normal line:\n%s", text)
	}
	wantExpect(t, large, "results.0.status.code", "SUCCESS")
	wantExpect(t, large, "results.1.status.code", "SUCCESS")
	gte := false
	for _, e := range large.Expect {
		gte = gte || (e.Path == "results.0.qty_on_hand" && e.Gte == "12345")
	}
	if !gte {
		t.Fatalf("the large line's reported level is at least what it added, so a cap on it fails:\n%s", text)
	}
	wantExpect(t, planStep(t, p, "get_product_after_add_stock_batch_qty_large"), "product.qty_on_hand", "${add_stock_batch_qty_large.results.0.qty_on_hand}")
}
