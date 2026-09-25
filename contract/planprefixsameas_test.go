package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func TestPlanForAPrefixFilterDeclaredSameAsAFullValueStillPlansTheCaseVariant(t *testing.T) {
	forms := map[string]func(rpcs map[string]*contract.RPCContract){
		"value": func(map[string]*contract.RPCContract) {},
		"same_as": func(rpcs map[string]*contract.RPCContract) {
			if c := rpcs["shop.catalog.v1.ProductService/CreateProduct"]; c != nil {
				c.Fields["sku"].Value = "sku-${vars.tag}-a"
			}
			if c := rpcs["shop.catalog.v1.ProductService/ListProducts"]; c != nil {
				c.Fields["sku_prefix"].Value = ""
				c.Fields["sku_prefix"].SameAs = "shop.catalog.v1.ProductService/CreateProduct->sku"
				c.Fields["sku_prefix"].Note = "filter by prefix, case-sensitive; empty lists all"
			}
		},
	}
	for name, mutate := range forms {
		p, text := shopDemoMutated(t, contract.PlanOptions{}, mutate, "ListProducts")
		prefix := bodyAt(t, planStep(t, p, "list_products"), "sku_prefix")
		if strings.Contains(prefix, "${steps.") {
			t.Fatalf("%s: the prefix is the start the fixtures share, not a reference to one whole sku: %s\n%s", name, prefix, text)
		}
		cased := bodyAt(t, planStep(t, p, "create_product_prefix_case"), "sku")
		if !strings.HasPrefix(strings.ToLower(cased), strings.ToLower(prefix)) || strings.HasPrefix(cased, prefix) {
			t.Fatalf("%s: the case fixture starts with the prefix in another letter case: prefix %s, sku %s\n%s", name, prefix, cased, text)
		}
		planStep(t, p, "create_product_prefix_inside")
		wantExists(t, planStep(t, p, "list_products"), "products.3", false)
	}
}

func TestAPlannedSameAsPrefixIsTerminatedAndPassesStrictLint(t *testing.T) {
	for _, sku := range []string{"sku-${vars.tag}", "sku-${vars.tag}-a"} {
		p, text := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
			if c := rpcs["shop.catalog.v1.ProductService/CreateProduct"]; c != nil {
				c.Fields["sku"].Value = sku
			}
			if c := rpcs["shop.catalog.v1.ProductService/ListProducts"]; c != nil {
				c.Fields["sku_prefix"].Value = ""
				c.Fields["sku_prefix"].SameAs = "shop.catalog.v1.ProductService/CreateProduct->sku"
				c.Fields["sku_prefix"].Note = "filter by prefix, case-sensitive; empty lists all"
			}
		}, "ListProducts")
		if prefix := bodyAt(t, planStep(t, p, "list_products"), "sku_prefix"); prefix != "sku-${vars.tag}-" {
			t.Fatalf("%s: the prefix must end with the terminator every fixture carries after the var, got %q\n%s", sku, prefix, text)
		}
		path := filepath.Join(t.TempDir(), "sameas.yaml")
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := chain.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		cat, lib := shopDemo(t)
		for _, i := range contract.LintChain(c, cat, contract.ChainLintOptions{Strict: true, Library: lib, Chain: chain.LintOptions{Hints: true}}) {
			if i.Kind == chain.KindUnterminatedPrefix || i.IsError() {
				t.Fatalf("%s: the planned chain fails its own strict lint: [%s] %s\n%s", sku, i.Step, i.Message, text)
			}
		}
	}
}
