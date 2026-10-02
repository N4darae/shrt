package main

import (
	"strings"
	"testing"
)

func rowChains() []*gateChain {
	const create, get, fetch, confirm, product = "x.v1.CustomerService/CreateCustomer", "x.v1.CustomerService/GetCustomer", "x.v1.OrderService/FetchOrder", "x.v1.OrderService/ConfirmOrder", "x.v1.ProductService/GetProduct"
	unclear := reason{Kind: reasonUnclear, Step: "create_customer", RPC: create, Read: "get_customer", ReadRPC: get, Path: "customer.name", Want: "Customer t1", Got: "cust-t1@example.test"}
	set := reason{Kind: reasonSet, Step: "fetch_order", RPC: fetch, Path: "order.lines"}
	chains := []*gateChain{
		{name: "customers", failed: true, class: "regression", items: []gateItem{{Step: "get_customer", Call: get, Path: "customer.name", Want: "Customer t1", Got: "cust-t1@example.test", Failed: true, Reason: unclear}}},
		{name: "orders", failed: true, class: "regression", items: []gateItem{{Step: "fetch_order", Call: fetch, Path: "order.lines", Want: "2", Got: "1", Failed: true, Reason: set}}},
		{name: "stock", failed: true, class: "regression", items: []gateItem{
			{Step: "get_product", Call: product, Path: "product.qty_on_hand", Want: "4", Got: "8", Failed: true, Reason: reason{Kind: reasonWrite, Step: "confirm_order", RPC: confirm}},
			{Step: "confirm_order", Call: confirm, Path: "status.code", Want: "SUCCESS", Got: "REJECTED", Failed: true, Reason: reason{Kind: reasonWrite, Step: "confirm_order", RPC: confirm}},
		}},
		{name: "stored", failed: true, class: "regression", items: []gateItem{{Step: "get_thing", Call: "x.v1.S/Get", Path: "thing.state", Want: "DONE", Got: "OPEN", Failed: true,
			Reason: reason{Kind: reasonStored, Step: "w", RPC: "x.v1.S/Confirm", Path: "thing.state", Want: "DONE", Got: "OPEN", ReadRPC: "Get"}}}},
		{name: "confirms", failed: true, class: "regression", items: []gateItem{{Step: "confirm_order", Call: confirm, Path: "order.status", Want: "CONFIRMED", Got: "PENDING", Failed: true, Reason: reason{Kind: reasonWrite, Step: "confirm_order", RPC: confirm, Profile: "clerk"}}}},
	}
	settleGate(chains)
	return chains
}

func TestAGateRowNamesItsReadOnce(t *testing.T) {
	chains := rowChains()
	rows := map[string]string{}
	for _, g := range chains {
		rows[g.name] = g.line(0)
	}
	for name, want := range map[string]string{
		"customers": "customer.name want=Customer t1 got=cust-t1@example.test; unclear: write create_customer (CustomerService/CreateCustomer) or the read",
		"orders":    "fetch_order (OrderService/FetchOrder) order.lines want=2 got=1; suspect the read: answers another set of order.lines",
		"stock":     "get_product (ProductService/GetProduct) product.qty_on_hand want=4 got=8; suspect write confirm_order (OrderService/ConfirmOrder)",
		"stored":    "get_thing (S/Get) thing.state want=DONE got=OPEN; suspect write w (S/Confirm): stores other than it answered",
		"confirms":  "confirm_order (OrderService/ConfirmOrder) order.status want=CONFIRMED got=PENDING; suspect the write as clerk",
	} {
		if !strings.HasSuffix(rows[name], want) {
			t.Errorf("%s: want the row to end %q, got %q", name, want, rows[name])
		}
	}
	if strings.Contains(rows["customers"], "answered") || strings.Count(rows["customers"], "cust-t1@example.test") != 1 {
		t.Errorf("the row printed the read's want and got once, the unclear form does not repeat them: %q", rows["customers"])
	}
}
