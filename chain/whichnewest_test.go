package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func okResponse() any {
	return map[string]any{"error": map[string]any{"code": "OK"}}
}

func matchOf(t *testing.T, hits []chain.WhichChain, chainName, step string) (chain.WhichChain, chain.WhichStep) {
	t.Helper()
	for _, h := range hits {
		if h.Chain != chainName {
			continue
		}
		for _, m := range h.Matches {
			if m.Step == step {
				return h, m
			}
		}
	}
	t.Fatalf("no match %s/%s in %+v", chainName, step, hits)
	return chain.WhichChain{}, chain.WhichStep{}
}

func TestWhichCitesTheNewestRunThatReachedTheStepEvenWhenItContradictsTheCode(t *testing.T) {
	hits := chain.Which(whichFixture(), chain.WhichQuery{Code: "1218"}, chain.WhichOptions{
		Observations: func(name string) []chain.Observation {
			if name != "short" {
				return nil
			}
			return []chain.Observation{
				{Run: "old", Step: "boom", Status: "passed", Reached: true, Response: failureResponse(float64(1218))},
				{Run: "new", Step: "boom", Status: "failed", Reached: true, Response: okResponse(), Failures: []chain.ExpectResult{
					{Path: "error.code", Rule: "equals", Want: "failed_precondition", Got: "OK"},
					{Path: "error.details.0.app_code", Rule: "equals", Want: float64(1218), Detail: "path not present in response"},
				}},
			}
		},
	})
	h, m := matchOf(t, hits, "short", "boom")
	ev := m.Observed
	if ev == nil || ev.Run != "new" {
		t.Fatalf("the newest run that reached the step must be cited whatever it got, not an older run that happened to match: %+v", ev)
	}
	if ev.Code != "OK" || ev.Status != "failed" || ev.Holds {
		t.Fatalf("the newest run got OK and failed, the evidence must say so and must not claim the code held: %+v", ev)
	}
	if ev.Asserted != "error.details.0.app_code" || ev.Path != "error.code" {
		t.Fatalf("the evidence must name the asserted path and where the code was read instead: %+v", ev)
	}
	if len(ev.Failures) != 2 || ev.Failures[0].Path != "error.code" {
		t.Fatalf("the failing expectations of the cited run must be carried through: %+v", ev.Failures)
	}
	if !h.Observed {
		t.Fatal("a chain whose run reached a matching step is observed, whatever that run got")
	}
}

func TestAContradictedStepRanksBelowOneThatIsOnlyAsserted(t *testing.T) {
	hits := chain.Which(whichFixture(), chain.WhichQuery{Code: "1218"}, chain.WhichOptions{
		Observations: func(name string) []chain.Observation {
			if name != "short" {
				return nil
			}
			return []chain.Observation{
				{Run: "new", Step: "boom", Status: "failed", Reached: true, Response: okResponse()},
			}
		},
	})
	if len(hits) != 2 || hits[0].Chain != "long" || hits[1].Chain != "short" {
		t.Fatalf("a chain whose newest reaching run did not answer 1218 is the least likely to reproduce it, got order %s, %s",
			hits[0].Chain, hits[1].Chain)
	}
	if hits[1].Matches[0].Observed == nil {
		t.Fatal("ranking it last must not hide its evidence")
	}
}

func TestWhichSaysWhenTheNewestRunDidNotReachTheStep(t *testing.T) {
	hits := chain.Which(whichFixture(), chain.WhichQuery{Code: "1218"}, chain.WhichOptions{
		Observations: func(name string) []chain.Observation {
			switch name {
			case "short":
				return []chain.Observation{
					{Run: "r1", Step: "seed", Status: "passed", Reached: true, Response: okResponse()},
					{Run: "r1", Step: "boom", Status: "passed", Reached: true, Response: failureResponse(float64(1218))},
					{Run: "r2", Step: "seed", Status: "failed", Reached: true, Response: okResponse()},
					{Run: "r2", Step: "boom", Status: "skipped"},
				}
			case "long":
				return []chain.Observation{
					{Run: "r3", Step: "a", Status: "failed", Reached: true, Response: okResponse()},
				}
			}
			return nil
		},
	})
	_, m := matchOf(t, hits, "short", "boom")
	if m.Observed == nil || m.Observed.Run != "r1" || m.Observed.NewerRuns != 1 {
		t.Fatalf("the older run that reached the step is the evidence, and one newer run did not: %+v", m.Observed)
	}
	if m.Newest == nil || m.Newest.Run != "r2" || m.Newest.Status != "skipped" {
		t.Fatalf("the newest run skipped the step and the match must say so: %+v", m.Newest)
	}
	_, m = matchOf(t, hits, "long", "boom_long")
	if m.Observed != nil {
		t.Fatalf("no run reached boom_long: %+v", m.Observed)
	}
	if m.Newest == nil || m.Newest.Run != "r3" || m.Newest.Status != "not in run" {
		t.Fatalf("a step absent from the newest run must say it was not in that run: %+v", m.Newest)
	}
}

func TestTheNewestRunReachingTheStepCarriesNoStaleNote(t *testing.T) {
	hits := chain.Which(whichFixture(), chain.WhichQuery{Code: "1218"}, chain.WhichOptions{
		Observations: func(name string) []chain.Observation {
			return []chain.Observation{
				{Run: "r1", Step: "boom", Status: "passed", Reached: true, Response: failureResponse(float64(1218))},
			}
		},
	})
	_, m := matchOf(t, hits, "short", "boom")
	if m.Newest != nil || m.Observed == nil || m.Observed.NewerRuns != 0 || !m.Observed.Holds {
		t.Fatalf("the newest run reached and answered 1218, there is nothing stale to report: %+v %+v", m.Observed, m.Newest)
	}
}

func TestWhichByRPCReadsGotFromTheAssertedPath(t *testing.T) {
	hits := chain.Which(whichFixture(), chain.WhichQuery{RPC: "pkg.Svc/Approve"}, chain.WhichOptions{
		Observations: func(name string) []chain.Observation {
			return []chain.Observation{
				{Run: "r1", Step: "boom", Status: "passed", Reached: true, Response: failureResponse(float64(1218))},
			}
		},
	})
	_, m := matchOf(t, hits, "short", "boom")
	if m.Observed == nil || m.Observed.Code != "1218" || m.Observed.Path != "error.details.0.app_code" {
		t.Fatalf("the step asserts app_code 1218, so got must be read from app_code, not from error.code: %+v", m.Observed)
	}
	a, ok := chain.PrimaryAssertion(m.Asserts, "")
	if !ok || a.Path != m.Observed.Path {
		t.Fatalf("the shown assertion and the observed path must agree: %+v vs %+v", a, m.Observed)
	}
}

func TestDescribeFailureNamesPathWantAndGot(t *testing.T) {
	got := chain.DescribeFailure(chain.ExpectResult{Path: "order.total_minor", Rule: "equals", Want: float64(14497), Got: "6250"})
	if got != "order.total_minor want=14497 got=6250" {
		t.Fatalf("got %q", got)
	}
	got = chain.DescribeFailure(chain.ExpectResult{Path: "a.b", Rule: "equals", Want: float64(1), Detail: "path not present in response"})
	if !strings.Contains(got, "want=1") || !strings.Contains(got, "path not present") {
		t.Fatalf("a missing path must say so: %q", got)
	}
}
