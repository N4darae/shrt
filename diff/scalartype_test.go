package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func replay(t *testing.T, confirmed, today string) *diff.Report {
	t.Helper()
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "read", Status: runner.StatusPassed, Response: json.RawMessage(confirmed)}}}
	rec := &runner.Record{Chain: "c", RunID: "today", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{ID: "read", Status: runner.StatusPassed, Response: json.RawMessage(today)}}}
	return diff.Compare(spot, rec)
}

func TestANumberThatBecomesAStringIsDrift(t *testing.T) {
	r := replay(t, `{"quantity":3}`, `{"quantity":"3"}`)

	if r.Clean() {
		t.Fatal("a backend that restates quantity 3 as \"3\" breaks every client that reads it as a " +
			"number, and verify exists to catch exactly what nobody asserted. Comparing them as formatted text made " +
			"the two identical")
	}
	if got := r.Changes[0].Kind; got != diff.KindType {
		t.Errorf("the value did not change, its type did; want %q, got %q", diff.KindType, got)
	}
}

func TestTheReportSaysWhichTypeEachSideWas(t *testing.T) {
	text := replay(t, `{"quantity":3}`, `{"quantity":"3"}`).Text()

	if !strings.Contains(text, "number 3") || !strings.Contains(text, `string "3"`) {
		t.Fatalf("printing want=3 got=3 reads like a false positive; the kinds are the whole finding: %s", text)
	}
}

func TestABooleanThatBecomesAStringIsDrift(t *testing.T) {
	if replay(t, `{"active":true}`, `{"active":"true"}`).Clean() {
		t.Fatal("true and the string \"true\" render identically as formatted text")
	}
}

func TestAStringThatBecomesNullIsDrift(t *testing.T) {
	if replay(t, `{"note":"x"}`, `{"note":null}`).Clean() {
		t.Fatal("a field that stopped being sent is drift")
	}
}

func TestAScalarThatBecomesAnObjectIsDrift(t *testing.T) {
	r := replay(t, `{"owner":"alice"}`, `{"owner":{"name":"alice"}}`)

	if r.Clean() {
		t.Fatal("a scalar promoted to a message is the widest kind of breaking change")
	}
	if got := r.Changes[0].Kind; got != diff.KindType {
		t.Errorf("want %q, got %q", diff.KindType, got)
	}
}

func TestJSONHasNoSeparateIntegerSoOneAndOnePointZeroAreTheSame(t *testing.T) {
	if !replay(t, `{"rate":1.0}`, `{"rate":1}`).Clean() {
		t.Fatal("both decode to the same float64; reporting drift here would make every replay of a " +
			"whole-numbered rate dirty")
	}
}

func TestAnUnchangedResponseStaysClean(t *testing.T) {
	body := `{"status":{"code":"SUCCESS"},"total_minor":"5498","lines":[{"quantity":3}]}`

	if !replay(t, body, body).Clean() {
		t.Fatal("comparing a response to itself must be clean, or the kind check has a false positive")
	}
}
