package chain_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestASliceCarriesTheKeptRedPinsOfTheStepsItKeeps(t *testing.T) {
	got := "SUCCESS"
	c := &chain.Chain{Name: "kr", Steps: []*chain.Step{
		{ID: "create", Call: "S/CreateThing", Body: map[string]any{"name": "x"},
			Expect: []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}},
		{ID: "confirm", Call: "S/ConfirmThing", Body: map[string]any{"id": "${create.thing.id}"},
			Expect: []chain.Expectation{{Path: "status.code", Equals: "REJECTED"}, {Path: "status.details.0.reason", Equals: "Short"}}},
		{ID: "later", Call: "S/GetThing", Body: map[string]any{"id": "${create.thing.id}"},
			Expect: []chain.Expectation{{Path: "thing.qty", Equals: 0}}},
	}, KeptRed: []chain.Pin{
		{Step: "confirm", Path: "status.code", Got: &got},
		{Step: "confirm", Path: "status.details.0.reason"},
		{Step: "later", Path: "thing.qty"},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	res, err := chain.Slice(c, "confirm", chain.SliceOptions{Mode: chain.SliceModeClosure})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chain.KeptRed) != 2 || res.Chain.KeptRed[0].Step != "confirm" || res.Chain.KeptRed[1].Step != "confirm" {
		t.Fatalf("the slice keeps confirm, so it must carry confirm's two pins and drop the pin on later: %+v", res.Chain.KeptRed)
	}
	if len(res.DroppedPins) != 1 || res.DroppedPins[0].Step != "later" {
		t.Fatalf("the pin on a step the slice drops is reported: %+v", res.DroppedPins)
	}
	raw, err := res.Chain.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "kr-slice.yaml")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := chain.LoadFile(path); err != nil {
		t.Fatalf("the slice with its pins must load: %v\n%s", err, raw)
	}
}
