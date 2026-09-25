package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/doctor"
)

const orphanSpot = `{"chain":"stock-batch","run_id":"r1","steps":[{"index":1,"id":"create","call":"shrt.test.v1.ThingService/Create"}]}`

const renamedChain = `apiVersion: shrt/v1
name: stock-batch-renamed
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
`

func TestASafeSpotWhoseChainIsGoneIsAWarningWithTheRemedy(t *testing.T) {
	cfg := repo(t)
	write(t, filepath.Join(cfg.Abs(cfg.Paths.SafeSpots), "stock-batch.json"), orphanSpot)
	write(t, filepath.Join(cfg.Abs(cfg.Paths.Chains), "stock-batch-renamed.yaml"), renamedChain)
	r := run(t, cfg, options())
	f := find(t, r, doctor.CheckSafeSpots)
	if f.Level != doctor.LevelWarn {
		t.Fatalf("a safe spot with no chain fails the gate's verify, so doctor must warn:\n%s", r.Text())
	}
	for _, want := range []string{`no chain "stock-batch"`, `"stock-batch-renamed"`, "renamed"} {
		if !strings.Contains(f.Detail, want) {
			t.Errorf("the finding lacks %q: %s", want, f.Detail)
		}
	}
	for _, want := range []string{"shrt confirm stock-batch-renamed -rename-from stock-batch -by", "shrt confirm stock-batch-renamed", "git rm .shrt/safespots/stock-batch.json"} {
		if !strings.Contains(f.Remedy, want) {
			t.Errorf("the remedy lacks %q: %s", want, f.Remedy)
		}
	}
	if !r.Failed(true) {
		t.Errorf("doctor -strict fails on the warning, before the gate reaches verify")
	}
}

func TestEverySafeSpotWithItsChainIsOK(t *testing.T) {
	cfg := repo(t)
	write(t, filepath.Join(cfg.Abs(cfg.Paths.SafeSpots), "stock-batch-renamed.json"), orphanSpot)
	write(t, filepath.Join(cfg.Abs(cfg.Paths.Chains), "stock-batch-renamed.yaml"), renamedChain)
	r := run(t, cfg, options())
	if f := find(t, r, doctor.CheckSafeSpots); f.Level != doctor.LevelOK {
		t.Fatalf("a safe spot whose chain exists is fine: %+v", f)
	}
}
