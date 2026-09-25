package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func reorderedListRuns() (*store.SafeSpot, *runner.Record) {
	product := func(id, sku, name string) string {
		return `{"id_product":"` + id + `","sku":"` + sku + `","name":"` + name + `"}`
	}
	steps := func(tag, a, b string, reversed bool) []*runner.StepRecord {
		pa, pb := product(a, "sku-"+tag+"-a", "A"), product(b, "sku-"+tag+"-b", "B")
		list, first := pa+","+pb, pa
		if reversed {
			list, first = pb+","+pa, pb
		}
		return []*runner.StepRecord{
			{ID: "pa", Call: "S/CreateProduct", Status: runner.StatusPassed, Request: []byte(`{"sku":"sku-` + tag + `-a"}`), Response: []byte(`{"product":` + pa + `}`)},
			{ID: "pb", Call: "S/CreateProduct", Status: runner.StatusPassed, Request: []byte(`{"sku":"sku-` + tag + `-b"}`), Response: []byte(`{"product":` + pb + `}`)},
			{ID: "list", Call: "S/ListProducts", Status: runner.StatusPassed, Response: []byte(`{"products":[` + list + `]}`)},
			{ID: "get_first", Call: "S/GetProduct", Status: runner.StatusPassed, Response: []byte(`{"product":` + first + `}`)},
		}
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: steps("ln0", "prd-aaa111", "prd-bbb222", false)}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: steps("ln2", "prd-ccc333", "prd-ddd444", true)}
	return spot, rec
}

func TestAPositionalChangeAfterAReorderShowsThisRunsFixtureNameAsWant(t *testing.T) {
	spot, rec := reorderedListRuns()
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{
		{Step: "pa", Path: "sku", Kind: diff.KindChanged, Want: "sku-ln0-a", Got: "sku-ln2-a"},
		{Step: "pb", Path: "sku", Kind: diff.KindChanged, Want: "sku-ln0-b", Got: "sku-ln2-b"},
	}
	named := func(step, path string) bool { return path == "sku" }
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Named: named, Reads: map[string][]diff.Read{"get_first": {{Step: "list", Path: "products.0.id_product"}}}})
	text := rep.Text()
	if strings.Contains(text, "want=sku-ln0-a") {
		t.Fatalf("the want of a positional change is the safe spot's value with this run's fixture names: sku-ln2-a, not sku-ln0-a:\n%s", text)
	}
	if !strings.Contains(text, "product.sku want=sku-ln2-a got=sku-ln2-b") {
		t.Fatalf("want the renamed echo as want:\n%s", text)
	}
	head, _, _ := strings.Cut(text, "\n  ")
	listed := strings.Count(text, "\n  [")
	counted := len(rep.Changes)
	if listed != counted && !strings.Contains(text, "not listed one by one") {
		t.Fatalf("the header counts %d change(s) and %d are listed: say how many are hidden and how to see them:\n%s\n---\n%s", counted, listed, head, text)
	}
}
