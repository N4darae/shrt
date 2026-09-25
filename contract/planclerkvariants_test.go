package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func clerkOptions() contract.PlanOptions {
	return contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}}
}

func TestAShapeProbeOnARepeatedItemSendsTheBadValueOnTheFirstItemToo(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "CreateOrder")
	last := planStep(t, p, "create_order_qty_zero")
	if bodyAt(t, last, "lines.1.qty") != "0" {
		t.Fatalf("the last line carries qty 0:\n%s", text)
	}
	first := planStep(t, p, "create_order_qty_zero_first_item")
	if bodyAt(t, first, "lines.0.qty") != "0" || bodyAt(t, first, "lines.1.qty") == "0" {
		t.Fatalf("the first line alone carries qty 0:\n%s", text)
	}
	wantExpect(t, first, "transport.code", "invalid_argument")
}

func TestAListEveryRoleMayCallIsReadAsTheOtherProfileItemByItem(t *testing.T) {
	for _, target := range []string{"ListProducts", "ListOrders"} {
		p, text, _ := shopDemoPlanWith(t, clerkOptions(), target)
		main := planStep(t, p, map[string]string{"ListProducts": "list_products", "ListOrders": "list_orders"}[target])
		probe := planStep(t, p, main.ID+"_as_clerk")
		if probe.Auth != "clerk" {
			t.Fatalf("%s: the probe runs as clerk:\n%s", target, text)
		}
		list := map[string]string{"ListProducts": "products", "ListOrders": "orders"}[target]
		idField := map[string]string{"ListProducts": "id_product", "ListOrders": "id_order"}[target]
		wantExpect(t, probe, list+".0."+idField, "${"+main.ID+"."+list+".0."+idField+"}")
		found := false
		for _, e := range probe.Expect {
			if e.Exists != nil && !*e.Exists && strings.HasPrefix(e.Path, list+".") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: the clerk's list holds no item past the default profile's:\n%s", target, text)
		}
	}
}

func TestACreateEveryRoleMayCallIsRepeatedAsTheOtherProfileAndReadBack(t *testing.T) {
	p, text, _ := shopDemoPlanWith(t, clerkOptions(), "CreateCustomer")
	probe := planStep(t, p, "create_customer_as_clerk")
	if probe.Auth != "clerk" || bodyAt(t, probe, "email") == bodyAt(t, planStep(t, p, "create_customer"), "email") {
		t.Fatalf("the clerk creates a customer of its own:\n%s", text)
	}
	wantExpect(t, probe, "status.code", "SUCCESS")
	read := planStep(t, p, "get_customer_after_create_customer_as_clerk")
	wantExpect(t, read, "customer.name", "${steps.create_customer_as_clerk.request.name}")
}

func TestGapsNameAnRPCEveryRoleMayCallThatNoChainCallsAsAProfile(t *testing.T) {
	cat, lib := shopDemo(t)
	chains := []*chain.Chain{{Name: "a", Steps: []*chain.Step{
		{ID: "create_customer", Call: "shop.customers.v1.CustomerService/CreateCustomer"},
		{ID: "get_customer", Call: "shop.customers.v1.CustomerService/GetCustomer"},
		{ID: "get_customer_as_clerk", Call: "shop.customers.v1.CustomerService/GetCustomer", Auth: "clerk"},
	}}}
	got := map[string]bool{}
	for _, g := range contract.AuthProbeGaps(chains, lib, cat, clerkOptions()) {
		got[g.Kind+" "+g.RPC+" "+g.Profile] = true
	}
	if !got["parity shop.customers.v1.CustomerService/CreateCustomer clerk"] {
		t.Fatalf("CreateCustomer is never called as clerk: want a parity gap, got %v", got)
	}
	if got["parity shop.customers.v1.CustomerService/GetCustomer clerk"] {
		t.Fatalf("GetCustomer is called as clerk: no parity gap, got %v", got)
	}
}
