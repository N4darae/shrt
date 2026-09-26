package diff

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func latStep(id, call string, ms int64, resent ...int64) *runner.StepRecord {
	return &runner.StepRecord{ID: id, Call: call, Status: runner.StatusPassed, HTTPStatus: 200, LatencyMS: ms, LatencyResent: resent}
}

func TestLatencyFlagsAStepSlowerThanBothTheFloorAndTheRatio(t *testing.T) {
	spot := []*runner.StepRecord{latStep("create", "P/CreateProduct", 2), latStep("list", "P/ListProducts", 1)}
	rec := &runner.Record{Steps: []*runner.StepRecord{latStep("create", "P/CreateProduct", 3), latStep("list", "P/ListProducts", 702, 701, 703)}}
	flags := LatencyRegressions(spot, rec, nil, LatencyPolicyFrom(nil))
	if len(flags) != 1 || flags[0].Step != "list" || !flags[0].Confirmed || flags[0].AfterMS != 701 || flags[0].BeforeMS != 1 {
		t.Fatalf("want one confirmed flag on list judged on its fastest sample, got %+v", flags)
	}
	line := flags[0].Line()
	for _, want := range []string{"LATENCY", "ListProducts", "list", "1ms", "701ms", "+700ms"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q lacks %q", line, want)
		}
	}
}

func TestLatencyIgnoresASlowCallThatAReMeasurementAnsweredFast(t *testing.T) {
	spot := []*runner.StepRecord{latStep("list", "P/ListProducts", 1)}
	rec := &runner.Record{Steps: []*runner.StepRecord{latStep("list", "P/ListProducts", 900, 2)}}
	if flags := LatencyRegressions(spot, rec, nil, LatencyPolicyFrom(nil)); len(flags) != 0 {
		t.Fatalf("one slow call answered fast when re-sent is noise, got %+v", flags)
	}
}

func TestLatencyNeedsBothTheFloorAndTheRatio(t *testing.T) {
	p := LatencyPolicyFrom(nil)
	if p.Exceeds(1, 200) {
		t.Fatal("under the 250ms floor is not flagged")
	}
	if p.Exceeds(400, 900) {
		t.Fatal("+500ms but under 3x is not flagged")
	}
	if !p.Exceeds(100, 400) {
		t.Fatal("+300ms and 4x is flagged")
	}
	floor, ratio := int64(50), 2.0
	p = LatencyPolicyFrom(&config.Latency{FloorMS: &floor, Ratio: &ratio})
	if !p.Exceeds(40, 100) {
		t.Fatal("configured floor and ratio apply")
	}
}

func TestLatencyOnAWriteIsConfirmedOnlyByThePreviousRun(t *testing.T) {
	spot := []*runner.StepRecord{latStep("create", "P/CreateProduct", 2)}
	rec := &runner.Record{Steps: []*runner.StepRecord{latStep("create", "P/CreateProduct", 800)}}
	flags := LatencyRegressions(spot, rec, nil, LatencyPolicyFrom(nil))
	if len(flags) != 1 || flags[0].Confirmed {
		t.Fatalf("a write measured once is reported unconfirmed, got %+v", flags)
	}
	prev := &runner.Record{RunID: "r1", Steps: []*runner.StepRecord{latStep("create", "P/CreateProduct", 790)}}
	flags = LatencyRegressions(spot, rec, prev, LatencyPolicyFrom(nil))
	if len(flags) != 1 || !flags[0].Confirmed || !strings.Contains(flags[0].Line(), "r1") {
		t.Fatalf("a previous run slow at the same step confirms it, got %+v", flags)
	}
}

func TestLatencyOffFlagsNothing(t *testing.T) {
	spot := []*runner.StepRecord{latStep("list", "P/ListProducts", 1)}
	rec := &runner.Record{Steps: []*runner.StepRecord{latStep("list", "P/ListProducts", 900, 900, 900)}}
	if flags := LatencyRegressions(spot, rec, nil, LatencyPolicyFrom(&config.Latency{Off: true})); len(flags) != 0 {
		t.Fatalf("latency: {off: true} turns the check off, got %+v", flags)
	}
}
