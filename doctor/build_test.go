package doctor_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/doctor"
)

func TestTheBuildIsReportedSoAReceiptSaysWhichBinaryProducedIt(t *testing.T) {
	got := find(t, run(t, repo(t), options()), doctor.CheckBuild)

	if got.Level != doctor.LevelOK {
		t.Fatalf("knowing the build is provenance, not a verdict: %s", got.Level)
	}
	if strings.TrimSpace(got.Detail) == "" {
		t.Fatal("the build line is the one thing a bug report always needs, and it is empty")
	}
}

func TestADirtyBuildSaysItsCommitDoesNotDescribeIt(t *testing.T) {
	b := doctor.Build{Version: "v1.4.0", Revision: "0123456789abcdef", Modified: true, Go: "go1.26.2"}

	if !strings.Contains(b.String(), "dirty") {
		t.Errorf("a binary built from uncommitted changes has to say so: %q", b.String())
	}
	if !strings.Contains(b.Provenance(), "uncommitted") {
		t.Errorf("and say what it means: %q", b.Provenance())
	}
}

func TestAPseudoVersionIsNotPrintedTwice(t *testing.T) {
	b := doctor.Build{Version: "v1.5.1-0.20260922071528-ae82fbc00bd9+dirty", Revision: "ae82fbc00bd9ffff", Modified: true}

	if n := strings.Count(b.String(), "ae82fbc00bd9"); n != 1 {
		t.Errorf("the pseudo-version already carries the commit; printing it again reads like two "+
			"different builds: %q", b.String())
	}
	if n := strings.Count(b.String(), "dirty"); n != 1 {
		t.Errorf("same for the dirty marker: %q", b.String())
	}
}

func TestATaggedBuildHasNothingToApologiseFor(t *testing.T) {
	b := doctor.Build{Version: "v1.4.0", Revision: "0123456789abcdef", Time: "2026-09-01T00:00:00Z", Go: "go1.26.2"}

	if b.Provenance() != "" {
		t.Errorf("a clean tagged build needs no caveat: %q", b.Provenance())
	}
	if !strings.Contains(b.String(), "0123456789ab") {
		t.Errorf("but it still names its commit, shortened: %q", b.String())
	}
}

func TestABinaryWithNoStampSaysSoRatherThanLookingFine(t *testing.T) {
	b := doctor.Build{Version: "unknown"}

	if !strings.Contains(b.Provenance(), "no version and no commit") {
		t.Errorf("an unstamped binary is the one you cannot reason about at all: %q", b.Provenance())
	}
}
