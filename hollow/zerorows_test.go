package hollow_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/hollow"
)

func TestAListWhoseOnlyItemsAreAllZeroIsEmpty(t *testing.T) {
	for _, body := range []string{
		`{"error":{"code":"OK"},"products":[{}]}`,
		`{"error":{"code":"OK"},"products":[{"id_product":"","qty_on_hand":"0","price":0,"tags":[]}]}`,
		`{"error":{"code":"OK"},"products":[{},{"id_product":"","nested":{"x":""}}]}`,
		`{"error":{"code":"OK"},"product":{"lines":[{}]}}`,
	} {
		if !hollow.BodyIsEmpty(json.RawMessage(body)) {
			t.Errorf("every item of the list is at its zero value, so the read found nothing, exactly as "+
				"product: {} and [] do; want empty: %s", body)
		}
	}
	for _, body := range []string{
		`{"error":{"code":"OK"},"products":[{"id_product":"p-1"}]}`,
		`{"error":{"code":"OK"},"counts":[0]}`,
	} {
		if hollow.BodyIsEmpty(json.RawMessage(body)) {
			t.Errorf("want non-empty: %s", body)
		}
	}
}
