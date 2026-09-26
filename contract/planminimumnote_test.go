package contract_test

import (
	"strings"
	"testing"
)

func TestBoundaryNoteAsksForAMinimumOnlyForAFieldItDidNotProbe(t *testing.T) {
	p, text, notes := shopDemoPlan(t, "AddStock")
	planStep(t, p, "add_stock_qty_min")
	planStep(t, p, "add_stock_qty_below_min")
	for _, line := range strings.Split(notes, "\n") {
		if strings.Contains(line, "boundary and magnitude probes") && strings.Contains(line, "declare a minimum") {
			t.Fatalf("qty was probed at its minimum, so the note must not ask for one:\n%s\n%s", line, text)
		}
	}
}
