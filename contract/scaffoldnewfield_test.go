package contract_test

import (
	"strings"
	"testing"
)

const curatedOrderWithoutNewField = `apiVersion: shrt/contract/v1
domain: orders
description: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [lines]
        fields:
            id_customer:
                from: shop.customers.v1.CustomerService/CreateCustomer->customer.id_customer
                checked_by: app_lookup
            lines:
                note: curated note about lines
        status: draft
`

func TestContractInitScaffoldsAFieldNewToACuratedRPC(t *testing.T) {
	lib := shopLibrary(t, curatedOrderWithoutNewField)
	raw := string(shopScaffold(t, "orders", lib))
	entry := raw[strings.Index(raw, "shop.orders.v1.OrderService/CreateOrder:"):]
	if next := strings.Index(entry[1:], "\n    shop."); next >= 0 {
		entry = entry[:next+1]
	}
	if !strings.Contains(entry, "idempotency_key:") {
		t.Fatalf("a request field the curated entry does not document must be scaffolded into it:\n%s", entry)
	}
	for _, kept := range []string{"curated note about lines", "checked_by: app_lookup", "summary: records an order"} {
		if !strings.Contains(entry, kept) {
			t.Fatalf("curated content %q must be kept:\n%s", kept, entry)
		}
	}
	if strings.Count(entry, "id_customer:") != 1 || strings.Count(entry, "lines:") != 1 {
		t.Fatalf("a documented field must not be scaffolded twice:\n%s", entry)
	}
}
