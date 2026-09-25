package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestContractStatusGapsNamesARepeatedFieldNoChainSendsTwice(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/descriptor.binpb", string(catalogtest.ShopDescriptor()))
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", `apiVersion: shrt/v1
name: cli-thing-flow
steps:
    - id: create_order
      call: OrderService/CreateOrder
      body:
          id_customer: c
          lines:
              - id_product: p
                qty: "3"
`)
	var err error
	out := captureStdout(t, func() { err = contractStatus([]string{"-gaps"}) })
	if err != nil {
		t.Fatal(err)
	}
	want := "one item     shop.orders.v1.OrderService/CreateOrder lines: at most 1 item in every chain that sends it (cli-thing-flow)"
	if !strings.Contains(out, want) {
		t.Fatalf("want %q in:\n%s", want, out)
	}
	if !strings.Contains(out, "per-item logic") {
		t.Fatalf("the legend must say what a single item leaves untested:\n%s", out)
	}

	writeFile(t, ".shrt/chains/two.yaml", `apiVersion: shrt/v1
name: two
steps:
    - id: create_order
      call: OrderService/CreateOrder
      body:
          lines:
              - id_product: p
                qty: "3"
              - id_product: q
                qty: "1"
`)
	out = captureStdout(t, func() { err = contractStatus([]string{"-gaps"}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "one item     shop") {
		t.Fatalf("a chain sends two lines, so there is no single-item gap:\n%s", out)
	}
}

func TestContractStatusGapsNamesItemsThatAllPointAtOneResource(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/descriptor.binpb", string(catalogtest.ShopDescriptor()))
	writeFile(t, ".shrt/chains/same.yaml", `apiVersion: shrt/v1
name: same
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
          sku: s
    - id: create_order
      call: OrderService/CreateOrder
      body:
          lines:
              - id_product: ${create_product.product.id_product}
                qty: "2"
              - id_product: ${create_product.product.id_product}
                qty: "3"
`)
	var err error
	out := captureStdout(t, func() { err = contractStatus([]string{"-gaps"}) })
	if err != nil {
		t.Fatal(err)
	}
	want := "same resource shop.orders.v1.OrderService/CreateOrder lines: every chain that sends two or more items points them all at ${create_product.product.id_product} (same)"
	if !strings.Contains(out, want) {
		t.Fatalf("want %q in:\n%s", want, out)
	}
	if !strings.Contains(out, "same resource a repeated message field") {
		t.Fatalf("the legend must say what 'same resource' means:\n%s", out)
	}
	if strings.Contains(out, "no gaps") {
		t.Fatalf("a same-resource field is a gap:\n%s", out)
	}
}
