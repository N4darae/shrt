package store

import (
	"strings"
	"testing"
)

func TestAClippedValueKeepsTheTailThatTellsRowsApart(t *testing.T) {
	seen := map[string]string{}
	for _, sku := range []string{"sku-t3-catalog-stock-flow-a", "sku-t3-catalog-stock-flow-b", "sku-t3-catalog-stock-flow-clerk"} {
		got := shortValue(sku)
		if len([]rune(got)) > summaryValue+1 {
			t.Errorf("%q clipped to %q, longer than the cell allows", sku, got)
		}
		if !strings.HasSuffix(sku, strings.SplitN(got, "…", 2)[1]) || !strings.HasPrefix(got, "sku-t3") {
			t.Errorf("%q clipped to %q: keep the head and the tail", sku, got)
		}
		if other, dup := seen[got]; dup {
			t.Errorf("%q and %q both show as %q", sku, other, got)
		}
		seen[got] = sku
	}
	if got := shortValue("short"); got != "short" {
		t.Errorf("a short value is kept whole: %q", got)
	}
}
