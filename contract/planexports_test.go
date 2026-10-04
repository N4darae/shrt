package contract_test

import (
	"strings"
	"testing"
)

func TestAPlannedChainExportsNothingNoStepReads(t *testing.T) {
	plan := thingPlan(t, "ThingService/Fetch", libraryFrom(t, thingOverlay), "thing-fetch")
	raw, err := plan.YAML()
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "${create.id}") {
		t.Fatalf("fetch reads create's response directly:\n%s", text)
	}
	if strings.Contains(text, "export:") || strings.Contains(text, "create_id") {
		t.Fatalf("an export no step reads is noise in the written chain:\n%s", text)
	}
}
