package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func customerOrderSpot(cust, order, fetch string) *store.SafeSpot {
	return &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "cust", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(cust)},
		{ID: "order", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(order)},
		{ID: "fetch", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(fetch)},
	}}
}

func customerOrderRun(cust, order, fetch string) *runner.Record {
	return runOf("run",
		stepAs("cust", runner.StatusPassed, cust),
		stepAs("order", runner.StatusPassed, order),
		stepAs("fetch", runner.StatusPassed, fetch))
}

func TestAMaskedIdMustBeRenamedConsistentlyAcrossTheRecord(t *testing.T) {
	spot := customerOrderSpot(
		`{"customer":{"id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_order":"ord-819dac5f23ba","id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_order":"ord-819dac5f23ba","id_customer":"cus-5656cb156e31"}}`)

	consistent := customerOrderRun(
		`{"customer":{"id_customer":"cus-ba9876543210"}}`,
		`{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210"}}`,
		`{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210"}}`)
	if rep := diff.Compare(spot, consistent); !rep.Clean() {
		t.Fatalf("every id renamed to one new value is the same relationship, got drift:\n%s", rep.Text())
	}

	for name, fetch := range map[string]string{
		"another customer": `{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-000000000001"}}`,
		"zero customer":    `{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-0"}}`,
		"all zeros":        `{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-000000000000"}}`,
	} {
		run := customerOrderRun(
			`{"customer":{"id_customer":"cus-ba9876543210"}}`,
			`{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210"}}`,
			fetch)
		rep := diff.Compare(spot, run)
		if rep.Clean() {
			t.Errorf("%s: the order now points at a different customer than the one created, got no drift:\n%s", name, rep.Text())
			continue
		}
		text := rep.Text()
		if !strings.Contains(text, "fetch") || !strings.Contains(text, "order.id_customer") {
			t.Errorf("%s: the drift must name the step and path that broke the relationship:\n%s", name, text)
		}
	}

	merged := customerOrderRun(
		`{"customer":{"id_customer":"cus-ba9876543210"}}`,
		`{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210"}}`,
		`{"order":{"id_order":"ord-0123456789ab","id_customer":"cus-ba9876543210","id_referrer":"cus-ba9876543210"}}`)
	spotMerged := customerOrderSpot(
		`{"customer":{"id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_order":"ord-819dac5f23ba","id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_order":"ord-819dac5f23ba","id_customer":"cus-5656cb156e31","id_referrer":"cus-77777777aaaa"}}`)
	rep := diff.Compare(spotMerged, merged)
	if rep.Clean() || !strings.Contains(rep.Text(), "id_referrer") {
		t.Fatalf("two different ids of the safe spot became one value, so two relationships collapsed into one:\n%s", rep.Text())
	}
}

func TestAnUnchangedIdThatIsRenamedElsewhereIsDrift(t *testing.T) {
	spot := customerOrderSpot(
		`{"customer":{"id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_customer":"cus-5656cb156e31"}}`)
	run := customerOrderRun(
		`{"customer":{"id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_customer":"cus-5656cb156e31"}}`,
		`{"order":{"id_customer":"cus-ba9876543210"}}`)
	if rep := diff.Compare(spot, run); rep.Clean() {
		t.Fatalf("the customer id stayed the same in two steps and changed in the third, got no drift:\n%s", rep.Text())
	}
}

func TestAnIdThatBecameAllZerosIsReported(t *testing.T) {
	for _, zero := range []string{"cus-0", "cus-000000000000", "0", "00000000-0000-0000-0000-000000000000"} {
		want := `{"order":{"id_customer":"cus-5656cb156e31"}}`
		if strings.Count(zero, "-") == 4 {
			want = `{"order":{"id_customer":"3f2b8c1e-4a5d-4e6f-8a7b-1c2d3e4f5a6b"}}`
		}
		rep := diff.Compare(orderSpot(want), runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"order":{"id_customer":"`+zero+`"}}`)))
		if rep.Clean() {
			t.Errorf("an id that became %q is a zero id, not a fresh one, got no drift:\n%s", zero, rep.Text())
		}
	}
}
