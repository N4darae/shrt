package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

const (
	listSpotCreateW = `{"product":{"id_product":"prd-f9ef06e736d5","sku":"sku-w","price_minor":"1250"}}`
	listSpotCreateG = `{"product":{"id_product":"prd-bee2dc1a437c","sku":"sku-g","price_minor":"799"}}`
	listSpotList    = `{"products":[{"id_product":"prd-bee2dc1a437c","sku":"sku-g","price_minor":"799"},{"id_product":"prd-f9ef06e736d5","sku":"sku-w","price_minor":"1250"}]}`
	listRunCreateW  = `{"product":{"id_product":"prd-0066f622803d","sku":"sku-w","price_minor":"1250"}}`
	listRunCreateG  = `{"product":{"id_product":"prd-1bcacf4cff60","sku":"sku-g","price_minor":"799"}}`
	listRunReversed = `{"products":[{"id_product":"prd-0066f622803d","sku":"sku-w","price_minor":"1250"},{"id_product":"prd-1bcacf4cff60","sku":"sku-g","price_minor":"799"}]}`
)

func listSpot() *store.SafeSpot {
	return &store.SafeSpot{Chain: "shop", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "create_w", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(listSpotCreateW)},
		{ID: "create_g", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(listSpotCreateG)},
		{ID: "list", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(listSpotList)},
	}}
}

func listRun(list string, unordered []string) *runner.Record {
	rec := runOf("run",
		stepAs("create_w", runner.StatusPassed, listRunCreateW),
		stepAs("create_g", runner.StatusPassed, listRunCreateG),
		stepAs("list", runner.StatusPassed, list))
	rec.Steps[2].Unordered = unordered
	return rec
}

func TestAnUnorderedListIsComparedAsAMultisetWithIdsPairedByContent(t *testing.T) {
	rep := diff.Compare(listSpot(), listRun(listRunReversed, []string{"products"}))
	if !rep.Clean() {
		t.Fatalf("a list declared unordered holding the same items in another order is no drift:\n%s", rep.Text())
	}
	changed := `{"products":[{"id_product":"prd-0066f622803d","sku":"sku-w","price_minor":"1250"},{"id_product":"prd-1bcacf4cff60","sku":"sku-g","price_minor":"800"}]}`
	rep = diff.Compare(listSpot(), listRun(changed, []string{"products"}))
	if rep.Clean() || !strings.Contains(rep.Text(), "products.0.price_minor want=799 got=800") {
		t.Fatalf("a changed item of an unordered list is still drift, reported at the safe spot's position:\n%s", rep.Text())
	}
}

func TestAReorderedListWithoutTheDeclarationIsNamedAndPointsAtIt(t *testing.T) {
	rep := diff.Compare(listSpot(), listRun(listRunReversed, nil))
	if rep.Clean() {
		t.Fatal("without the declaration the order is compared, so another order is drift")
	}
	text := rep.Text()
	if !strings.Contains(text, "list products") || !strings.Contains(text, "unordered: [products]") {
		t.Fatalf("verify must say the list holds the same items in another order and name the declaration:\n%s", text)
	}
	if !rep.OnlyReordered() {
		t.Fatalf("every change here is the reordered list:\n%s", text)
	}
	changed := `{"products":[{"id_product":"prd-0066f622803d","sku":"sku-w","price_minor":"1250"},{"id_product":"prd-1bcacf4cff60","sku":"sku-g","price_minor":"800"}]}`
	rep = diff.Compare(listSpot(), listRun(changed, nil))
	if strings.Contains(rep.Text(), "unordered: [products]") || rep.OnlyReordered() {
		t.Fatalf("a list whose items changed is not the same set in another order:\n%s", rep.Text())
	}
}

func TestAReorderAnExpectationReadByPositionIsNotOfferedUnordered(t *testing.T) {
	rec := listRun(listRunReversed, nil)
	rec.Steps[2].Status = runner.StatusFailed
	rec.Steps[2].Expect = []chain.ExpectResult{{Path: "products.0.sku", Rule: "equals", Want: "sku-g", Got: "sku-w"}}
	text := diff.Compare(listSpot(), rec).Text()
	if strings.Contains(text, "unordered: [") || !strings.Contains(text, "which passed there, failed: products.0.sku") {
		t.Fatalf("an order the chain relied on and the safe spot held is a regression, not a list to declare unordered:\n%s", text)
	}
}

func TestAChainLevelUnorderedAdditionIsNamedOnce(t *testing.T) {
	rec := listRun(listRunReversed, nil)
	for _, st := range rec.Steps {
		st.Unordered = []string{"results"}
	}
	rec.Steps[2].Unordered = []string{"results", "products"}
	added := diff.UnorderedAdded(listSpot(), rec)
	if len(added) != 2 {
		t.Fatalf("a path added on every step is one chain-level line, and a step's own addition one more: %q", added)
	}
	if !strings.Contains(added[0], "`unordered: [results]` at chain level") || !strings.Contains(added[1], "`unordered: [products]` on step list") {
		t.Fatalf("want the chain-level addition once and the step's own addition by step: %q", added)
	}
}

func TestAChangedItemOfAnUnorderedListNamesItsIndexInTheReplay(t *testing.T) {
	a := `{"id_product":"prd-aaa111","name":"A","price_minor":"100"}`
	b := `{"id_product":"prd-bbb222","name":"B","price_minor":"200"}`
	moved := `{"id_product":"prd-aaa111","name":"A","price_minor":"101"}`
	step := func(list string) []*runner.StepRecord {
		return []*runner.StepRecord{{ID: "list", Call: "S/ListProducts", Status: runner.StatusPassed, Unordered: []string{"products"},
			Response: []byte(`{"products":[` + list + `]}`)}}
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: step(a + "," + b)}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Steps: step(b + "," + moved)}
	rep := diff.CompareMasking(spot, rec, nil)
	if len(rep.Changes) != 1 {
		t.Fatalf("one item changed: %v", rep.Changes)
	}
	text := rep.Text()
	if !strings.Contains(text, "products.1") {
		t.Fatalf("the changed item sits at products.1 in the replay; say so:\n%s", text)
	}
	if !strings.Contains(text, "products.0.price_minor") {
		t.Fatalf("keep the safe spot's index too, so both are named:\n%s", text)
	}
}

func reorderedFailingRun() *runner.Record {
	rec := listRun(listRunReversed, nil)
	list := rec.Steps[2]
	list.Status = runner.StatusFailed
	list.Expect = []chain.ExpectResult{{Path: "products.0.sku", Rule: "equals", Want: "sku-g", Got: "sku-w"}}
	rec.Status = runner.StatusFailed
	return rec
}

func TestAReorderWithAnOrderSensitiveExpectationIsAnOrderChange(t *testing.T) {
	rec := reorderedFailingRun()
	rep := diff.Compare(listSpot(), rec)
	rep.SeparateInput(listSpot(), rec, nil, diff.Fixtures{})
	text := rep.Text()
	if !rep.OnlyReordered() {
		t.Fatalf("the list holds the same items and only the expectation reading it by position failed: an order change:\n%s", text)
	}
	if strings.Contains(text, "not renamed consistently") {
		t.Fatalf("items that were merely reordered get no id-renaming lines:\n%s", text)
	}
	if !strings.Contains(text, "products.0.sku want=sku-g got=sku-w") {
		t.Fatalf("the failed expectation is named:\n%s", text)
	}
	if rep.FirstFailure != "step 0 list (failed): expectation failed: products.0.sku want=sku-g got=sku-w" {
		t.Fatalf("the first failing step names the expectation that failed, not only its status:\n%s", text)
	}
}

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

func TestAReorderedListIsNamedOnceAsStepThenPath(t *testing.T) {
	rep := diff.Compare(listSpot(), listRun(listRunReversed, nil))
	rep.SeparateInput(listSpot(), listRun(listRunReversed, nil), nil, diff.Fixtures{})
	text := rep.Text()
	if n := strings.Count(text, "same items in another order"); n != 1 {
		t.Fatalf("the reordered list must be named once, got %d:\n%s", n, text)
	}
	if !strings.Contains(text, "list products: same items in another order") {
		t.Fatalf("the line must read `<step> <path>: same items in another order`:\n%s", text)
	}
	if len(rep.Reordered) != 1 {
		t.Fatalf("one reordered list, got %v", rep.Reordered)
	}
}

func TestAReorderedListIsClassedOrderChangedWhateverElseDrifted(t *testing.T) {
	for _, other := range []bool{false, true} {
		spot, rec := listSpot(), listRun(listRunReversed, nil)
		if other {
			rec.Steps[0].Response = []byte(`{"id_product":"prd-0066f622803d","sku":"sku-w-changed"}`)
		}
		rep := diff.Compare(spot, rec)
		classes := map[string]string{}
		for _, c := range rep.Changes {
			classes[c.Step] = rep.Class(c)
		}
		if classes["list"] != "order changed" || other && classes["create_w"] != "regression" {
			t.Errorf("with another change %v: the reorder is order changed, the other a regression, got %v", other, classes)
		}
	}
}

func TestAListReorderedAtSeveralStepsIsExplainedOncePerPath(t *testing.T) {
	spot := listSpot()
	spot.Steps = append(spot.Steps, &runner.StepRecord{ID: "list_2", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(listSpotList)})
	rec := listRun(listRunReversed, nil)
	rec.Steps = append(rec.Steps, stepAs("list_2", runner.StatusPassed, listRunReversed))
	rep := diff.Compare(spot, rec)
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{})
	text := rep.Text()
	if n := strings.Count(text, "same items in another order"); n != 1 || !strings.Contains(text, "  list, list_2 products: same items in another order") ||
		!strings.Contains(text, "declare `unordered: [products]` on those steps") {
		t.Fatalf("one paragraph for the path, naming both steps, got %d:\n%s", n, text)
	}
	if got := rep.ReorderedLists(); len(got) != 1 || got[0] != "list, list_2 products" {
		t.Fatalf("one reordered list per path, got %v", got)
	}
}

func TestAStatusChangeIsNotListedBesideTheChangesAtItsStep(t *testing.T) {
	spot := spotOf(nil, &runner.StepRecord{ID: "get", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":"3"}`)})
	rec := recOf(&runner.StepRecord{ID: "get", Call: "ThingService/Fetch", Status: runner.StatusFailed, Response: json.RawMessage(`{"n":"2"}`)})
	text := diff.Compare(spot, rec).Text()
	if strings.Contains(text, "want=passed got=failed") || !strings.Contains(text, "[get] changed    n want=3 got=2") {
		t.Fatalf("the changed field says why the step failed; its status line only repeats it:\n%s", text)
	}
}

func TestAnItemMovedInAListThatAlsoChangedIsNamedMovedByItsID(t *testing.T) {
	reversedAndRepriced := strings.ReplaceAll(listRunReversed, `"1250"`, `"999"`)
	rep := diff.Compare(listSpot(), listRun(reversedAndRepriced, nil))
	if rep.Class(diff.Change{Step: "list", Path: "products.0.price_minor"}) == "order changed" {
		t.Fatalf("a reorder with a changed price is no pure reorder")
	}
	if !rep.Moved("list", "products.0.price_minor") || !rep.Moved("list", "products.1.sku") {
		t.Errorf("the item the safe spot held at each position now sits at another one")
	}
	inPlace := strings.ReplaceAll(listSpotList, `"1250"`, `"999"`)
	inPlace = strings.ReplaceAll(strings.ReplaceAll(inPlace, "prd-bee2dc1a437c", "prd-1bcacf4cff60"), "prd-f9ef06e736d5", "prd-0066f622803d")
	rep = diff.Compare(listSpot(), listRun(inPlace, nil))
	if rep.Moved("list", "products.1.price_minor") {
		t.Errorf("an item repriced in place did not move")
	}
}

func TestALineDroppedFromLinesOfOneProductIsNamedDropped(t *testing.T) {
	spot := &store.SafeSpot{Chain: "orders", RunID: "spot", Steps: []*runner.StepRecord{
		stepAs("fetch", runner.StatusPassed, `{"order":{"lines":[{"id_product":"prd-aaaa1111aaaa","qty":6},{"id_product":"prd-aaaa1111aaaa","qty":5}]}}`),
	}}
	rec := runOf("run", stepAs("fetch", runner.StatusFailed, `{"order":{"lines":[{"id_product":"prd-aaaa1111aaaa","qty":6}]}}`))
	text := diff.Compare(spot, rec).Text()
	if !strings.Contains(text, "(0 added, 1 dropped, by id_product; dropped prd-aaaa1111aaaa)") {
		t.Fatalf("a repeated key is matched by count, so the missing line is dropped:\n%s", text)
	}
	if strings.Contains(text, "another order") {
		t.Fatalf("a dropped line is not a reorder:\n%s", text)
	}
}

func requestStep(id, request, body string) *runner.StepRecord {
	st := step(id, body)
	st.Request = json.RawMessage(request)
	return st
}

func TestAListWhoseMembershipChangedIsOneLineNotOnePerItemId(t *testing.T) {
	owner := `{"owner":{"id_owner":"own-aaaaaaaaaaaa"}}`
	item := func(id, own string) string {
		return `{"id_item":"itm-` + id + `","id_owner":"own-` + own + `","label":"x"}`
	}
	spotList := `{"items":[` + item("111111111111", "aaaaaaaaaaaa") + `,` + item("222222222222", "aaaaaaaaaaaa") + `]}`
	spot := spotOf(nil,
		step("make_owner", owner),
		step("make_item", `{"item":`+item("111111111111", "aaaaaaaaaaaa")+`}`),
		step("make_item_2", `{"item":`+item("222222222222", "aaaaaaaaaaaa")+`}`),
		requestStep("list_items", `{"id_owner":"own-aaaaaaaaaaaa"}`, spotList))
	runList := `{"items":[` + item("999999999991", "cccccccccccc") + `,` + item("999999999992", "cccccccccccc") + `,` +
		item("333333333333", "bbbbbbbbbbbb") + `,` + item("444444444444", "bbbbbbbbbbbb") + `]}`
	rec := recOf(
		step("make_owner", `{"owner":{"id_owner":"own-bbbbbbbbbbbb"}}`),
		step("make_item", `{"item":`+item("333333333333", "bbbbbbbbbbbb")+`}`),
		step("make_item_2", `{"item":`+item("444444444444", "bbbbbbbbbbbb")+`}`),
		requestStep("list_items", `{"id_owner":"own-bbbbbbbbbbbb"}`, runList))
	text := diff.Compare(spot, rec).Text()
	if strings.Contains(text, "not renamed consistently") {
		t.Fatalf("per-index id lines of a list whose length changed must fold into its length line:\n%s", text)
	}
	want := "items want=2 item(s) got=4 item(s) (2 added, 0 dropped, by id_item; added itm-999999999991, itm-999999999992; 2 added have id_owner other than the request's own-bbbbbbbbbbbb)"
	if !strings.Contains(text, want) {
		t.Fatalf("want the membership line %q in:\n%s", want, text)
	}
}

func TestAListOfTheSameLengthWithOtherItemsSaysSoOnce(t *testing.T) {
	item := func(id string) string { return `{"id_item":"itm-` + id + `","label":"x"}` }
	spot := spotOf(nil,
		step("make_item", `{"item":`+item("111111111111")+`}`),
		step("make_item_2", `{"item":`+item("222222222222")+`}`),
		step("list_items", `{"items":[`+item("111111111111")+`,`+item("222222222222")+`]}`))
	rec := recOf(
		step("make_item", `{"item":`+item("333333333333")+`}`),
		step("make_item_2", `{"item":`+item("444444444444")+`}`),
		step("list_items", `{"items":[`+item("333333333333")+`,`+item("888888888888")+`]}`))
	rep := diff.Compare(spot, rec)
	text := rep.Text()
	if rep.Clean() || strings.Contains(text, "not renamed consistently") || !strings.Contains(text, "membership items want=2 item(s) got=2 item(s) (1 added, 1 dropped, by id_item") {
		t.Fatalf("a replaced item must be one membership line:\n%s", text)
	}
}

func TestAListThatGainedItemsTestsThePrefixFilterAndComparesItemsById(t *testing.T) {
	item := func(id, code, n string) string {
		return `{"id_item":"itm-` + id + `","code":"` + code + `","n":"` + n + `"}`
	}
	spot := spotOf(nil,
		step("make_item", `{"item":`+item("111111111111", "ab-1", "5")+`}`),
		step("make_item_2", `{"item":`+item("222222222222", "ab-2", "6")+`}`),
		requestStep("list_items", `{"code_prefix":"ab-"}`, `{"items":[`+item("111111111111", "ab-1", "5")+`,`+item("222222222222", "ab-2", "6")+`]}`))
	rec := recOf(
		step("make_item", `{"item":`+item("333333333333", "ab-1", "5")+`}`),
		step("make_item_2", `{"item":`+item("444444444444", "ab-2", "6")+`}`),
		requestStep("list_items", `{"code_prefix":"ab-"}`, `{"items":[`+item("000000000000", "AB-0", "1")+`,`+item("333333333333", "ab-1", "5")+`,`+
			item("444444444444", "ab-2", "7")+`]}`))
	text := diff.Compare(spot, rec).Text()
	if !strings.Contains(text, `(1 added, 0 dropped, by id_item; added itm-000000000000; 1 added have code not starting with code_prefix "ab-"`) {
		t.Errorf("an added item outside the prefix filter is counted:\n%s", text)
	}
	if strings.Contains(text, "items.0.") || strings.Contains(text, "items.1.") {
		t.Errorf("positional lines of a list whose items were added are dropped:\n%s", text)
	}
	if !strings.Contains(text, "items.2.n want=6 got=7") {
		t.Errorf("an item present in both, compared by its id, still reports its change:\n%s", text)
	}
}

func TestAListWhoseMembershipChangedNamesTheItemsAndTheStepsThatMadeThem(t *testing.T) {
	line := func(id, n string) string { return `{"id_line":"lin-` + id + `","n":"` + n + `"}` }
	item := func(id, lines string) string { return `{"id_item":"itm-` + id + `","lines":[` + lines + `]}` }
	made := func(id, body string) *runner.StepRecord {
		st := step(id, body)
		st.Call = "ThingService/Create"
		return st
	}
	spot := spotOf(nil,
		made("make_item", `{"item":`+item("111111111111", "")+`}`),
		made("make_item_2", `{"item":`+item("222222222222", "")+`}`),
		step("list_items", `{"items":[`+item("111111111111", line("aaaaaaaaaaa1", "1")+`,`+line("aaaaaaaaaaa2", "2"))+`,`+
			item("222222222222", line("bbbbbbbbbbb1", "3"))+`]}`))
	rec := recOf(
		made("make_item", `{"item":`+item("333333333333", "")+`}`),
		made("make_item_2", `{"item":`+item("444444444444", "")+`}`),
		step("list_items", `{"items":[`+item("444444444444", line("bbbbbbbbbbb1", "3"))+`,`+item("999999999999", line("ccccccccccc1", "9"))+`,`+
			item("888888888888", "")+`]}`))
	text := diff.Compare(spot, rec).Text()
	if !strings.Contains(text, "(2 added, 1 dropped, by id_item; dropped itm-333333333333 (make_item); added itm-999999999999, itm-888888888888)") {
		t.Errorf("the dropped and added items are named, with the step that made them:\n%s", text)
	}
	if strings.Contains(text, "items.0.lines") || strings.Contains(text, "items.1.lines") || strings.Contains(text, "items.2.lines") {
		t.Errorf("no positional line compares the nested lists of unrelated items:\n%s", text)
	}
}
