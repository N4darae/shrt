package contract_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func shopDemo(t *testing.T) (*catalog.Catalog, *contract.Library) {
	t.Helper()
	path, ok := chain.EnvelopePath(), chain.EnvelopeOK()
	item := chain.ItemEnvelope()
	codes := chain.CodeFields()
	contract.ApplyConventions(nil, "status.code", "SUCCESS")
	chain.ApplyItemEnvelope("results[].status.code")
	chain.ApplyCodeFields([]string{"app_code", "reason"})
	t.Cleanup(func() {
		contract.ApplyConventions(nil, path, ok)
		chain.ApplyItemEnvelope(item)
		chain.ApplyCodeFields(codes)
	})
	raw, err := os.ReadFile(filepath.Join("testdata", "shopdemo", "descriptor.binpb"))
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	lib, broken, err := contract.LoadLibraryIn(filepath.Join("testdata", "shopdemo", "contracts"), cat)
	if err != nil || len(broken) > 0 {
		t.Fatalf("load shopdemo contracts: %v %v", err, broken)
	}
	return cat, lib
}

func shopDemoEdited(t *testing.T, edit func(name, body string) string) (*catalog.Catalog, *contract.Library) {
	t.Helper()
	cat, _ := shopDemo(t)
	lib, broken, err := contract.LoadLibraryIn(editedContracts(t, edit), cat)
	if err != nil || len(broken) > 0 {
		t.Fatalf("load edited contracts: %v %v", err, broken)
	}
	return cat, lib
}

func editedContracts(t *testing.T, edit func(name, body string) string) string {
	t.Helper()
	src := filepath.Join("testdata", "shopdemo", "contracts")
	dir := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), []byte(edit(e.Name(), string(raw))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func editedPlan(t *testing.T, edit func(name, body string) string, targets ...string) *contract.Plan {
	t.Helper()
	cat, lib := shopDemoEdited(t, edit)
	p, err := contract.BuildPlanFor(targets, lib, cat, "shopdemo")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func shopDemoPlan(t *testing.T, targets ...string) (*contract.Plan, string, string) {
	t.Helper()
	return shopDemoPlanWith(t, contract.PlanOptions{}, targets...)
}

func shopDemoPlanWith(t *testing.T, opts contract.PlanOptions, targets ...string) (*contract.Plan, string, string) {
	t.Helper()
	cat, lib := shopDemo(t)
	return planFrom(t, cat, lib, opts, targets...)
}

func planFrom(t *testing.T, cat *catalog.Catalog, lib *contract.Library, opts contract.PlanOptions, targets ...string) (*contract.Plan, string, string) {
	t.Helper()
	p, err := contract.BuildPlanWith(targets, lib, cat, "shopdemo", opts)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.YAML()
	if err != nil {
		t.Fatal(err)
	}
	return p, string(raw), strings.Join(p.Notes, "\n")
}

func stepIDs(p *contract.Plan) []string {
	ids := []string{}
	for _, s := range p.Chain.Steps {
		ids = append(ids, s.ID)
	}
	return ids
}

func planStep(t *testing.T, p *contract.Plan, id string) *chain.Step {
	t.Helper()
	st, ok := p.Chain.Step(id)
	if !ok {
		t.Fatalf("no step %s in the plan (steps: %s)", id, strings.Join(stepIDs(p), ", "))
	}
	return st
}

func wantExpect(t *testing.T, st *chain.Step, path string, want any) {
	t.Helper()
	for _, e := range st.Expect {
		if e.Path == path && e.Equals != nil && fmt.Sprint(e.Equals) == fmt.Sprint(want) {
			return
		}
	}
	t.Fatalf("step %s: want %s equals %v, got %+v", st.ID, path, want, st.Expect)
}

func wantExists(t *testing.T, st *chain.Step, path string, want bool) {
	t.Helper()
	for _, e := range st.Expect {
		if e.Path == path && e.Exists != nil && *e.Exists == want {
			return
		}
	}
	t.Fatalf("step %s: want %s exists %v, got %+v", st.ID, path, want, st.Expect)
}

func bodyAt(t *testing.T, st *chain.Step, path string) string {
	t.Helper()
	var cur any = st.Body
	for _, seg := range chain.SplitPath(path) {
		switch c := cur.(type) {
		case map[string]any:
			cur = c[seg]
		case []any:
			i := 0
			fmt.Sscan(seg, &i)
			if i >= len(c) {
				t.Fatalf("step %s: %s is out of range in %v", st.ID, path, st.Body)
			}
			cur = c[i]
		default:
			t.Fatalf("step %s: no %s in %v", st.ID, path, st.Body)
		}
	}
	return fmt.Sprint(cur)
}
