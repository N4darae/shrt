package runner_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestRecordConfirmReplayDetectsRegression(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	root := t.TempDir()
	st := store.New(filepath.Join(root, "runs"), filepath.Join(root, "safespots"))
	r := newRunner(t, srv)
	ctx := context.Background()

	first, err := r.Run(ctx, testChain(), runner.Options{})
	if err != nil || !first.Passed() {
		t.Fatalf("first run: %v %s", err, first.Failure)
	}
	if _, err := st.SaveRun(first); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, _, err := st.Promote(first, store.Confirmation{By: "agent"}); !errors.Is(err, store.ErrNotConfirmed) {
		t.Fatal("an agent must not be able to create a safe spot")
	}
	spot, _, err := st.Promote(first, store.Confirmation{By: "reviewer", Acknowledged: true})
	if err != nil {
		t.Fatalf("promote: %v", err)
	}

	clean, err := r.Run(ctx, testChain(), runner.Options{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep := diff.Compare(spot, clean); !rep.Clean() {
		t.Fatalf("a faithful replay must not drift, got:\n%s", rep.Text())
	}

	srv.setDrift("gadget")
	broken, err := r.Run(ctx, testChain(), runner.Options{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	rep := diff.Compare(spot, broken)
	if rep.Clean() {
		t.Fatal("a changed response must be reported as drift")
	}
	found := false
	for _, c := range rep.Changes {
		if c.Step == "fetch" && c.Path == "name" && c.Want == "widget" && c.Got == "gadget" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the diff must name the step and field that changed, got:\n%s", rep.Text())
	}
}
