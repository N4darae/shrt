package chain_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func orderEchoChain(t *testing.T, secondLine string) *chain.Chain {
	t.Helper()
	path := filepath.Join(t.TempDir(), "echo.yaml")
	raw := `apiVersion: shrt/v1
name: echo
vars:
    tag: t
steps:
    - id: create_a
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-a", name: "Anchor", price_minor: "1"}
    - id: create_b
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-b", name: "Bolt", price_minor: "2"}
    - id: customer
      call: CustomerService/CreateCustomer
      body: {email: "c-${vars.tag}@example.test"}
    - id: order
      call: OrderService/CreateOrder
      body:
        id_customer: ${customer.customer.id_customer}
        lines:
          - id_product: ${create_a.product.id_product}
            qty: "2"
          - id_product: ${` + secondLine + `.product.id_product}
            qty: "3"
      expect:
        - path: order.lines.0.id_product
          equals: ${create_a.product.id_product}
        - path: order.lines.1.id_product
          equals: ${create_b.product.id_product}
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLintDoesNotHintOnAListThatEchoesTheRequestsOrder(t *testing.T) {
	if got := orderHints(orderEchoChain(t, "create_b")); got != "" {
		t.Fatalf("order.lines echoes the request's lines, item for item, so its order is the request's, not a sort key's: %q", got)
	}
}

func TestLintStillHintsWhenTheListDoesNotMirrorTheRequest(t *testing.T) {
	if got := orderHints(orderEchoChain(t, "create_a")); got == "" {
		t.Fatal("the request's second line is create_a, so order.lines.1 being create_b is not an echo of the request's order")
	}
}
