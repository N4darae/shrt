package main

import "testing"

func TestAReadHeldBehindAHeldReadNamesTheWriteThatLostTheField(t *testing.T) {
	rec := shopRecord(
		shopStep("create", shopCreate, `{"product":{"id_product":"p1"}}`).failing("product.created_at", "t", nil),
		shopStep("get", shopGet, `{"product":{"id_product":"p1","created_at":"t"}}`, "create").heldBy("create", "product.created_at"),
		shopStep("get_as_clerk", shopGet, `{"product":{"id_product":"p1","created_at":"t"}}`, "create").heldBy("get", "product.created_at"),
	)
	write, own, cascade := blameOf(t, rec, "get_as_clerk", "status")
	if write != "create" || own != "" || cascade != "unevaluated because CreateProduct lost product.created_at" {
		t.Errorf("got write %q own %q cascade %q", write, own, cascade)
	}
}
