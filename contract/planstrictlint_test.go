package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func TestAFreshlyPlannedChainPassesStrictLintWithNoUnassertedTimestamp(t *testing.T) {
	cat, lib := shopDemo(t)
	plansPassStrictLint(t, cat, lib, [][]string{
		{"ListOrders", "AddStock", "ConfirmOrder", "CancelOrder"},
		{"CreateProduct", "ListProducts"},
		{"GetProduct"},
		{"AddStock"},
		{"CreateCustomer"},
		{"FetchOrder"},
	})
}

func TestAPlannedAddStockAssertsAStockLevelItsContractDeclaresAsTerminal(t *testing.T) {
	cat, lib := shopDemoEdited(t, func(name, body string) string {
		if name != "catalog.yaml" {
			return body
		}
		return strings.Replace(body, "        exports:\n            qty_on_hand: stock on hand after the addition\n",
			"        terminal:\n            qty_on_hand: stock on hand after the addition\n", 1)
	})
	if c, ok := lib.Get("shop.catalog.v1.StockService/AddStock"); !ok || c.Terminal["qty_on_hand"] == "" {
		t.Fatal("fixture: AddStock must declare qty_on_hand under terminal")
	}
	plansPassStrictLint(t, cat, lib, [][]string{{"AddStock"}, {"ListOrders", "AddStock", "ConfirmOrder"}})
}

func strictLint(t *testing.T, name, text string, cat *catalog.Catalog, lib *contract.Library, opts chain.LintOptions) []chain.Issue {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contract.LintChain(c, cat, contract.ChainLintOptions{Strict: true, Library: lib, Chain: opts})
}

func plansPassStrictLint(t *testing.T, cat *catalog.Catalog, lib *contract.Library, plans [][]string) {
	t.Helper()
	for _, targets := range plans {
		p, err := contract.BuildPlanFor(targets, lib, cat, "strict")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := p.YAML()
		if err != nil {
			t.Fatal(err)
		}
		issues := strictLint(t, "strict.yaml", string(raw), cat, lib, chain.LintOptions{Hints: true})
		bad := []string{}
		for _, i := range issues {
			if i.IsError() || i.Kind == chain.KindUnassertedTimestamp {
				bad = append(bad, "["+i.Step+"] "+i.Message)
			}
		}
		if len(bad) > 0 {
			t.Fatalf("%v: the planned chain fails its own strict lint:\n%s", targets, strings.Join(bad, "\n"))
		}
	}
}
