package contract

import "testing"

func TestATodoUnderExportsDoesNotDeclareTheResponseField(t *testing.T) {
	shape := MethodShape{ResponseFields: []string{"qty_on_hand", "sku", "flag"}}
	c := settled(&RPCContract{
		Exports:     map[string]string{"qty_on_hand": TodoMarker + ": what a later step reads this for"},
		Terminal:    map[string]string{"sku": TodoMarker},
		SoftSignals: map[string]string{"flag": "  " + TodoMarker + ": when it is set"},
	})
	got := measureRPC("d", "svc/AddThing", c, shape, nil)
	if len(got.UndeclaredResponseFields) != 3 {
		t.Fatalf("a TODO says nothing, so all three fields are still undeclared, got %v", got.UndeclaredResponseFields)
	}

	c.Exports["qty_on_hand"] = "the stock level after the add"
	if got := measureRPC("d", "svc/AddThing", c, shape, nil); len(got.UndeclaredResponseFields) != 2 {
		t.Fatalf("an answered export declares its field, got %v", got.UndeclaredResponseFields)
	}
}
