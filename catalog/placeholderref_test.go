package catalog_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestAReferenceStandingInAWellKnownFieldLintsClean(t *testing.T) {
	cat := catalogtest.Rich()
	body := map[string]any{
		"due_at":       "${vars.when}",
		"window":       "${vars.window}",
		"update_mask":  "${vars.mask}",
		"note":         "${seed.id_order}",
		"amount_minor": "${vars.amount}",
		"flagged":      "${vars.flagged}",
		"ratio":        "${vars.ratio}",
		"metadata":     "${vars.metadata}",
		"tags":         "${vars.tags}",
		"first_line":   "${vars.line}",
	}
	c := &chain.Chain{
		APIVersion: chain.APIVersion,
		Name:       "wktrefs",
		Vars: map[string]any{
			"when": "2026-09-12T00:00:00Z", "window": "3.5s", "mask": "dueAt",
			"amount": "1200", "flagged": true, "ratio": 1.5,
			"metadata": map[string]any{}, "tags": []any{}, "line": map[string]any{},
		},
		Steps: []*chain.Step{
			{ID: "seed", Call: "OrderService/PlaceOrder"},
			{ID: "place_order", Call: "OrderService/PlaceOrder", Body: body},
		},
	}
	for _, st := range c.Steps {
		st.Expect = []chain.Expectation{{Path: "error.code", Equals: "OK"}}
	}
	for _, i := range chain.Lint(c, cat) {
		t.Errorf("a reference standing in for a value must not be linted as a shape error: %s %s", i.Severity, i.Message)
	}
}

func TestPlaceholderMatchesTheJSONFormOfTheFieldItStandsIn(t *testing.T) {
	cat := catalogtest.Rich()
	m, err := cat.Lookup("OrderService/PlaceOrder")
	if err != nil {
		t.Fatal(err)
	}
	schema := catalog.DescribeMessage(m.Input())
	fields := []string{
		"due_at", "window", "update_mask", "note", "amount_minor",
		"flagged", "ratio", "metadata", "ping", "tags", "payload", "first_line", "id_order",
	}
	body := map[string]any{}
	for _, name := range fields {
		body[name] = "${vars.x}"
	}
	probed := map[string]any{}
	raw, err := json.Marshal(chain.Probe(body, schema))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &probed); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"due_at":       `"1970-01-01T00:00:00Z"`,
		"window":       `"0s"`,
		"update_mask":  `""`,
		"note":         `""`,
		"amount_minor": `"0"`,
		"flagged":      `false`,
		"ratio":        `0`,
		"metadata":     `{}`,
		"ping":         `{}`,
		"tags":         `[]`,
		"payload":      `{}`,
		"first_line":   `{}`,
		"id_order":     `"shrt-placeholder"`,
	}
	for name, expected := range want {
		got := mustJSONString(t, probed[name])
		if got != expected {
			t.Errorf("a reference in %s stands in as %s, want %s", name, got, expected)
		}
	}
	if err := cat.ValidateInput(m, raw); err != nil {
		t.Fatalf("every placeholder must be a shape the request message accepts: %v\n%s", err, raw)
	}
}
