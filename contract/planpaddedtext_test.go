package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestACreateSendsFreeTextPaddedWithSpacesAndReadsItBackAsSent(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateProduct")
	probe := planStep(t, p, "create_product_padded_text")
	name := bodyAt(t, probe, "name")
	if !strings.HasPrefix(name, "  ") || !strings.HasSuffix(name, "  ") || strings.TrimSpace(name) != bodyAt(t, planStep(t, p, "create_product"), "name") {
		t.Fatalf("the probe pads the name with spaces on both sides, got %q:\n%s", name, text)
	}
	wantExpect(t, probe, "product.name", "${steps.create_product_padded_text.request.name}")
	wantExpect(t, planStep(t, p, "get_product_after_create_product_padded_text"), "product.name", "${steps.create_product_padded_text.request.name}")
	if strings.HasPrefix(bodyAt(t, probe, "sku"), " ") {
		t.Fatalf("sku is not free text and its contract does not say it is kept untrimmed:\n%s", text)
	}
	if !strings.Contains(notes, "create_product_padded_text sends free text") {
		t.Fatalf("the plan says why it pads the text:\n%s", notes)
	}
}

func TestAPaddedTextProbeLeavesOutAFieldTheContractSaysIsTrimmed(t *testing.T) {
	p, text := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs["shop.catalog.v1.ProductService/CreateProduct"]; c != nil {
			c.Fields["name"].Note = "display name, stored trimmed"
			c.Fields["sku"].Note = "unique, compared exactly as sent, not trimmed"
		}
	}, "CreateProduct")
	probe := planStep(t, p, "create_product_padded_text")
	if strings.HasPrefix(bodyAt(t, probe, "name"), " ") {
		t.Fatalf("a name the contract says is trimmed is not padded:\n%s", text)
	}
	if !strings.HasPrefix(bodyAt(t, probe, "sku"), "  ") {
		t.Fatalf("a sku the contract says is not trimmed is padded:\n%s", text)
	}
	if !strings.Contains(strings.Join(p.Notes, "\n"), "name is not padded with spaces") {
		t.Fatalf("the plan says which field it left unpadded:\n%s", strings.Join(p.Notes, "\n"))
	}
}
