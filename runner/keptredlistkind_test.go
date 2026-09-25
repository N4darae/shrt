package runner

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestANewFailureInAListSaysWhatKindOfChangeItIs(t *testing.T) {
	response, _ := json.Marshal(map[string]any{"orders": []any{
		map[string]any{"id_order": "ord-b", "status": "PENDING", "total_minor": "2750"},
		map[string]any{"id_order": "ord-c", "status": "CONFIRMED", "total_minor": "100"},
		map[string]any{"id_order": "ord-a", "status": "CANCELLED", "total_minor": "4250"},
	}})
	sr := &StepRecord{ID: "list", Status: StatusFailed, Response: response, Expect: []chain.ExpectResult{
		{Path: "orders.0.id_order", Rule: "equals", Want: "ord-a", Got: "ord-b"},
		{Path: "orders.0.status", Rule: "equals", Want: "CANCELLED", Got: "PENDING"},
		{Path: "orders.1.id_order", Rule: "equals", Want: "ord-z", Got: "ord-c"},
		{Path: "orders.2.total_minor", Rule: "equals", Want: "4000", Got: "4250"},
		{Path: "orders.3", Rule: "exists", Want: false, Got: true},
		{Path: "orders.2", Rule: "exists", Want: false, Got: true},
	}}
	_, fresh, _ := stepMismatch("list", sr, []chain.Pin{{Step: "list", Path: "orders.2"}})
	joined := strings.Join(fresh, "\n")
	for _, want := range []string{
		"orders.0.id_order want=ord-a got=ord-b (reordered: ord-a is at orders.2)",
		"orders.0.status want=CANCELLED got=PENDING (another item at orders.0, see orders.0.id_order)",
		"orders.1.id_order want=ord-z got=ord-c (item missing: no item of orders has id_order=ord-z)",
		"orders.2.total_minor want=4000 got=4250 (value changed: the item at orders.2 holds another total_minor)",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in\n%s", want, joined)
		}
	}
	if kind := ListChangeKind("list " + fresh[0]); kind != "reordered" {
		t.Errorf("kind of %q is %q", fresh[0], kind)
	}
	kinds := listChangeKinds(sr)
	if !strings.HasPrefix(kinds[4], "item added: orders holds 3 item(s)") {
		t.Errorf("an extra item: %q", kinds[4])
	}
}
