package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
)

func TestTextPairsAMissingAndAnUnexpectedFieldHoldingOneValueAsARename(t *testing.T) {
	r := &diff.Report{SafeSpotID: "spot-1", Changes: []diff.Change{
		{Step: "create_order", Path: "order.amount_minor", Kind: diff.KindMissing, Want: float64(3400)},
		{Step: "create_order", Path: "order.total_cents", Kind: diff.KindUnexpected, Got: float64(3400)},
		{Step: "create_order", Path: "order.note", Kind: diff.KindUnexpected, Got: "x"},
		{Step: "list_orders", Path: "orders.0.amount_minor", Kind: diff.KindMissing, Want: float64(3400)},
		{Step: "list_orders", Path: "orders.0.total_cents", Kind: diff.KindUnexpected, Got: float64(9)},
	}}
	renames := r.RenamedFields()
	if len(renames) != 1 || renames[0].From != "order.amount_minor" || renames[0].To != "order.total_cents" {
		t.Fatalf("want one rename, amount_minor -> total_cents at create_order, got %+v", renames)
	}
	text := r.Text()
	for _, want := range []string{
		"[create_order] renamed    order.amount_minor -> order.total_cents (both 3400): likely a renamed field, still a change",
		"[create_order] unexpected order.note",
		"[list_orders] missing    orders.0.amount_minor",
		"[list_orders] unexpected orders.0.total_cents",
		"1 missing and unexpected field pair(s)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[create_order] missing") || strings.Contains(text, "unexpected order.total_cents") {
		t.Fatalf("the paired fields are one renamed line, not two:\n%s", text)
	}
}
