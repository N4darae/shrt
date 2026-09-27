package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestKeyFieldForReadsIdsUniqueFailuresNotesAndIdempotencyKeys(t *testing.T) {
	lib := contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/contract/v1",
		Domain:     "test",
		RPCs: map[string]*contract.RPCContract{
			"shop.v1.CustomerService/CreateCustomer": {
				Fields: map[string]*contract.FieldContract{
					"email": {Note: "must contain @"},
					"sku":   {Note: "unique per catalog"},
					"name":  {Note: "free text"},
				},
				Failures: []contract.Failure{{Code: 1101, Reason: "EmailTaken", Field: "email"}},
			},
		},
	}})
	key := contract.KeyFieldFor(lib)
	for field, want := range map[string]bool{"email": true, "sku": true, "id_customer": true, "idempotency_key": true, "name": false} {
		if got, known := key("shop.v1.CustomerService/CreateCustomer", field); !known || got != want {
			t.Errorf("%s: key=%v known=%v, want key=%v", field, got, known, want)
		}
	}
	if _, known := key("shop.v1.OtherService/Create", "email"); known {
		t.Error("an rpc without a contract is unknown, so the caller falls back to comparing any field")
	}
}
