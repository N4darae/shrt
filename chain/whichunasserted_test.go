package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func unassertedFixture() []*chain.Chain {
	return []*chain.Chain{{
		Name: "confirm",
		Steps: []*chain.Step{
			{ID: "confirm_short", Call: "shop.v1.OrderService/ConfirmOrder", Expect: []chain.Expectation{
				{Path: "error.code", Equals: "REJECTED"},
				{Path: "error.details.0.reason", Equals: "InsufficientStock"},
			}},
		},
	}}
}

func TestWhichReportsACodeRunsObservedThatNoChainAssertsByNumber(t *testing.T) {
	obs := func(string) []chain.Observation {
		return []chain.Observation{{Run: "r1", Step: "confirm_short", Status: "passed", Reached: true, Response: map[string]any{
			"error": map[string]any{"code": "REJECTED", "details": []any{map[string]any{"app_code": float64(1305), "reason": "InsufficientStock"}}},
		}}}
	}
	q := chain.WhichQuery{Code: "1305"}
	if hits := chain.Which(unassertedFixture(), q, chain.WhichOptions{Observations: obs}); len(hits) != 0 {
		t.Fatalf("nothing asserts 1305, so there is no asserting match: %+v", hits)
	}
	seen := chain.WhichObservedUnasserted(unassertedFixture(), q, chain.WhichOptions{Observations: obs})
	if len(seen) != 1 {
		t.Fatalf("the run observed 1305 on confirm_short, want it reported, got %+v", seen)
	}
	s := seen[0]
	if s.Step != "confirm_short" || s.Path != "error.details.0.app_code" || s.Run != "r1" {
		t.Errorf("want the step, the path the code was at and the run, got %+v", s)
	}
	if !strings.Contains(s.Command, "-step confirm_short -mode pin -run r1") {
		t.Errorf("the command must reproduce that observation: %s", s.Command)
	}
	if again := chain.WhichObservedUnasserted(unassertedFixture(), chain.WhichQuery{Code: "1306"}, chain.WhichOptions{Observations: obs}); len(again) != 0 {
		t.Errorf("a code no run carried is not observed: %+v", again)
	}
}

func TestWhichByRPCRanksAChainWhoseBestStepFailedBelowOneThatPassed(t *testing.T) {
	chains := []*chain.Chain{
		{Name: "a-small", Steps: []*chain.Step{
			{ID: "fetch", Call: "shop.v1.S/Fetch", Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		}},
		{Name: "b-large", Steps: []*chain.Step{
			{ID: "make", Call: "shop.v1.S/Make"},
			{ID: "other", Call: "shop.v1.S/Make"},
			{ID: "fetch", Call: "shop.v1.S/Fetch", Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		}},
	}
	obs := func(name string) []chain.Observation {
		status := "passed"
		if name == "a-small" {
			status = "failed"
		}
		return []chain.Observation{{Run: "r-" + name, Step: "fetch", Status: status, Reached: true, Response: okResponse()}}
	}
	hits := chain.Which(chains, chain.WhichQuery{RPC: "shop.v1.S/Fetch"}, chain.WhichOptions{Observations: obs})
	if len(hits) != 2 || hits[0].Chain != "b-large" {
		t.Fatalf("the chain whose step passed is the better evidence, even though the other is smaller: %+v", hits)
	}
}
