package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestADriftDumpRendersObjectsAsJSONNotGoMaps(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		stepAs("create", runner.StatusPassed, `{"product":{"sku":"sku-a","price_minor":"250"},"status":{"code":"OK"}}`),
	}}
	rec := runOf("run", stepAs("create", runner.StatusFailed, `{"status":{"code":"REJECTED"}}`))
	text := diff.Compare(spot, rec).Text()
	if strings.Contains(text, "map[") {
		t.Fatalf("a changed object must render as JSON, not as a Go map:\n%s", text)
	}
	if !strings.Contains(text, `{"price_minor":"250","sku":"sku-a"}`) {
		t.Fatalf("the object must read as JSON:\n%s", text)
	}
}

func TestATimedOutStepIsNotReportedAsNotReached(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		stepAs("create", runner.StatusPassed, `{"id":"a"}`),
		stepAs("fetch", runner.StatusPassed, `{"id":"a"}`),
	}}
	create := stepAs("create", runner.StatusFailed, `{"id":"a","error":{"code":"x"}}`)
	fetch := &runner.StepRecord{ID: "fetch", Call: "ThingService/Fetch", Status: runner.StatusError,
		Error: "POST http://x/ThingService/Fetch: sent, no answer before target.timeout (1s): context deadline exceeded"}
	rec := runOf("run", create, fetch)
	text := diff.Compare(spot, rec).Text()
	if strings.Contains(text, "[fetch] not_reached") {
		t.Fatalf("a step that was sent and timed out was reached; it must not read not_reached:\n%s", text)
	}
	if !strings.Contains(text, "[fetch] status") || !strings.Contains(text, "no answer before target.timeout") {
		t.Fatalf("the timed-out step must read as a status change that says it timed out:\n%s", text)
	}
}
