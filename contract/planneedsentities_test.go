package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func stockedBefore(p *contract.Plan, upto int, product string) bool {
	for _, s := range p.Chain.Steps[:upto] {
		if !strings.HasSuffix(s.Call, "/AddStock") || isRefusal(s) {
			continue
		}
		if v, ok := s.Body["id_product"].(string); ok && v == "${"+product+".product.id_product}" {
			return true
		}
	}
	return false
}

func isRefusal(s *chain.Step) bool {
	for _, e := range s.Expect {
		if e.Path == "transport.code" || (e.Path == "status.code" && e.NotEqual != nil) {
			return true
		}
		if e.Path == "status.code" && e.Equals != nil && e.Equals != "SUCCESS" {
			return true
		}
	}
	return false
}

func TestPlanStocksEveryProductOfAnOrderBeforeAnyConfirmWhateverTheTargetOrder(t *testing.T) {
	for _, targets := range [][]string{
		{"ListOrders", "AddStock", "ConfirmOrder", "CancelOrder"},
		{"ListOrders", "AddStock"},
		{"ConfirmOrder"},
	} {
		p, text := confirmNeedsStockPlan(t, targets...)
		for i, st := range p.Chain.Steps {
			if !strings.HasSuffix(st.Call, "/ConfirmOrder") {
				continue
			}
			ref, _ := st.Body["id_order"].(string)
			order := strings.TrimSuffix(strings.TrimPrefix(ref, "${"), ".order.id_order}")
			creator, ok := p.Chain.Step(order)
			if !ok {
				continue
			}
			lines, _ := creator.Body["lines"].([]any)
			for _, l := range lines {
				item, _ := l.(map[string]any)
				prod, _ := item["id_product"].(string)
				prod = strings.TrimSuffix(strings.TrimPrefix(prod, "${"), ".product.id_product}")
				if !stockedBefore(p, i, prod) {
					t.Fatalf("%v: step %s confirms %s, whose line reads %s, but no AddStock for %s runs before it:\n%s",
						targets, st.ID, order, prod, prod, text)
				}
			}
		}
	}
}

func shopDemoEdited(t *testing.T, edit func(name, body string) string) *contract.Library {
	t.Helper()
	cat, _ := shopDemo(t)
	src := filepath.Join("testdata", "shopdemo", "contracts")
	dir := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), []byte(edit(e.Name(), string(raw))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lib, broken, err := contract.LoadLibraryIn(dir, cat)
	if err != nil || len(broken) > 0 {
		t.Fatalf("load edited contracts: %v %v", err, broken)
	}
	return lib
}

func confirmNeedsStockPlan(t *testing.T, targets ...string) (*contract.Plan, string) {
	t.Helper()
	cat, _ := shopDemo(t)
	lib := shopDemoEdited(t, func(name, body string) string {
		if name != "orders.yaml" {
			return body
		}
		body = strings.Replace(body, "        needs: [shop.catalog.v1.StockService/AddStock]\n", "", 1)
		return strings.Replace(body, "    shop.orders.v1.OrderService/ConfirmOrder:\n", "    shop.orders.v1.OrderService/ConfirmOrder:\n        needs: [shop.catalog.v1.StockService/AddStock]\n", 1)
	})
	p, err := contract.BuildPlanFor(targets, lib, cat, "shopdemo")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.YAML()
	if err != nil {
		t.Fatal(err)
	}
	return p, string(raw) + "\n" + strings.Join(p.Notes, "\n")
}
