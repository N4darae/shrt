package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

const shortCreateOrder = `apiVersion: shrt/contract/v1
domain: orders2
rpcs:
    OrderService/CreateOrder:
        summary: IMPOSTOR
        required: [NONE]
        status: draft
`

func TestAShortKeyForAnRPCAnotherOverlayDefinesIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeOverlay(t, dir, "orders.yaml", realCreateOrder)
	writeOverlay(t, dir, "orders2.yaml", shortCreateOrder)
	_, broken, err := contract.LoadLibraryIn(dir, catalogtest.Shop())
	if err != nil {
		t.Fatal(err)
	}
	if len(broken) != 1 {
		t.Fatalf("OrderService/CreateOrder is the same rpc as shop.orders.v1.OrderService/CreateOrder; want one error, got %v", broken)
	}
	msg := broken[0].Error()
	for _, want := range []string{"orders.yaml", "orders2.yaml", "shop.orders.v1.OrderService/CreateOrder", "OrderService/CreateOrder"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the error must name %q: %s", want, msg)
		}
	}
	lib, broken, err := contract.LoadLibraryIn(dir+"/none", catalogtest.Shop())
	if err != nil || len(broken) != 0 || lib.Count() != 0 {
		t.Fatalf("a missing directory is an empty library, got %v %v", broken, err)
	}
}
