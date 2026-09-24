package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestAnotherPrincipalBehindTheSameProfileIsAnInputChange(t *testing.T) {
	body := `{"error":{"code":"OK"}}`
	want := stepAs("create", runner.StatusPassed, body)
	want.AuthProfile, want.AuthPrincipal = "default", "aaaa1111bbbb2222"
	got := stepAs("create", runner.StatusPassed, body)
	got.AuthProfile, got.AuthPrincipal = "default", "cccc3333dddd4444"
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{want}}

	rep := diff.CompareWithRequests(spot, runOf("run", got), nil, nil)
	if !rep.PrincipalChanged() {
		t.Fatalf("the same profile logged in as someone else, so the safe spot does not vouch for this run:\n%s", rep.Text())
	}
	if !strings.Contains(rep.Text(), "principal") {
		t.Fatalf("the report must say the principal changed:\n%s", rep.Text())
	}

	same := stepAs("create", runner.StatusPassed, body)
	same.AuthProfile, same.AuthPrincipal = "default", "aaaa1111bbbb2222"
	if rep := diff.CompareWithRequests(spot, runOf("run", same), nil, nil); rep.PrincipalChanged() {
		t.Fatalf("the same principal is no input change:\n%s", rep.Text())
	}
	old := stepAs("create", runner.StatusPassed, body)
	old.AuthProfile = "default"
	if rep := diff.CompareWithRequests(spot, runOf("run", old), nil, nil); rep.PrincipalChanged() {
		t.Fatalf("a record that does not say which principal ran is not compared:\n%s", rep.Text())
	}
}
