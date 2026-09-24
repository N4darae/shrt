package doctor_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/doctor"
)

func TestTheEnvelopeCountSaysRpcsBecauseItCountsRpcs(t *testing.T) {
	got := conventionsReport(t, config.Default(), catalogtest.Foreign())
	for _, f := range got {
		if f.Level != doctor.LevelWarn || !strings.Contains(f.Detail, "conventions.envelope_path") {
			continue
		}
		if strings.Contains(f.Detail, "response message(s)") || !strings.Contains(f.Detail, "rpc(s)") {
			t.Fatalf("the count is of rpcs, two of which may share one response message, so it must "+
				"say rpcs: %s", f.Detail)
		}
		return
	}
	t.Fatalf("no envelope warning: %v", got)
}
