package diff_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func causalRuns(orderWas, orderNow, confirmNow, stockNow string) (*store.SafeSpot, *runner.Record) {
	steps := func(order, confirm, stock string) []*runner.StepRecord {
		return []*runner.StepRecord{
			{ID: "product", Call: "S/CreateProduct", Status: runner.StatusPassed, Response: []byte(`{"id":"p1"}`)},
			{ID: "customer", Call: "S/CreateCustomer", Status: runner.StatusPassed, Response: []byte(`{"id":"c1"}`)},
			{ID: "order", Call: "S/CreateOrder", Status: runner.StatusPassed, Response: []byte(`{"total":` + order + `}`)},
			{ID: "confirm", Call: "S/Confirm", Status: runner.StatusPassed, Response: []byte(`{"total":` + confirm + `}`)},
			{ID: "stock", Call: "S/GetProduct", Status: runner.StatusPassed, Response: []byte(`{"qty":` + stock + `}`)},
		}
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: steps(orderWas, orderWas, "7")}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: steps(orderNow, confirmNow, stockNow)}
	return spot, rec
}

func causalReads() map[string][]diff.Read {
	return map[string][]diff.Read{
		"order":   {{Step: "customer"}, {Step: "product"}},
		"confirm": {{Step: "order"}},
		"stock":   {{Step: "product"}},
	}
}

func unexplainedSteps(rep *diff.Report) string {
	out := []string{}
	for _, c := range rep.Unexplained() {
		out = append(out, c.Step+":"+c.Path)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestAnInputChangeWhoseResponseDidNotChangeExplainsNothingDownstream(t *testing.T) {
	spot, rec := causalRuns("5348", "6250", "6250", "7")
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "customer", Path: "headers.X-Trace-Note", Kind: diff.KindUnexpected, Got: "lab"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Reads: causalReads()})
	if got := unexplainedSteps(rep); got != "confirm:total,order:total" {
		t.Fatalf("customer answered as before, so its header explains no change at the steps reading it: %s\n%s", got, rep.Text())
	}
}

func TestAnInputChangeExplainsItsOwnStepAndTheStepsReadingItsChangedResponse(t *testing.T) {
	spot, rec := causalRuns("5348", "6598", "6598", "6")
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "order", Path: "lines.0.qty", Kind: diff.KindChanged, Want: "3", Got: "4"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Reads: causalReads()})
	if got := unexplainedSteps(rep); got != "stock:qty" {
		t.Fatalf("the qty explains order's total and confirm reading order, not stock, which reads only product: %s\n%s", got, rep.Text())
	}
}

func TestARequestValueReadDownstreamExplainsTheReader(t *testing.T) {
	spot, rec := causalRuns("5348", "5348", "6598", "7")
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "order", Path: "lines.0.qty", Kind: diff.KindChanged, Want: "3", Got: "4"}}
	reads := causalReads()
	reads["confirm"] = []diff.Read{{Step: "order", Request: true, Path: "lines"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Reads: reads})
	if got := unexplainedSteps(rep); got != "" {
		t.Fatalf("confirm reads order's changed request value, so its change is explained: %s\n%s", got, rep.Text())
	}
}
