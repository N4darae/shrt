package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

func TestChainLint_ARequestFieldFedFromAnotherEntityIsAnError(t *testing.T) {
	fixtures := `
    - id: create_customer
      call: shop.customers.v1.CustomerService/CreateCustomer
      body: {email: a@shop.test, name: A}
      export: {cust: customer.id_customer}
    - id: create_product
      call: shop.catalog.v1.ProductService/CreateProduct
      body: {sku: sku-a, name: A, price_minor: "1250"}
    - id: get_product
      call: shop.catalog.v1.ProductService/GetProduct
      body: {id_product: "${create_product.product.id_product}"}
    - id: list_products
      call: shop.catalog.v1.ProductService/ListProducts
      body: {sku_prefix: sku-a}
`
	cases := []struct {
		name, body, expect, want string
	}{
		{"customer id into a line", `{id_customer: "${create_customer.customer.id_customer}", lines: [{id_product: "${create_customer.customer.id_customer}", qty: "2"}]}`, "",
			"lines.id_product is fed from CreateCustomer customer.id_customer, but its contract takes it from CreateProduct product.id_product"},
		{"through an export", `{id_customer: "${cust}", lines: [{id_product: "${cust}", qty: "2"}]}`, "",
			"lines.id_product is fed from CreateCustomer customer.id_customer, but its contract takes it from CreateProduct product.id_product"},
		{"same rpc, another path", `{id_customer: "${cust}", lines: [{id_product: "${steps.create_product.response.product.sku}", qty: "2"}]}`, "",
			"lines.id_product is fed from CreateProduct product.sku, but its contract takes it from CreateProduct product.id_product"},
		{"product id into the customer", `{id_customer: "${create_product.product.id_product}", lines: [{id_product: "${create_product.product.id_product}", qty: "2"}]}`, "",
			"id_customer is fed from CreateProduct product.id_product, but its contract takes it from CreateCustomer customer.id_customer"},
		{"as the contract says", `{id_customer: "${cust}", lines: [{id_product: "${create_product.product.id_product}", qty: "2"}]}`, "", ""},
		{"read back by GetProduct", `{id_customer: "${cust}", lines: [{id_product: "${get_product.product.id_product}", qty: "2"}]}`, "", ""},
		{"read from ListProducts", `{id_customer: "${cust}", lines: [{id_product: "${list_products.products[0].id_product}", qty: "2"}]}`, "", ""},
		{"a probe expecting the refusal", `{id_customer: "${cust}", lines: [{id_product: "${cust}", qty: "2"}]}`, "{path: status.code, equals: ProductNotFound}", ""},
		{"interpolated into other text", `{id_customer: "${cust}", lines: [{id_product: "${cust}-x", qty: "2"}]}`, "", ""},
	}
	cat, lib := shopDemo(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "name: wiring\nsteps:" + fixtures + "    - id: create_order\n      call: shop.orders.v1.OrderService/CreateOrder\n      body: " + tc.body + "\n"
			if tc.expect != "" {
				src += "      expect: [" + tc.expect + "]\n"
			}
			c := &chain.Chain{}
			if err := yaml.Unmarshal([]byte(src), c); err != nil {
				t.Fatal(err)
			}
			_ = c.Normalize()
			got := []string{}
			for _, i := range contract.LintChain(c, cat, contract.ChainLintOptions{Library: lib}) {
				if strings.Contains(i.Message, " is fed from ") {
					got = append(got, i.Step+": "+i.Message)
				}
			}
			want := []string{}
			if tc.want != "" {
				want = append(want, "create_order: "+tc.want)
			}
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}
