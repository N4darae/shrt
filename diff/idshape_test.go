package diff_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func orderSpot(body string) *store.SafeSpot {
	return &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "fetch_order", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(body)},
	}}
}

func TestIdNamedFieldIsMaskedOnlyWhenBothSidesAreIdShaped(t *testing.T) {
	want := `{"order":{"id_order":"ord-819dac5f23ba","id_customer":"cus-5656cb156e31","created_at":"2026-09-24T15:59:18.1Z","total":"500"}}`
	fresh := `{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210","created_at":"2026-09-25T10:00:00Z","total":"500"}}`
	rep := diff.Compare(orderSpot(want), runOf("run", stepAs("fetch_order", runner.StatusPassed, fresh)))
	if !rep.Clean() || rep.Masked != 3 {
		t.Fatalf("fresh ids and timestamps differ every run and must be masked, masked=%d:\n%s", rep.Masked, rep.Text())
	}
	for name, got := range map[string]string{
		"empty id":        `{"order":{"id_order":"ord-0123456789ab","id_customer":"","created_at":"2026-09-25T10:00:00Z","total":"500"}}`,
		"undefined id":    `{"order":{"id_order":"ord-0123456789ab","id_customer":"undefined","created_at":"2026-09-25T10:00:00Z","total":"500"}}`,
		"null id":         `{"order":{"id_order":"ord-0123456789ab","id_customer":null,"created_at":"2026-09-25T10:00:00Z","total":"500"}}`,
		"number id":       `{"order":{"id_order":"ord-0123456789ab","id_customer":7,"created_at":"2026-09-25T10:00:00Z","total":"500"}}`,
		"missing id":      `{"order":{"id_customer":"cus-ba9876543210","created_at":"2026-09-25T10:00:00Z","total":"500"}}`,
		"empty timestamp": `{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210","created_at":"","total":"500"}}`,
		"junk timestamp":  `{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210","created_at":"never","total":"500"}}`,
	} {
		rep := diff.Compare(orderSpot(want), runOf("run", stepAs("fetch_order", runner.StatusPassed, got)))
		if rep.Clean() {
			t.Errorf("%s: a change to a value that is not id- or timestamp-shaped is drift, got no drift:\n%s", name, rep.Text())
		}
		runs := diff.CompareRuns(runOf("a", stepAs("fetch_order", runner.StatusPassed, want)), runOf("b", stepAs("fetch_order", runner.StatusPassed, got)))
		if runs.Same() {
			t.Errorf("%s: shrt diff must show it too:\n%s", name, runs.Text())
		}
	}
}

func TestNumericIdsOnBothSidesAreMasked(t *testing.T) {
	rep := diff.Compare(orderSpot(`{"order":{"id":41}}`), runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"order":{"id":42}}`)))
	if !rep.Clean() || rep.Masked != 1 {
		t.Fatalf("two numeric ids are id-shaped, masked=%d:\n%s", rep.Masked, rep.Text())
	}
	rep = diff.Compare(orderSpot(`{"order":{"id":41}}`), runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"order":{"id":0}}`)))
	if rep.Clean() {
		t.Fatalf("an id that became 0 is drift:\n%s", rep.Text())
	}
}

func TestAllLetterHexIdIsStillIdShaped(t *testing.T) {
	spot := &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create_product_2", Call: "ThingService/Fetch", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"product":{"id_product":"prd-60b05a32d153","name":"Mug"}}`)},
		{ID: "fetch_order", Call: "ThingService/Fetch", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"order":{"id_order":"ord-9e5744d2dabc","id_product":"prd-60b05a32d153"}}`)},
	}}
	run := runOf("run",
		stepAs("create_product_2", runner.StatusPassed, `{"product":{"id_product":"prd-edababebdffe","name":"Mug"}}`),
		stepAs("fetch_order", runner.StatusPassed, `{"order":{"id_order":"ord-bcabceaddfdf","id_product":"prd-edababebdffe"}}`))
	rep := diff.Compare(spot, run)
	if !rep.Clean() || rep.Masked != 3 {
		t.Fatalf("prefix plus 12 random hex characters is an id whether or not a digit happens to appear, masked=%d:\n%s", rep.Masked, rep.Text())
	}
	runs := diff.CompareRuns(runOf("a",
		stepAs("create_product_2", runner.StatusPassed, `{"product":{"id_product":"prd-60b05a32d153","name":"Mug"}}`)),
		runOf("b", stepAs("create_product_2", runner.StatusPassed, `{"product":{"id_product":"prd-edababebdffe","name":"Mug"}}`)))
	if !runs.Same() {
		t.Fatalf("shrt diff must mask it too:\n%s", runs.Text())
	}
	for name, got := range map[string]string{
		"word id":        `{"product":{"id_product":"prd-undefinedxx","name":"Mug"}}`,
		"short word":     `{"product":{"id_product":"prd-none","name":"Mug"}}`,
		"shorter hex":    `{"product":{"id_product":"prd-abcdef","name":"Mug"}}`,
		"changed name":   `{"product":{"id_product":"prd-edababebdffe","name":"Cup"}}`,
		"other prefix":   `{"product":{"id_product":"ord-edababebdffe","name":"Mug"}}`,
		"extra segment":  `{"product":{"id_product":"prd-edababebdffe-x","name":"Mug"}}`,
		"placeholder id": `{"product":{"id_product":"prd-000000000000","name":"Mug"}}`,
	} {
		one := &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: spot.Steps[:1]}
		rep := diff.Compare(one, runOf("run", stepAs("create_product_2", runner.StatusPassed, got)))
		if rep.Clean() {
			t.Errorf("%s: must still be reported as drift:\n%s", name, rep.Text())
		}
	}
}
