package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestAReadOfACreatedRecordAssertsEveryFieldTheCreateSent(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "GetProduct")
	get := planStep(t, p, "get_product")
	for _, field := range []string{"sku", "name", "price_minor"} {
		wantExpect(t, get, "product."+field, "${steps.create_product.request."+field+"}")
	}
	if strings.Count(text, "path: product.qty_on_hand\n          equals: ${steps.create_product") > 0 {
		t.Fatalf("qty_on_hand is not a field the create sent:\n%s", text)
	}
}

func TestACreateIsReadBackAndProbedInMixedCase(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateCustomer")
	read := planStep(t, p, "get_customer_after_create_customer")
	wantExpect(t, read, "customer.name", "${steps.create_customer.request.name}")
	wantExpect(t, read, "customer.email", "${create_customer.customer.email}")
	probe := planStep(t, p, "create_customer_mixed_case")
	email := bodyAt(t, probe, "email")
	if email == strings.ToLower(email) || email == bodyAt(t, planStep(t, p, "create_customer"), "email") {
		t.Fatalf("the probe sends a fresh email with letters in upper case, got %s:\n%s", email, text)
	}
	back := planStep(t, p, "get_customer_after_create_customer_mixed_case")
	wantExpect(t, back, "customer.email", "${create_customer_mixed_case.customer.email}")
	wantExpect(t, back, "customer.name", "${steps.create_customer_mixed_case.request.name}")
	wantExpect(t, probe, "customer.name", "${steps.create_customer_mixed_case.request.name}")
	if !strings.Contains(notes, "normalises") || !strings.Contains(notes, "case swapped") {
		t.Fatalf("the plan says what the read-back asserts and why email is compared with the echo:\n%s", notes)
	}
}

func TestAFieldTheContractSaysIsNormalisedIsComparedWithTheEchoNotTheRequest(t *testing.T) {
	p, text := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs["shop.catalog.v1.ProductService/CreateProduct"]; c != nil && c.Fields["name"] != nil {
			c.Fields["name"].Note = "display name, stored lowercased"
		}
	}, "GetProduct")
	get := planStep(t, p, "get_product")
	wantExpect(t, get, "product.name", "${create_product.product.name}")
	wantExpect(t, get, "product.sku", "${steps.create_product.request.sku}")
	if strings.Contains(text, "product.name\n          equals: ${steps.create_product_mixed_case.request.name}") {
		t.Fatalf("a name the backend lowercases is not expected echoed in the case it was sent:\n%s", text)
	}
}
