package hollow_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/hollow"
	"github.com/N4darae/shrt/runner"
)

const emptyRead = `{"error":{"code":"OK"},"assets":[],"pagination":{"current_page":"1","total":"0"}}`

func recordWith(t *testing.T, expect []chain.ExpectResult) *hollow.Report {
	t.Helper()
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		Chain: "sweep", RunID: "r1", Status: "passed",
		Steps: []*runner.StepRecord{
			step("fetch", "/acme.mdm.asset.v1.AssetService/FetchAsset", emptyRead, expect),
		},
	})
	return scan(t, runs, emptyAllow(t), nil)
}

func TestAnAbsenceAssertionDoesNotBuyAnEmptyReadOutOfTheHollowGate(t *testing.T) {
	no := false
	rep := recordWith(t, []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "assets.0.id_asset", Rule: "exists", Want: no, Got: false, Passed: true},
	})
	if rep.Unallowed != 1 {
		t.Fatalf("want 1 hollow step, got %d — 'this row is absent' cannot fail when the list is empty, "+
			"so counting it as a data assertion lets one line hide an empty read from the gate", rep.Unallowed)
	}
}

func TestAPresenceAssertionOnRealDataStillSparesTheStep(t *testing.T) {
	yes := true
	rep := recordWith(t, []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "assets.0.id_asset", Rule: "exists", Want: yes, Got: true, Passed: true},
	})
	if rep.Unallowed != 0 {
		t.Errorf("want 0 hollow steps, got %d — asserting a row IS present is a real data assertion "+
			"that fails on an empty read, which is exactly what the gate wants authors to write", rep.Unallowed)
	}
}

func TestBothDataAssertionPredicatesAgreeAboutAbsence(t *testing.T) {
	no, yes := false, true
	absence := chain.Expectation{Path: "assets.0.id_asset", Exists: &no}
	presence := chain.Expectation{Path: "assets.0.id_asset", Exists: &yes}

	c := &chain.Chain{Name: "sweep", Steps: []*chain.Step{
		{ID: "absent", Call: "ThingService/Fetch", Expect: []chain.Expectation{absence}},
		{ID: "present", Call: "ThingService/Fetch", Expect: []chain.Expectation{presence}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	asserted := hollow.DataAsserted([]*chain.Chain{c})

	if asserted["sweep\x00absent"] || asserted["sweep/absent"] {
		t.Error("DataAsserted counts an absence assertion as data. assertsOnlyEnvelope does not, and the " +
			"two answer the same question from different inputs — when they disagree the chain-source one " +
			"wins and one line of YAML hides an empty read from the gate")
	}
	found := false
	for k, v := range asserted {
		if v && len(k) > 0 && k[len(k)-len("present"):] == "present" {
			found = true
		}
	}
	if !found {
		t.Error("asserting a row IS present must still count as data: it fails on an empty read, which " +
			"is exactly the assertion the gate wants authors to write")
	}
}

func TestExistsTrueOnAnEmptyListDoesNotRescueTheStep(t *testing.T) {
	yes := true
	rep := recordWith(t, []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "assets", Rule: "exists", Want: yes, Got: true, Passed: true},
	})
	if rep.Unallowed != 1 {
		t.Fatalf("want 1 hollow step, got %d — the read returned an empty list and 'exists: true' passed "+
			"on it, so the assertion discriminated nothing. hollow only inspects PASSED steps, so any "+
			"assertion it sees necessarily passed: exists can never be the one that catches emptiness, "+
			"which GRAMMAR says outright (not_empty is false on an empty list, exists is not)", rep.Unallowed)
	}
}

func TestPinningAValueStillRescuesTheStep(t *testing.T) {
	rep := recordWith(t, []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "pagination.total", Rule: "equals", Want: "0", Got: "0", Passed: true},
	})
	if rep.Unallowed != 0 {
		t.Errorf("want 0 hollow steps, got %d — 'total equals 0' fails the moment the read returns a row, "+
			"so it is an author deliberately pinning emptiness, not an accident", rep.Unallowed)
	}
}

func TestExistsOnTheContainerDoesNotRescueButExistsIntoItDoes(t *testing.T) {
	yes := true
	container := recordWith(t, []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "assets", Rule: "exists", Want: yes, Got: true, Passed: true},
	})
	if container.Unallowed != 1 {
		t.Errorf("exists:true on the LIST itself passes whether or not it holds rows, so it discriminates "+
			"nothing and must not rescue the step; got %d hollow", container.Unallowed)
	}
	into := recordWith(t, []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "assets.0.id_asset", Rule: "exists", Want: yes, Got: true, Passed: true},
	})
	if into.Unallowed != 0 {
		t.Errorf("exists:true on a path INTO the list fails the moment the list is empty, so it is a real "+
			"data assertion and must still rescue the step; got %d hollow", into.Unallowed)
	}
}

func TestNotEqualDoesNotRescueAnEmptyRead(t *testing.T) {
	rep := recordWith(t, []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "assets", Rule: "not_equal", Want: "NEVER_THIS", Got: nil, Passed: true},
	})
	if rep.Unallowed != 1 {
		t.Fatalf("want 1 hollow step, got %d — not_equal against a value the field can never hold passes "+
			"on an empty list and on a full one alike, so it discriminates nothing and must not take the "+
			"step out of the population", rep.Unallowed)
	}
}
