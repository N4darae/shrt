package contract_test

import (
	"strings"
	"testing"
)

func TestPlanReplaysAnIdempotencyKeyAfterEachStateTransition(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "CreateOrder")
	for _, tr := range []struct{ rpc, state string }{{"confirm_order", "ORDER_STATUS_CONFIRMED"}, {"cancel_order", "ORDER_STATUS_CANCELLED"}} {
		fixture := "create_order_for_replay_after_" + tr.rpc
		move := planStep(t, p, tr.rpc+"_for_replay")
		if got := bodyAt(t, move, "id_order"); got != "${"+fixture+".order.id_order}" {
			t.Fatalf("%s moves %s, got %s:\n%s", move.ID, fixture, got, text)
		}
		wantExpect(t, move, "order.status", tr.state)
		read := planStep(t, p, "fetch_order_after_"+tr.rpc+"_for_replay")
		if got := bodyAt(t, read, "id_order"); got != "${"+fixture+".order.id_order}" {
			t.Fatalf("%s reads %s, got %s:\n%s", read.ID, fixture, got, text)
		}
		replay := planStep(t, p, "create_order_replay_after_"+tr.rpc)
		if got := bodyAt(t, replay, "idempotency_key"); got != "${steps."+fixture+".request.idempotency_key}" {
			t.Fatalf("%s sends %s's key, got %s:\n%s", replay.ID, fixture, got, text)
		}
		wantExpect(t, replay, "order.id_order", "${"+fixture+".order.id_order}")
		wantExpect(t, replay, "order.status", "${"+read.ID+".order.status}")
		wantExpect(t, replay, "order.total_minor", "${"+read.ID+".order.total_minor}")
		ids := stepIDs(p)
		if !(stepIndex(p.Chain, fixture) < stepIndex(p.Chain, move.ID) && stepIndex(p.Chain, move.ID) < stepIndex(p.Chain, read.ID) &&
			stepIndex(p.Chain, read.ID) < stepIndex(p.Chain, replay.ID)) {
			t.Fatalf("fixture, move, read, replay in that order: %s", strings.Join(ids, ", "))
		}
		after := planStep(t, p, "fetch_order_after_"+replay.ID)
		if got := bodyAt(t, after, "id_order"); got != "${"+replay.ID+".order.id_order}" {
			t.Fatalf("%s reads what %s answered, got %s:\n%s", after.ID, replay.ID, got, text)
		}
		wantExpect(t, after, "order.status", tr.state)
		if stepIndex(p.Chain, after.ID) != stepIndex(p.Chain, replay.ID)+1 {
			t.Fatalf("the record is read right after the replay: %s", strings.Join(ids, ", "))
		}
	}
	if !strings.Contains(notes, "create_order_replay_after_confirm_order") {
		t.Fatalf("a note names the replays after a transition: %s", notes)
	}
}
