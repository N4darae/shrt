package runner_test

import (
	"context"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestRecordNamesEachCallByItsFullNameHoweverTheChainSpeltIt(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	c := testChain()
	c.Steps[0].Call = "Create"
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for i, want := range []string{"shrt.test.v1.ThingService/Create", "shrt.test.v1.ThingService/Fetch"} {
		if rec.Steps[i].Call != want {
			t.Fatalf("step %d must be recorded as %s however the chain spelt it, got %q", i, want, rec.Steps[i].Call)
		}
	}
}
