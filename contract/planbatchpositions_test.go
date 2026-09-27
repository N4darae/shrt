package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestABatchIsProbedWithTheRefusedLineFirstLastAndNamingAnUnknownID(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "AddStockBatch")
	first := planStep(t, p, "add_stock_batch_partial_first")
	if bodyAt(t, first, "lines.0.qty") != "0" || bodyAt(t, first, "lines.1.qty") == "0" {
		t.Fatalf("the refused line comes first:\n%s", text)
	}
	wantExpect(t, first, "results.1.status.code", "SUCCESS")
	wantExists(t, first, "results.2", false)
	last := planStep(t, p, "add_stock_batch_partial_last")
	if bodyAt(t, last, "lines.1.qty") != "0" || bodyAt(t, last, "lines.0.qty") == "0" {
		t.Fatalf("the refused line comes last:\n%s", text)
	}
	wantExpect(t, last, "results.0.status.code", "SUCCESS")
	unknown := planStep(t, p, "add_stock_batch_unknown_id_product_line")
	if got := bodyAt(t, unknown, "lines.1.id_product"); !strings.HasSuffix(got, "}-unknown") {
		t.Fatalf("the last line names an id no record has, got %s:\n%s", got, text)
	}
	wantExpect(t, unknown, "results.1.status.details.0.reason", "ProductNotFound")
	wantExpect(t, unknown, "results.0.status.code", "SUCCESS")
	before := planStep(t, p, "get_product_2_before_add_stock_batch_unknown_id_product_line")
	after := planStep(t, p, "get_product_2_after_add_stock_batch_unknown_id_product_line")
	wantExpect(t, after, "product.qty_on_hand", "${"+before.ID+".product.qty_on_hand}")
	wantExpect(t, planStep(t, p, "get_product_after_add_stock_batch_unknown_id_product_line"), "product.qty_on_hand", int64(12))
	wantExpect(t, planStep(t, p, "get_product_after_add_stock_batch_partial_first"), "product.qty_on_hand",
		"${get_product_before_add_stock_batch_partial_first.product.qty_on_hand}")
	if !strings.Contains(notes, "also refuse one line each") {
		t.Fatalf("the plan says what the positional probes prove:\n%s", notes)
	}
}

func TestABatchWhoseFailuresSayTheLineAloneIsRefusedProbesAnUnknownIDOnOneLineOnly(t *testing.T) {
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
		text := strings.NewReplacer(
			"when: a line has qty zero or negative; reported on that line only",
			"when: a line's qty is zero or negative; that line alone is refused, the others still apply",
			"when: a line names an unknown product; reported on that line only",
			"when: a line names a product that does not exist; that line alone is refused, the others still apply",
		).Replace(string(raw))
		if err := os.WriteFile(filepath.Join(dir, e.Name()), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lib, broken, err := contract.LoadLibraryIn(dir, cat)
	if err != nil || len(broken) > 0 {
		t.Fatalf("load contracts: %v %v", err, broken)
	}
	p, err := contract.BuildPlanFor([]string{"AddStockBatch"}, lib, cat, "shopdemo")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := p.YAML()
	unknown := planStep(t, p, "add_stock_batch_unknown_id_product_line")
	wantExpect(t, unknown, "status.code", "SUCCESS")
	wantExpect(t, unknown, "results.0.status.code", "SUCCESS")
	wantExpect(t, unknown, "results.1.status.details.0.reason", "ProductNotFound")
	for _, s := range p.Chain.Steps {
		if s.ID == "add_stock_batch_unknown_id_product" {
			t.Fatalf("a refusal stated per line is not probed as a refusal of the whole batch:\n%s", raw)
		}
	}
}
