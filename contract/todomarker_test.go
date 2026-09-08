package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func todoFields(t *testing.T, doc string) []string {
	t.Helper()
	out := []string{}
	for _, i := range contract.ScanTodos("dealing", []byte(doc)) {
		out = append(out, i.Field+": "+i.Message)
	}
	return out
}

func TestScanTodosReportsTheScaffoldsOwnMarker(t *testing.T) {
	cases := map[string]string{
		"scalar value": "summary: 'TODO: what this rpc does'\n",
		"bare marker":  "summary: TODO\n",
		"line comment": "summary: something # TODO: explain the units\n",
		"inside a seq": "required:\n    - 'TODO: which fields the server rejects without'\n",
	}
	for name, doc := range cases {
		if got := todoFields(t, doc); len(got) == 0 {
			t.Errorf("%s: ScanTodos must still report the scaffold's marker, got nothing for %q", name, doc)
		}
	}
}

func TestScanTodosIgnoresProseThatMentionsAMarker(t *testing.T) {
	cases := map[string]string{
		"mid-sentence":     "summary: the source carries a TODO(owner) marker at that line\n",
		"quoted in a note": "summary: grep for TODO before trusting the comment\n",
		"part of a word":   "summary: the TODOS list is elsewhere\n",
	}
	for name, doc := range cases {
		got := todoFields(t, doc)
		if len(got) != 0 {
			t.Errorf("%s: prose naming a marker is not an unfilled TODO — lint flagging it means you cannot document a marker without a false warning. got %v", name, got)
		}
	}
}

func TestScanTodosStillReportsAMarkerAtTheStartOfProse(t *testing.T) {
	got := todoFields(t, "summary: 'TODO: pick a from'\n")
	if len(got) != 1 || !strings.Contains(got[0], "pick a from") {
		t.Errorf("want one issue naming the todo text, got %v", got)
	}
}
