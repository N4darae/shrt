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
	if got := unexplainedSteps(rep); got != "" {
		t.Fatalf("order is a write whose answer changed with its qty, so the server state after it may differ: the stock read is explained too: %s\n%s", got, rep.Text())
	}
}

func TestAnInputChangeAtAReadExplainsOnlyItsReaders(t *testing.T) {
	spot, rec := causalRuns("5348", "5348", "5348", "6")
	spot.Steps[1].Call, rec.Steps[1].Call = "S/GetCustomer", "S/GetCustomer"
	rec.Steps[1].Response = []byte(`{"id":"c2"}`)
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "customer", Path: "id", Kind: diff.KindChanged, Want: "c1", Got: "c2"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Reads: causalReads()})
	if got := unexplainedSteps(rep); got != "stock:qty" {
		t.Fatalf("a read changes no server state, so its different input explains nothing at stock, which does not read it: %s\n%s", got, rep.Text())
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

func TestAReaderIsExplainedOnlyWhenTheValueItReadChanged(t *testing.T) {
	product := func(id, price string) string {
		return `{"id_product":"` + id + `","price_minor":"` + price + `"}`
	}
	steps := func(a, b string, list []string, price string) []*runner.StepRecord {
		return []*runner.StepRecord{
			{ID: "pa", Call: "S/CreateProduct", Status: runner.StatusPassed, Response: []byte(`{"product":` + product(a, "100") + `}`)},
			{ID: "pb", Call: "S/CreateProduct", Status: runner.StatusPassed, Response: []byte(`{"product":` + product(b, "200") + `}`)},
			{ID: "list", Call: "S/ListProducts", Status: runner.StatusPassed, Response: []byte(`{"products":[` + strings.Join(list, ",") + `]}`)},
			{ID: "get_first", Call: "S/GetProduct", Status: runner.StatusPassed, Response: []byte(`{"product":` + product(a, price) + `}`)},
		}
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: steps("prd-aaa111", "prd-bbb222",
		[]string{product("prd-aaa111", "100"), product("prd-bbb222", "200")}, "100")}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: steps("prd-ccc333", "prd-ddd444",
		[]string{product("prd-ccc333", "100")}, "101")}
	reads := map[string][]diff.Read{"get_first": {{Step: "list", Path: "products.0.id_product"}}}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "list", Path: "sku_prefix", Kind: diff.KindChanged, Want: "sku-", Got: "sku-a"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Reads: reads})
	if got := unexplainedSteps(rep); got != "get_first:product.price_minor" {
		t.Fatalf("get_first read products.0.id_product, the same product after renaming, so its input did not differ: %s\n%s", got, rep.Text())
	}

	rec.Steps[2].Response = []byte(`{"products":[` + product("prd-ddd444", "200") + `]}`)
	rep = diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "list", Path: "sku_prefix", Kind: diff.KindChanged, Want: "sku-", Got: "sku-b"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Reads: reads})
	if got := unexplainedSteps(rep); got != "" {
		t.Fatalf("the first listed product is another one now, so get_first read another value: %s\n%s", got, rep.Text())
	}
}
