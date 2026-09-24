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

func TestASafeSpotWithoutAPrincipalSaysPrincipalCheckingIsOff(t *testing.T) {
	want := stepAs("create", runner.StatusPassed, `{"error":{"code":"OK"}}`)
	want.AuthProfile = "default"
	got := stepAs("create", runner.StatusPassed, `{"error":{"code":"REJECTED"}}`)
	got.AuthProfile, got.AuthPrincipal = "default", "cccc3333dddd4444"
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{want}}
	rep := diff.CompareWithRequests(spot, runOf("run", got), nil, nil)
	if len(rep.PrincipalUnchecked) != 1 || rep.PrincipalUnchecked[0] != "create" {
		t.Fatalf("the safe spot cannot say which principal ran create, got %v", rep.PrincipalUnchecked)
	}
	text := rep.Text()
	if !strings.Contains(text, "principal checking is off") || !strings.Contains(text, "shrt confirm thing-flow -supersede") {
		t.Fatalf("the report must say principal checking is off and how to turn it on:\n%s", text)
	}
	both := stepAs("create", runner.StatusPassed, `{"error":{"code":"OK"}}`)
	both.AuthProfile, both.AuthPrincipal = "default", "aaaa"
	spot.Steps[0] = both
	if rep := diff.CompareWithRequests(spot, runOf("run", got), nil, nil); len(rep.PrincipalUnchecked) != 0 {
		t.Fatalf("a safe spot with a principal is checked, got %v", rep.PrincipalUnchecked)
	}
}
