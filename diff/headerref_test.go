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
		t.Fatalf("a header reading another field is still a change: %+v", changes)
	}
}

func TestAHeaderReadingAnotherFieldIsAChainChangeLikeABodyReference(t *testing.T) {
	spot := &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "get", Call: "ThingService/Fetch", Status: runner.StatusPassed,
			Headers: map[string]string{"X-Ref": "r-${steps.cust.response.customer.id_customer}", "X-Lit": "one"}},
	}}
	rec := runOf("run", stepAs("get", runner.StatusPassed, `{}`))
	rec.Steps[0].Headers = map[string]string{"X-Ref": "r-${steps.cust.response.customer.name}", "X-Lit": "one"}

	rep := &diff.Report{RequestChanges: diff.CompareRequests(spot, rec, nil)}
	if len(rep.RequestChanges) != 1 || rep.RequestChanges[0].Detail != diff.HeaderRefDetail {
		t.Fatalf("the header template reads another field: one change naming it, got %+v", rep.RequestChanges)
	}
	if !rep.OnlyChainChanged() {
		t.Fatalf("a header reference edit is a chain change, as a body reference edit is: %+v", rep.RequestChanges)
	}

	rec.Steps[0].Headers = map[string]string{"X-Ref": "r-${steps.cust.response.customer.id_customer}", "X-Lit": "two"}
	rep = &diff.Report{RequestChanges: diff.CompareRequests(spot, rec, nil)}
	if len(rep.RequestChanges) != 1 || rep.OnlyChainChanged() {
		t.Fatalf("a literal header value changed is different input, as a literal body value is: %+v", rep.RequestChanges)
	}
}
