package diff_test

import (
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestARespelledHeaderReferenceIsNoChangeOfInput(t *testing.T) {
	spot := &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "get", Call: "ThingService/Fetch", Status: runner.StatusPassed,
			Headers: map[string]string{"X-Ref": "r-${steps.mk.response.product.IdProduct}", "X-Other": "o-${steps.mk.response.product.sku}"}},
	}}
	rec := runOf("run", stepAs("get", runner.StatusPassed, `{}`))
	rec.Steps[0].Headers = map[string]string{"X-Ref": "r-${steps.mk.response.product.id_product}", "X-Other": "o-${steps.mk.response.product.name}"}

	changes := diff.CompareRequests(spot, rec, nil)
	for _, c := range changes {
		if c.Path == "headers.X-Ref" {
			t.Fatalf("respelling the same field is no change (GRAMMAR section 7), got %+v", c)
		}
	}
	found := false
	for _, c := range changes {
		found = found || c.Path == "headers.X-Other"
	}
	if !found {
		t.Fatalf("a header reading another field is still a change of input: %+v", changes)
	}
}
