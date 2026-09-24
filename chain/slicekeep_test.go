package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func keepFixture(t *testing.T) *chain.Chain {
	t.Helper()
	c := &chain.Chain{Name: "keepy", Steps: []*chain.Step{
		{ID: "seed", Call: "pkg.Svc/Create"},
		{ID: "owner", Call: "pkg.Svc/Create"},
		{ID: "noise", Call: "pkg.Svc/Update", Body: map[string]any{"owner": "${owner.id}"}},
		{ID: "boom", Call: "pkg.Svc/Approve", Body: map[string]any{"id": "${seed.id}"}},
		{ID: "after", Call: "pkg.Svc/Create"},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSliceKeepPutsADroppedWriteBackWithWhatItNeeds(t *testing.T) {
	c := keepFixture(t)
	plain, err := chain.Slice(c, "boom", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !plain.UnderIncluded || len(plain.DroppedWrites) != 2 {
		t.Fatalf("without -keep the slice drops owner and noise: %+v", plain.DroppedWrites)
	}
	res, err := chain.Slice(c, "boom", chain.SliceOptions{Keep: []string{"noise"}})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, k := range res.Kept {
		ids = append(ids, k.ID+":"+k.Kind)
	}
	if got := strings.Join(ids, " "); got != "seed:produces owner:produces noise:requested boom:target" {
		t.Fatalf("a kept step brings its producers and keeps chain order, got %s", got)
	}
	if res.UnderIncluded || len(res.DroppedWrites) != 0 {
		t.Fatalf("every write before the target is kept, so nothing is under-included: %+v", res.DroppedWrites)
	}
	if !strings.Contains(res.Chain.Description, "Kept on request: noise.") {
		t.Fatalf("the written slice must record what was kept on request:\n%s", res.Chain.Description)
	}
}

func TestSliceKeepRefusesUnknownAndLaterSteps(t *testing.T) {
	c := keepFixture(t)
	if _, err := chain.Slice(c, "boom", chain.SliceOptions{Keep: []string{"nope"}}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("an unknown step id must be refused by name, got %v", err)
	}
	if _, err := chain.Slice(c, "boom", chain.SliceOptions{Keep: []string{"after"}}); err == nil || !strings.Contains(err.Error(), "after the target") {
		t.Fatalf("a step after the target cannot change its verdict and must be refused, got %v", err)
	}
}
