package doctor_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/doctor"
)

const goodOverlay = `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shrt.test.v1.ThingService/Create:
        summary: records a pending order
        status: draft
`

func TestABrokenContractOverlayFailsDoctor(t *testing.T) {
	cfg := repo(t)
	write(t, cfg.Abs(cfg.Paths.Contracts)+"/orders.yaml", goodOverlay)
	if got := find(t, run(t, cfg, options()), doctor.CheckContracts); got.Level != doctor.LevelOK {
		t.Fatalf("one sound overlay is ok, got %s: %s", got.Level, got.Detail)
	}

	write(t, cfg.Abs(cfg.Paths.Contracts)+"/broken.yaml", "rpcs: [unclosed\n")
	got := find(t, run(t, cfg, options()), doctor.CheckContracts)
	if got.Level != doctor.LevelError || !strings.Contains(got.Detail, "broken.yaml") {
		t.Fatalf("an overlay that does not parse stops every contract command; want FAIL naming it, got %s: %s", got.Level, got.Detail)
	}
}

func TestAnOverlayInASubdirectoryIsAWarning(t *testing.T) {
	cfg := repo(t)
	write(t, cfg.Abs(cfg.Paths.Contracts)+"/orders.yaml", goodOverlay)
	write(t, cfg.Abs(cfg.Paths.Contracts)+"/legacy/broken.yaml", "rpcs: [unclosed\n")
	got := find(t, run(t, cfg, options()), doctor.CheckContracts)
	if got.Level != doctor.LevelWarn || !strings.Contains(got.Detail, "legacy/broken.yaml") {
		t.Fatalf("shrt reads only the top level of the contracts directory; want WARN naming the file, got %s: %s", got.Level, got.Detail)
	}
}

func TestAContractEntryForARemovedRPCIsAWarning(t *testing.T) {
	cfg := repo(t)
	write(t, cfg.Abs(cfg.Paths.Contracts)+"/orders.yaml", goodOverlay+`    shrt.test.v1.ThingService/GetThing:
        summary: reads a thing the proto no longer declares
        status: draft
`)
	got := find(t, run(t, cfg, options()), doctor.CheckContracts)
	if got.Level != doctor.LevelWarn || !strings.Contains(got.Detail, "shrt.test.v1.ThingService/GetThing in .shrt/contracts/orders.yaml") ||
		!strings.Contains(got.Detail, "removed from the proto?") {
		t.Fatalf("an entry naming an rpc the descriptor lacks is dead contract; want WARN naming it and its file, got %s: %s", got.Level, got.Detail)
	}
}
