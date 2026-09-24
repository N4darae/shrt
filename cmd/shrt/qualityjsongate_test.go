package main

import (
	"strings"
	"testing"
)

func TestContractQualityJSONStillAppliesTheGate(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)

	if err := contractQuality([]string{"-gate", "-baseline", "no-such-baseline"}); err == nil {
		t.Fatal("precondition: -gate with a missing baseline file fails")
	}
	var err error
	out := captureStdout(t, func() {
		err = contractQuality([]string{"-json", "-gate", "-baseline", "no-such-baseline"})
	})
	if err == nil {
		t.Fatal("-json must not switch the gate off: a CI step asking for JSON and a gate would pass blind")
	}
	if !strings.Contains(out, "total_score") {
		t.Fatalf("-json -gate must still emit the JSON report:\n%s", out)
	}
}
