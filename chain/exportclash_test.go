package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAnExportWrittenByTwoStepsIsReported(t *testing.T) {
	issues := lintOf(t,
		&chain.Step{ID: "first", Call: "ThingService/Create", Body: map[string]any{"name": "a", "kind": "KIND_A"},
			Export: map[string]string{"tid": "id"}},
		&chain.Step{ID: "second", Call: "ThingService/Create", Body: map[string]any{"name": "b", "kind": "KIND_A"},
			Export: map[string]string{"tid": "id"}},
		&chain.Step{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${tid}"}},
	)
	for _, i := range issues {
		if i.Step == "second" && i.Kind == chain.KindExportOverwritten && strings.Contains(i.Message, `"first"`) {
			if chain.Promote([]chain.Issue{i}, chain.IsAssertionQualityIssue)[0].Severity != chain.SeverityError {
				t.Fatalf("-strict must fail on an export one step silently overwrites: %+v", i)
			}
			return
		}
	}
	t.Fatalf("export tid is written by two steps and the later silently wins; lint must say so, got %+v", issues)
}

func TestAnExportNamedLikeAStepIsAnError(t *testing.T) {
	issues := lintOf(t,
		&chain.Step{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "a", "kind": "KIND_A"},
			Export: map[string]string{"fetch": "id"}},
		&chain.Step{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${create.id}"}},
	)
	for _, i := range issues {
		if i.Step == "create" && i.IsError() && strings.Contains(i.Message, `export "fetch"`) && strings.Contains(i.Message, "step") {
			return
		}
	}
	t.Fatalf("an export named like a step id makes ${fetch} ambiguous; lint must reject it, got %+v", issues)
}
