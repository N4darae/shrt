package contract_test

import (
	"strings"
	"testing"
)

func TestPlanForACreateWithAnIdempotencyKeyReplaysIt(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateOrder")
	base := planStep(t, p, "create_order")
	replay := planStep(t, p, "create_order_replay")
	if got := bodyAt(t, replay, "idempotency_key"); got != "${steps.create_order.request.idempotency_key}" {
		t.Fatalf("the replay sends the key create_order sent, got %s:\n%s", got, text)
	}
	wantExpect(t, replay, "order.id_order", "${create_order.order.id_order}")

	other := planStep(t, p, "create_order_replay_other_body")
	if got := bodyAt(t, other, "idempotency_key"); got != "${steps.create_order.request.idempotency_key}" {
		t.Fatalf("the replay with another body keeps the key, got %s:\n%s", got, text)
	}
	if bodyAt(t, other, "lines.0.qty") == bodyAt(t, base, "lines.0.qty") {
		t.Fatalf("the second replay changes the body:\n%s", text)
	}
	wantExpect(t, other, "order.id_order", "${create_order.order.id_order}")
	wantExpect(t, other, "order.total_minor", "${create_order.order.total_minor}")

	if !strings.Contains(notes, "create_order_replay") {
		t.Fatalf("the plan says what the replays prove: %s", notes)
	}
}
