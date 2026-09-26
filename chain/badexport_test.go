package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAnExportOfANonexistentFieldIsALintError(t *testing.T) {
	issues := lintOf(t, &chain.Step{ID: "create", Call: "ThingService/Create",
		Body:   map[string]any{"name": "widget", "kind": "KIND_A"},
		Export: map[string]string{"thing_id": "no_such_field"}})
	for _, i := range issues {
		if i.Kind == chain.KindBadExport {
			if !i.IsError() {
				t.Fatalf("the run fails this step when the export path is missing, so lint must not pass it: %+v", i)
			}
			return
		}
	}
	t.Fatalf("an export of a field the response does not have must be reported, got %+v", issues)
}
