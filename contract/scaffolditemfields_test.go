package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func scaffoldEntry(t *testing.T, domain, rpc string, existing *contract.Library) string {
	t.Helper()
	cat, _ := shopDemo(t)
	raw := renderScaffold(t, domain, contract.Domains(cat.Methods())[domain], existing, cat)
	text := string(raw)
	entry := text[strings.Index(text, rpc+":"):]
	if next := strings.Index(entry[1:], "\n    shop."); next >= 0 {
		entry = entry[:next+1]
	}
	return entry
}

func TestContractInitScaffoldsEveryFieldOfARepeatedItem(t *testing.T) {
	for _, c := range [][2]string{{"orders", "shop.orders.v1.OrderService/CreateOrder"}, {"catalog", "shop.catalog.v1.StockService/AddStockBatch"}} {
		entry := scaffoldEntry(t, c[0], c[1], contract.NewLibrary(nil))
		for _, key := range []string{"lines:", "lines.id_product:", "lines.qty:"} {
			if !strings.Contains(entry, "\n            "+key) {
				t.Fatalf("%s: a repeated item's field %s is scaffolded:\n%s", c[1], key, entry)
			}
		}
	}
}

func TestContractInitAddsAnItemFieldACuratedEntryLacks(t *testing.T) {
	entry := scaffoldEntry(t, "orders", "shop.orders.v1.OrderService/CreateOrder", shopLibrary(t, curatedOrderWithoutNewField))
	if !strings.Contains(entry, "\n            lines.qty:") {
		t.Fatalf("lines.qty is new to the curated entry, so it is scaffolded:\n%s", entry)
	}
	if !strings.Contains(entry, "curated note about lines") || strings.Count(entry, "\n            lines:") != 1 {
		t.Fatalf("the curated lines entry is kept once:\n%s", entry)
	}
}
