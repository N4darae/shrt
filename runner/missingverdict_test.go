package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAMissingOrEmptyVerdictFailsAStepThatPinsNone(t *testing.T) {
	for name, answer := range map[string]map[string]any{
		"no envelope":    {},
		"empty envelope": {"status": map[string]any{}},
		"empty code":     {"status": map[string]any{"code": ""}},
	} {
		srv := newShopServer(t, map[string]any{"AddStock": answer})
		c := normalized(t, addStockChain(chain.Expectation{Path: "qty_on_hand", Equals: 0}))
		rec, err := srv.runner().Run(context.Background(), c, runner.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if rec.Status != runner.StatusFailed {
			t.Fatalf("%s: the response carries no verdict at status.code, so qty_on_hand == 0 read a zero value "+
				"nothing vouches for; got %s %+v", name, rec.Status, rec.Steps[0].Expect)
		}
		if !strings.Contains(rec.Failure, "no verdict") {
			t.Fatalf("%s: say the verdict was missing, got %q", name, rec.Failure)
		}
	}
}

func TestAMissingVerdictThatIsPinnedIsJudgedByThePin(t *testing.T) {
	srv := newShopServer(t, map[string]any{"AddStock": map[string]any{}})
	absent := false
	c := normalized(t, addStockChain(chain.Expectation{Path: "status.code", Exists: &absent}, chain.Expectation{Path: "qty_on_hand", Equals: 0}))
	rec, err := srv.runner().Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusPassed {
		t.Fatalf("the step pins the empty verdict itself, so it is judged by that pin; got %s %+v", rec.Status, rec.Steps[0].Expect)
	}
}

func TestAPresentOkVerdictIsUnaffected(t *testing.T) {
	srv := newShopServer(t, map[string]any{"AddStock": map[string]any{"status": map[string]any{"code": "SUCCESS"}}})
	c := normalized(t, addStockChain(chain.Expectation{Path: "qty_on_hand", Equals: 0}))
	rec, err := srv.runner().Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StatusPassed {
		t.Fatalf("got %s %+v", rec.Status, rec.Steps[0].Expect)
	}
}
