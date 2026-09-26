package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestTheRecordCarriesTheUnorderedListsVerifyReads(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	c := testChain()
	c.Unordered = []string{"items"}
	c.Steps[1].Unordered = []string{"lines"}
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(rec.Steps[0].Unordered, ","); got != "items" {
		t.Errorf("a chain-level declaration applies to every step, got %q", got)
	}
	if got := strings.Join(rec.Steps[1].Unordered, ","); got != "items,lines" {
		t.Errorf("a step's declaration is added to the chain's, got %q", got)
	}
}
