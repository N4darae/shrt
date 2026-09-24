package diff_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
)

var wantGot = regexp.MustCompile(`want=(\S+) got=(\S+)`)

func TestAStaleIdIsNotPrintedAsWantAndGotThatLookEqual(t *testing.T) {
	for name, tc := range map[string][2]string{
		"stale after a rename": {`{"customer":{"id_customer":"cus-9389f45a3d9e"}}`, `{"customer":{"id_customer":"cus-9389aa000001"}}`},
		"renamed elsewhere":    {`{"customer":{"id_customer":"cus-9389f45a3d9e"}}`, `{"customer":{"id_customer":"cus-c82a220685c5"}}`},
	} {
		spot := customerOrderSpot(tc[0],
			`{"order":{"id_customer":"cus-9389f45a3d9e"}}`,
			`{"order":{"id_customer":"cus-9389f45a3d9e"}}`)
		run := customerOrderRun(tc[1],
			`{"order":{"id_customer":"cus-9389f45a3d9e"}}`,
			`{"order":{"id_customer":"cus-9389f45a3d9e"}}`)
		rep := diff.Compare(spot, run)
		if rep.Clean() {
			t.Fatalf("%s: the order still names the safe spot's customer, got no drift", name)
		}
		seen := false
		for _, line := range strings.Split(rep.Text(), "\n") {
			if !strings.Contains(line, "[order]") || !strings.Contains(line, "id_customer") {
				continue
			}
			seen = true
			m := wantGot.FindStringSubmatch(line)
			if m == nil || m[1] == m[2] {
				t.Errorf("%s: a changed id must not print want and got as the same text: %s", name, line)
			}
		}
		if !seen {
			t.Errorf("%s: no line for order id_customer:\n%s", name, rep.Text())
		}
	}
}
