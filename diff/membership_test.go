package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

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
	want := "items want=2 item(s) got=4 item(s) (2 added, 0 dropped, by id_item; 2 added have id_owner other than the request's own-bbbbbbbbbbbb; per-item ids not listed)"
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
	if !strings.Contains(text, `(1 added, 0 dropped, by id_item; 1 added have code not starting with code_prefix "ab-"`) {
		t.Errorf("an added item outside the prefix filter is counted:\n%s", text)
	}
	if strings.Contains(text, "items.0.") || strings.Contains(text, "items.1.") {
		t.Errorf("positional lines of a list whose items were added are dropped:\n%s", text)
	}
	if !strings.Contains(text, "items.2.n want=6 got=7") {
		t.Errorf("an item present in both, compared by its id, still reports its change:\n%s", text)
	}
}
