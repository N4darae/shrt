package catalog_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestAnUnknownEnumNameNextToAnAddedFieldIsKeptNotZeroed(t *testing.T) {
	cat := catalogtest.Listing()
	m, err := cat.Lookup("WidgetService/ListWidgets")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"result":{"code":"OK"},"widgets":[{"id":"w1","status":"WIDGET_STATE_REFUNDED","refundedAt":"x"},{"id":"w2","status":"WIDGET_STATE_OPEN"}]}`)
	full, present, unknown, err := cat.CanonicalizeDiscardingUnknown(m.Output(), body)
	if err == nil || len(unknown) != 1 || unknown[0] != "widgets[].refundedAt" {
		t.Fatalf("the added field is named: unknown=%v err=%v", unknown, err)
	}
	for name, raw := range map[string][]byte{"full": full, "present": present} {
		var v struct {
			Widgets []struct {
				Status string `json:"status"`
			} `json:"widgets"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		if len(v.Widgets) != 2 || v.Widgets[0].Status != "WIDGET_STATE_REFUNDED" || v.Widgets[1].Status != "WIDGET_STATE_OPEN" {
			t.Fatalf("%s: an enum value the backend sent must not be recorded as another value: %s", name, raw)
		}
	}
	enums := catalog.UnknownEnumValues(m.Output(), body)
	if len(enums) != 1 || enums[0].Path != "widgets.0.status" || enums[0].Value != "WIDGET_STATE_REFUNDED" {
		t.Fatalf("the unknown enum value must be named with its path: %+v", enums)
	}
}
