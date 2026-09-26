package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestLintSaysAStepThatDoesNotExistDoesNotExistAndSuggestsTheClosest(t *testing.T) {
	issues := lintOf(t,
		&chain.Step{ID: "create_customer", Call: "ThingService/Create", Body: map[string]any{"name": "w"}},
		&chain.Step{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${create_custmer.id}"}},
	)
	if len(issues) != 1 {
		t.Fatalf("want one issue, got %v", issues)
	}
	msg := issues[0].Message
	if strings.Contains(msg, "does not run before") || !strings.Contains(msg, "does not exist") || !strings.Contains(msg, `did you mean "create_customer"`) {
		t.Fatalf("a step no chain declares does not exist, and the near id is suggested: %q", msg)
	}
}

func TestLintSaysAnExportUsedBeforeItsStepIsExportedLater(t *testing.T) {
	issues := lintOf(t,
		&chain.Step{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${pid}"}},
		&chain.Step{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w"}, Export: map[string]string{"pid": "id"}},
	)
	found := ""
	for _, i := range issues {
		if strings.Contains(i.Message, "${pid}") {
			found = i.Message
		}
	}
	if !strings.Contains(found, "exported by create at step 2, which runs later") {
		t.Fatalf("an export read before its step runs says where it comes from: %q (%v)", found, issues)
	}
}
