package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func unscopedListPlan(t *testing.T, sku string) (*contract.Plan, string) {
	t.Helper()
	p := editedPlan(t, func(name, body string) string {
		if name != "catalog.yaml" {
			return body
		}
		body = strings.Replace(body, "                value: sku-${vars.tag}\n                note: prefix filter", "                note: prefix filter", 1)
		return strings.Replace(body, "                value: sku-${vars.tag}\n                note: unique", "                value: "+sku+"\n                note: unique", 1)
	}, "ListProducts")
	raw, err := p.YAML()
	if err != nil {
		t.Fatal(err)
	}
	return p, string(raw) + "\n" + strings.Join(p.Notes, "\n")
}

func TestAListWithAnEmptyPrefixIsScopedToThisRunBeforeItsCountIsAsserted(t *testing.T) {
	p, text := unscopedListPlan(t, "sku-${vars.tag}")
	list := planStep(t, p, "list_products")
	if got := bodyAt(t, list, "sku_prefix"); got != "sku-${vars.tag}-" {
		t.Fatalf("an empty prefix lists every product in the database; the plan scopes it to the start the fixtures share, got %q:\n%s", got, text)
	}
	wantExists(t, list, "products.4", false)
	if !strings.Contains(text, "the start every fixture's value shares") {
		t.Fatalf("the plan says it scoped the list:\n%s", text)
	}
}

func TestAListNothingCanScopeAssertsALowerBoundOnly(t *testing.T) {
	p, text := unscopedListPlan(t, "sku-fixed")
	list := planStep(t, p, "list_products")
	if got := bodyAt(t, list, "sku_prefix"); got != "" {
		t.Fatalf("nothing in the fixtures varies per run, so the prefix stays empty, got %q:\n%s", got, text)
	}
	wantExists(t, list, "products.3", true)
	for _, e := range list.Expect {
		if strings.HasPrefix(e.Path, "products.") && (e.Equals != nil || (e.Exists != nil && !*e.Exists)) {
			t.Fatalf("an unscoped list holds whatever other runs left, so no position or exact count is asserted, got %s:\n%s", e.Path, text)
		}
	}
	cat, _ := shopDemo(t)
	for _, i := range chain.Lint(p.Chain, cat) {
		if i.Kind == chain.KindUnscopedCount {
			t.Fatalf("the planned chain passes its own lint rule: %v", i)
		}
	}
	if !strings.Contains(text, "asserts at least 4 item(s)") {
		t.Fatalf("the plan says why it asserts only a lower bound:\n%s", text)
	}
}
