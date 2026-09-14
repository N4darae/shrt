package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func lookup(t *testing.T, rpc string) *catalog.Method {
	t.Helper()
	m, err := catalogtest.New().Lookup(rpc)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSuccessExpectationUsesTheEnvelopeWhenTheResponseCarriesOne(t *testing.T) {
	defer chain.SetEnvelope("", "")
	got := contract.SuccessExpectation(lookup(t, "ThingService/Create"))
	want := []chain.Expectation{{Path: "error.code", Equals: "OK"}}
	if len(got) != 1 || got[0].Path != want[0].Path || got[0].Equals != want[0].Equals {
		t.Fatalf("SuccessExpectation = %+v, want %+v", got, want)
	}
}

func TestSuccessExpectationAssertsARealFieldWhenTheBackendHasNoEnvelope(t *testing.T) {
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")

	got := contract.SuccessExpectation(lookup(t, "ThingService/Create"))
	if len(got) != 1 {
		t.Fatalf("want exactly one expectation, got %+v", got)
	}
	if got[0].Path == "status.code" {
		t.Fatal("scaffolded an assertion on a path this response does not carry: it can never pass, " +
			"and an author reads the resulting lint warning as their own mistake")
	}
	if !got[0].NotEmpty {
		t.Errorf("want not_empty on a real response field, got %+v — error.code == OK was the floor, "+
			"and a backend without an envelope must not end up below it", got[0])
	}
}

func TestSetEnvelopeDerivesTheFieldFromThePathAndFallsBackToTheDefault(t *testing.T) {
	defer chain.SetEnvelope("", "")

	chain.SetEnvelope("status.code", "SUCCESS")
	if chain.EnvelopeField() != "status" || chain.EnvelopePath() != "status.code" || chain.EnvelopeOK() != "SUCCESS" {
		t.Errorf("SetEnvelope(status.code, SUCCESS) gave field=%q path=%q ok=%q",
			chain.EnvelopeField(), chain.EnvelopePath(), chain.EnvelopeOK())
	}

	chain.SetEnvelope("", "")
	if chain.EnvelopePath() != chain.DefaultEnvelopePath || chain.EnvelopeOK() != chain.DefaultEnvelopeOK {
		t.Errorf("an empty conventions block must leave the defaults in place, got path=%q ok=%q",
			chain.EnvelopePath(), chain.EnvelopeOK())
	}
}

func TestApplyConventionsWidensWhatCountsAsARead(t *testing.T) {
	defer chain.SetReadOnlyPrefixes(nil)

	if !chain.IsReadOnlyCall("FleetService/QueryTrips") {
		t.Error("Query is not read-only by default, so a repo naming its reads Query* has every read " +
			"scaffolded as a write and offered as a producer")
	}
	contract.ApplyConventions([]string{"Obtain"}, "", "")
	if chain.IsReadOnlyCall("FleetService/QueryTrips") {
		t.Error("an explicit read_only_prefixes list must replace the default, not extend it")
	}
	if !chain.IsReadOnlyCall("FleetService/ObtainTrips") {
		t.Error("the configured prefix did not take effect")
	}
}
