package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/doctor"
)

const confSpot = `{"chain":"conf","run_id":"r1","steps":[
{"index":1,"id":"cust","call":"shrt.test.v1.ThingService/Create","expect":[{"path":"error.code","rule":"equals","passed":true},{"path":"name","rule":"equals","passed":true}]},
{"index":2,"id":"getc","call":"shrt.test.v1.ThingService/Fetch","expect":[{"path":"error.code","rule":"equals","passed":true}]}]}`

func twoStepChain(name string, nameExpect bool) string {
	extra := ""
	if nameExpect {
		extra = "          - path: name\n            equals: widget\n"
	}
	return `apiVersion: shrt/v1
name: ` + name + `
steps:
    - id: cust
      call: ThingService/Create
      body:
          name: widget
      expect:
          - path: error.code
            equals: OK
` + extra + `    - id: getc
      call: ThingService/Fetch
      body:
          id: x
      expect:
          - path: error.code
            equals: OK
`
}

func TestAnOrphanSafeSpotNamesTheCopyWithTheSameStepsAmongLookAlikes(t *testing.T) {
	cfg := repo(t)
	chains := cfg.Abs(cfg.Paths.Chains)
	write(t, filepath.Join(cfg.Abs(cfg.Paths.SafeSpots), "conf.json"), confSpot)
	write(t, filepath.Join(chains, "conf-new.yaml"), twoStepChain("conf-new", true))
	write(t, filepath.Join(chains, "sec.yaml"), twoStepChain("sec", false))
	write(t, filepath.Join(chains, "hdr.yaml"), twoStepChain("hdr", false))
	r := run(t, cfg, options())
	f := find(t, r, doctor.CheckSafeSpots)
	if f.Level != doctor.LevelWarn || !strings.Contains(f.Detail, `chain "conf-new" has the same step ids and calls`) {
		t.Fatalf("conf-new alone has the safe spot's steps and expectations, so doctor names it: %+v", f)
	}
	if !strings.Contains(f.Remedy, "shrt confirm conf-new") {
		t.Errorf("the remedy runs and proposes conf-new: %s", f.Remedy)
	}
}

func TestAnOrphanSafeSpotListsEveryChainItMayHaveBecome(t *testing.T) {
	cfg := repo(t)
	chains := cfg.Abs(cfg.Paths.Chains)
	write(t, filepath.Join(cfg.Abs(cfg.Paths.SafeSpots), "conf.json"), confSpot)
	write(t, filepath.Join(chains, "a.yaml"), twoStepChain("a", true))
	write(t, filepath.Join(chains, "b.yaml"), twoStepChain("b", true))
	r := run(t, cfg, options())
	f := find(t, r, doctor.CheckSafeSpots)
	if !strings.Contains(f.Detail, `chains "a", "b" have the same step ids and calls`) {
		t.Fatalf("two chains match equally, so doctor names both: %+v", f)
	}
}

func TestAChainFileWhoseNameDiffersFromItsFileIsAWarning(t *testing.T) {
	cfg := repo(t)
	write(t, filepath.Join(cfg.Abs(cfg.Paths.SafeSpots), "conf.json"), confSpot)
	write(t, filepath.Join(cfg.Abs(cfg.Paths.Chains), "conf.yaml"), twoStepChain("conf-new", true))
	r := run(t, cfg, options())
	f := find(t, r, doctor.CheckSafeSpots)
	if f.Level != doctor.LevelWarn || !strings.Contains(f.Detail, "conf.yaml declares name: conf-new") {
		t.Fatalf("conf.yaml named conf-new splits its runs from its safe spot, so doctor warns: %+v\n%s", f, r.Text(true))
	}
	if !strings.Contains(f.Remedy, "rename the file to conf-new.yaml, or set name: conf") {
		t.Errorf("the remedy says how to make them agree: %s", f.Remedy)
	}
	orphan := false
	for _, g := range findAll(r, doctor.CheckSafeSpots) {
		orphan = orphan || strings.Contains(g.Detail, `no chain "conf"`) && strings.Contains(g.Detail, `chain "conf-new"`)
	}
	if !orphan {
		t.Errorf("conf.json belongs to no chain now that conf.yaml is chain conf-new, so it is an orphan naming conf-new:\n%s", r.Text(true))
	}
}
