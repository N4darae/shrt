package contract

import (
	"regexp"
	"testing"
)

func TestEveryProbePassNamesTheLabFaultsOnlyItCatches(t *testing.T) {
	faults := regexp.MustCompile(`^[FD][0-9]+( [FD][0-9]+)*$`)
	seen := map[string]bool{}
	for _, pass := range probePasses {
		if !faults.MatchString(pass.caught) {
			t.Errorf("probe %q lists no lab fault it alone catches: %q", pass.label, pass.caught)
		}
		if seen[pass.label] {
			t.Errorf("probe %q listed twice", pass.label)
		}
		seen[pass.label] = true
	}
}
