package chain_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func orderEchoLaterChain(t *testing.T, secondLine, readRef string) *chain.Chain {
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
    - id: confirm
      call: OrderService/ConfirmOrder
      body:
        id_order: ${` + readRef + `}
      expect:
        - path: order.lines.0.id_product
          equals: ${create_a.product.id_product}
        - path: order.lines.1.id_product
          equals: ${create_b.product.id_product}
    - id: fetch
      call: OrderService/FetchOrder
      body:
        id_order: ${confirm.order.id_order}
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

func TestLintDoesNotHintOnAListThatMirrorsTheRequestOfTheStepThatCreatedTheEntity(t *testing.T) {
	if got := orderHints(orderEchoLaterChain(t, "create_b", "order.order.id_order")); got != "" {
		t.Fatalf("confirm and fetch read the order created with lines a, b, so order.lines mirrors that request, not a sort: %q", got)
	}
}

func TestLintStillHintsWhenTheCreatingRequestDoesNotMirrorTheAssertedOrder(t *testing.T) {
	if got := orderHints(orderEchoLaterChain(t, "create_a", "order.order.id_order")); got == "" {
		t.Fatal("the order was created with lines a, a, so order.lines.1 being create_b mirrors no request")
	}
}
