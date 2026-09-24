package diff_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestAnUnapprovedPatternDoesNotListValuesThatOnlyEchoAFixtureName(t *testing.T) {
	spot := &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create", Call: "ThingService/Fetch", Status: runner.StatusPassed,
			Request: json.RawMessage(`{"sku":"sku-first"}`), Response: json.RawMessage(`{"n":1}`)},
		{ID: "list", Call: "ThingService/Fetch", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"products":[{"sku":"sku-first","qty":3}]}`)},
	}}
	rec := runOf("run",
		stepAs("create", runner.StatusPassed, `{"n":1}`),
		stepAs("list", runner.StatusPassed, `{"products":[{"sku":"sku-again","qty":4}]}`))
	rec.Steps[0].Request = json.RawMessage(`{"sku":"sku-again"}`)
	rec.Steps[1].Volatile = []string{"products"}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = diff.CompareRequests(spot, rec, nil)
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Named: func(step, path string) bool { return step == "create" && path == "sku" }})
	if !rep.Widened() {
		t.Fatal("the pattern is still unapproved")
	}
	for _, p := range rep.UnapprovedMasked {
		if p == "list products.0.sku" {
			t.Fatalf("products.0.sku only echoes the fixture name, which verify masks anyway, so the pattern hid nothing there: %v", rep.UnapprovedMasked)
		}
	}
	found := false
	for _, p := range rep.UnapprovedMasked {
		found = found || p == "list products.0.qty"
	}
	if !found {
		t.Fatalf("a real value the pattern hid is still listed: %v", rep.UnapprovedMasked)
	}
}
