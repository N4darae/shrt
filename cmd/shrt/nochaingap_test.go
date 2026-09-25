package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestContractStatusGapsNamesAnRpcNoChainCalls(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/descriptor.binpb", string(catalogtest.ShopDescriptor()))
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", `apiVersion: shrt/v1
name: cli-thing-flow
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
          sku: s
`)
	var err error
	out := captureStdout(t, func() { err = contractStatus([]string{"-gaps"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no chain     shop.orders.v1.OrderService/CreateOrder (repeated request field(s) no chain sends at all: lines)") {
		t.Fatalf("an rpc no chain calls is a gap, and so are its repeated fields:\n%s", out)
	}
	if strings.Contains(out, "no chain     shop.catalog.v1.ProductService/CreateProduct") {
		t.Fatalf("a chain calls CreateProduct:\n%s", out)
	}
	if !strings.Contains(out, "no chain     shop.orders.v1.OrderService/WatchOrder") {
		t.Fatalf("a server-streaming rpc is callable, so no chain calling it is a gap:\n%s", out)
	}
	if !strings.Contains(out, "no chain         no chain calls the rpc") {
		t.Fatalf("the legend must explain the gap:\n%s", out)
	}
}
