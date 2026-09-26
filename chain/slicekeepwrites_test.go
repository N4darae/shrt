package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestSliceKeepWritesKeepsEveryEarlierWrite(t *testing.T) {
	c := keepFixture(t)
	res, err := chain.Slice(c, "boom", chain.SliceOptions{Keep: []string{chain.SliceKeepWrites}})
	if err != nil {
		t.Fatal(err)
	}
	if res.UnderIncluded || len(res.DroppedWrites) != 0 {
		t.Fatalf("-keep writes keeps every write before the target, so nothing is dropped: %+v", res.DroppedWrites)
	}
	ids := []string{}
	for _, k := range res.Kept {
		ids = append(ids, k.ID)
	}
	if len(ids) != 4 || ids[0] != "seed" || ids[1] != "owner" || ids[2] != "noise" || ids[3] != "boom" {
		t.Fatalf("want seed, owner, noise and the target kept, got %v", ids)
	}
}

func TestSliceKeepWritesCombinesWithIDs(t *testing.T) {
	c := &chain.Chain{Name: "mixed", Steps: []*chain.Step{
		{ID: "make", Call: "pkg.Svc/Create"},
		{ID: "peek", Call: "pkg.Svc/Fetch"},
		{ID: "boom", Call: "pkg.Svc/Approve"},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	res, err := chain.Slice(c, "boom", chain.SliceOptions{Keep: []string{"peek", chain.SliceKeepWrites}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Kept) != 3 {
		t.Fatalf("want the write, the asked read and the target, got %+v", res.Kept)
	}
}

func TestSliceKeepWritesKeepsAWriteTheSourceRunShowsRefused(t *testing.T) {
	c := &chain.Chain{Name: "refusals", Steps: []*chain.Step{
		{ID: "make", Call: "pkg.Svc/Create"},
		{ID: "owner", Call: "pkg.Svc/Create"},
		{ID: "boom", Call: "pkg.Svc/Approve"},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	refused := func(id string) (string, bool) { return "refused in-band", id == "owner" }
	res, err := chain.Slice(c, "boom", chain.SliceOptions{Keep: []string{chain.SliceKeepWrites}, Refused: refused})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Kept) != 3 || res.Kept[1].ID != "owner" {
		t.Fatalf("a refused write can still change state, so -keep writes keeps it: %+v", res.Kept)
	}
	if res.UnderIncluded || len(res.RefusedWrites) != 0 {
		t.Fatalf("nothing is dropped: %+v %+v", res.DroppedWrites, res.RefusedWrites)
	}
}
