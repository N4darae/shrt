package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/doctor"
)

func TestDoctorReportsNullStepAsAnEditedSafeSpot(t *testing.T) {
	cfg := repo(t)
	spot := sealedSpot(t, "broken")
	spot.Steps[0] = nil
	write(t, filepath.Join(cfg.Abs(cfg.Paths.SafeSpots), "broken.json"), spotJSON(t, spot))
	report := run(t, cfg, options())
	f := find(t, report, doctor.CheckSafeSpotDigests)
	if f.Level != doctor.LevelError || !strings.Contains(f.Detail, "changed after") || !strings.Contains(f.Detail, "broken.json") {
		t.Fatalf("want an edited-safe-spot diagnostic, got %s", report.Text(true))
	}
}
