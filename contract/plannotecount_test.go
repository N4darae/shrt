package contract_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var heldCount = regexp.MustCompile(`step (\w+): .* which asserts it holds exactly (\d+) item\(s\)`)

func TestListExclusionNoteStatesTheCountTheListStepAsserts(t *testing.T) {
	for _, targets := range [][]string{
		{"CreateProduct", "ListProducts"},
		{"ListProducts", "CreateProduct"},
		{"CreateOrder", "ListOrders"},
		{"ListProducts"},
	} {
		p, text, notes := shopDemoPlan(t, targets...)
		found := false
		for _, m := range heldCount.FindAllStringSubmatch(notes, -1) {
			found = true
			st := planStep(t, p, m[1])
			n, _ := strconv.Atoi(m[2])
			asserted := -1
			for _, e := range st.Expect {
				if e.Exists == nil || *e.Exists {
					continue
				}
				head, idx, ok := strings.Cut(e.Path, ".")
				if !ok || strings.Contains(idx, ".") || head == "status" {
					continue
				}
				if v, err := strconv.Atoi(idx); err == nil {
					asserted = v
				}
			}
			if asserted != n {
				t.Fatalf("%v: the note says %s holds exactly %d item(s), but the step asserts index %d absent:\n%s\n%s",
					targets, m[1], n, asserted, notes, text)
			}
		}
		if !found {
			t.Fatalf("%v: no exclusion note in:\n%s", targets, notes)
		}
	}
}
