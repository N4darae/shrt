package diff_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestRespelledReferenceIsNotAChainChange(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "order", Call: "S/Create", Status: runner.StatusPassed, Response: []byte(`{"order":{"id_order":"o-1"}}`)},
		{ID: "confirm", Call: "S/Confirm", Status: runner.StatusPassed, Request: []byte(`{"id_order":"o-1","note":"x o-1"}`),
			BodyRefs: map[string]string{"id_order": "${order.order.id_order}", "note": "x ${ order.order.id_order }"}},
	}}
	for _, spelled := range []map[string]any{
		{"id_order": "${steps.order.response.order.id_order}", "note": "x ${steps.order.order.id_order}"},
		{"id_order": "${order.response.order.id_order}", "note": "x ${order.order.id_order}"},
	} {
		now := &chain.Chain{Name: "c", Steps: []*chain.Step{
			{ID: "order", Call: "S/Create"},
			{ID: "confirm", Call: "S/Confirm", Body: spelled},
		}}
		if got := diff.ChainChanges(spot, now); len(got) != 0 {
			t.Errorf("%v reads the same field as the confirmed run, so it is no chain change: %+v", spelled, got)
		}
	}
	other := &chain.Chain{Name: "c", Steps: []*chain.Step{
		{ID: "order", Call: "S/Create"},
		{ID: "confirm", Call: "S/Confirm", Body: map[string]any{"id_order": "${steps.order.request.order.id_order}", "note": "x ${order.order.id_order}"}},
	}}
	if got := diff.ChainChanges(spot, other); len(got) != 1 {
		t.Errorf("reading the request instead of the response is a chain change: %+v", got)
	}
}
