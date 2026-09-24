package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func probeStep(expect ...chain.Expectation) *chain.Step {
	return &chain.Step{ID: "probe", Call: "ThingService/Fetch", SkipAuth: true,
		Body: map[string]any{"id": "thing-1"}, Expect: expect}
}

func TestLintAcceptsTheTransportPathsWithoutADescriptorField(t *testing.T) {
	issues := lintOf(t, probeStep(
		chain.Expectation{Path: "transport.code", Equals: "unauthenticated"},
		chain.Expectation{Path: "transport.http_status", Equals: 401},
		chain.Expectation{Path: "transport.message", Contains: "token"},
	))
	if len(issues) != 0 {
		t.Fatalf("transport.* is read from the recorded transport result, not the response message, so "+
			"no descriptor field is needed: %+v", issues)
	}
}

func TestLintRefusesAnUnknownTransportPath(t *testing.T) {
	issues := lintOf(t, probeStep(chain.Expectation{Path: "transport.status", Equals: 401}))
	if len(issues) != 1 || !issues[0].IsError() {
		t.Fatalf("a misspelled transport path can never be present; want one ERROR, got %+v", issues)
	}
	for _, want := range chain.TransportFieldNames() {
		if !strings.Contains(issues[0].Message, want) {
			t.Errorf("the refusal should list %s: %q", want, issues[0].Message)
		}
	}
}

func TestLintCallsAnAlwaysPresentTransportPathUnfailable(t *testing.T) {
	for _, e := range []chain.Expectation{
		{Path: "transport.code", Exists: transportFlag(true)},
		{Path: "transport.code", NotEmpty: true},
		{Path: "transport.http_status", Exists: transportFlag(true)},
	} {
		found := false
		for _, i := range lintOf(t, probeStep(e)) {
			if i.Kind == chain.KindUnfailable {
				found = true
			}
		}
		if !found {
			t.Errorf("%s %+v holds for every answer, success or refusal: want an unfailable warning", e.Path, e)
		}
	}
}

func TestLintPointsABareConnectCodeAtTheTransportPath(t *testing.T) {
	issues := lintOf(t, probeStep(chain.Expectation{Path: "code", Equals: "unauthenticated"}))
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "transport.code") {
		t.Fatalf("the response message has no 'code'; a Connect refusal is asserted at transport.code "+
			"and the warning should say so: %+v", issues)
	}
}

func TestLintRefusesAnExportFromAnInvalidTokenProbe(t *testing.T) {
	s := probeStep(chain.Expectation{Path: "transport.code", Equals: "unauthenticated"})
	s.SkipAuth = false
	s.Auth = "invalid"
	s.Export = map[string]string{"name": "name"}
	errs := []chain.Issue{}
	for _, i := range lintOf(t, s) {
		if i.IsError() {
			errs = append(errs, i)
		}
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "export") {
		t.Fatalf("a step sent with a token the backend never issued has nothing to export: %+v", errs)
	}
}

func TestLintAcceptsAnInvalidTokenProbe(t *testing.T) {
	s := probeStep(chain.Expectation{Path: "transport.code", Equals: "unauthenticated"})
	s.SkipAuth = false
	s.Auth = "invalid"
	if issues := lintOf(t, s); len(issues) != 0 {
		t.Fatalf("auth: invalid with a transport assertion is the sanctioned probe: %+v", issues)
	}
}

func TestTransportOutcomeOfASuccessCarriesNoMessage(t *testing.T) {
	out := chain.TransportOutcome(200, "", "")
	if r := (chain.Expectation{Path: "transport.code", Equals: chain.TransportOK}).Evaluate(out); !r.Passed {
		t.Fatalf("success reads %q: %+v", chain.TransportOK, r)
	}
	if r := (chain.Expectation{Path: "transport.message", Exists: transportFlag(false)}).Evaluate(out); !r.Passed {
		t.Fatalf("a success has no transport message: %+v", r)
	}
	refused := chain.TransportOutcome(401, "unauthenticated", "token rejected")
	if r := (chain.Expectation{Path: "transport.http_status", Equals: "401"}).Evaluate(refused); !r.Passed {
		t.Fatalf("http_status compares as text like every other rule: %+v", r)
	}
}

func transportFlag(v bool) *bool { return &v }
