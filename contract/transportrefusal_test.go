package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestChainBodyLint_ATransportRefusalProbeIsLeftAlone(t *testing.T) {
	for name, e := range map[string]chain.Expectation{
		"code equals":         {Path: "transport.code", Equals: "invalid_argument"},
		"code not ok":         {Path: "transport.code", NotEqual: chain.TransportOK},
		"status equals 400":   {Path: "transport.http_status", Equals: 400},
		"status not_equal ok": {Path: "transport.http_status", NotEqual: 200},
	} {
		t.Run(name, func(t *testing.T) {
			c := stepChain(&chain.Step{
				ID: "reject_empty_name", Call: "ThingService/Create", SkipAuth: true,
				Body:   map[string]any{"name": "", "kind": "KIND_A"},
				Expect: []chain.Expectation{e},
			})
			if got := bodyIssues(t, c, libraryRequiring("name")); len(got) != 0 {
				t.Fatalf("a backend that refuses a shape error as a Connect error is probed at "+
					"transport.*, and that probe expects a refusal as surely as one on the envelope: %+v", got)
			}
		})
	}
}

func TestChainBodyLint_ATransportSuccessAssertionStillExpectsSuccess(t *testing.T) {
	c := stepChain(&chain.Step{
		ID: "create", Call: "ThingService/Create", SkipAuth: true,
		Body:   map[string]any{"name": "", "kind": "KIND_A"},
		Expect: []chain.Expectation{{Path: "transport.code", Equals: chain.TransportOK}},
	})
	if got := bodyIssues(t, c, libraryRequiring("name")); len(got) != 1 {
		t.Fatalf("transport.code equals ok says the call should succeed; the empty required field is still an error: %+v", got)
	}
}
