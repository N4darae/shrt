package contract

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSetBodyPathFillsRepeatedMessageElement(t *testing.T) {
	body := map[string]any{
		"id_book": "",
		"lines":   []any{map[string]any{"id_give_asset": "", "id_get_asset": ""}},
	}
	setBodyPath(body, "lines.id_give_asset", "${create_asset_give.id_asset}")
	setBodyPath(body, "lines.id_get_asset", "${create_asset_get.id_asset}")

	want := []any{map[string]any{
		"id_give_asset": "${create_asset_give.id_asset}",
		"id_get_asset":  "${create_asset_get.id_asset}",
	}}
	if !reflect.DeepEqual(body["lines"], want) {
		t.Fatalf("lines composed as %#v, want %#v", body["lines"], want)
	}
}

func TestSetBodyPathHonoursAnExplicitIndex(t *testing.T) {
	body := map[string]any{
		"lines": []any{map[string]any{"qty": ""}, map[string]any{"qty": ""}},
	}
	setBodyPath(body, "lines.1.qty", "7")

	list := body["lines"].([]any)
	if got := list[1].(map[string]any)["qty"]; got != "7" {
		t.Fatalf("lines.1.qty = %v, want 7", got)
	}
	if got := list[0].(map[string]any)["qty"]; got != "" {
		t.Fatalf("lines.0.qty = %v, want it untouched", got)
	}
}

func TestSetBodyPathLeavesAnOutOfRangeIndexAlone(t *testing.T) {
	body := map[string]any{"lines": []any{map[string]any{"qty": ""}}}
	setBodyPath(body, "lines.4.qty", "7")

	if got := len(body["lines"].([]any)); got != 1 {
		t.Fatalf("lines grew to %d elements, want 1", got)
	}
}

func TestSetBodyPathStillNestsPlainMessages(t *testing.T) {
	body := map[string]any{"meta": map[string]any{"source": ""}}
	setBodyPath(body, "meta.source", "shrt")
	setBodyPath(body, "meta.trace_id", "abc")

	want := map[string]any{"source": "shrt", "trace_id": "abc"}
	if !reflect.DeepEqual(body["meta"], want) {
		t.Fatalf("meta composed as %#v, want %#v", body["meta"], want)
	}
}

func TestSetBodyPathReplacesAScalarStandingWhereAMessageBelongs(t *testing.T) {
	body := map[string]any{"meta": ""}
	setBodyPath(body, "meta.source", "shrt")

	want := map[string]any{"source": "shrt"}
	if !reflect.DeepEqual(body["meta"], want) {
		t.Fatalf("meta composed as %#v, want %#v", body["meta"], want)
	}
}

func TestMarkUnfilledSeesATodoCarriedAsALineComment(t *testing.T) {
	raw := []byte(`apiVersion: shrt/contract/v1
domain: demo
rpcs:
    demo.v1.Svc/DoThing:
        summary: does a thing
        required: [] # TODO: which fields the server rejects without
        status: draft
    demo.v1.Svc/DoOther:
        summary: does another thing
        required: [id_thing]
        status: draft
`)
	o := &Overlay{}
	if err := yaml.Unmarshal(raw, o); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	o.Domain = "demo"
	markUnfilled(o, raw)

	unfilled := o.RPCs["demo.v1.Svc/DoThing"]
	if len(unfilled.Required) != 0 {
		t.Fatalf("required parsed as %v, want empty — the TODO is a comment, not a value", unfilled.Required)
	}
	if !unfilled.IsUnfilled("required") {
		t.Fatal("an empty required carrying a TODO comment must be reported unfilled")
	}
	if o.RPCs["demo.v1.Svc/DoOther"].IsUnfilled("required") {
		t.Fatal("a genuinely filled required must not be reported unfilled")
	}
}
