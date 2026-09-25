package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestRedactPatternAddedAfterApprovalIsUnapprovedNotARegression(t *testing.T) {
	spot := orderSpot(`{"results":[{"qty_on_hand":5,"name":"Widget"}]}`)
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"results":[{"qty_on_hand":"<redacted>","name":"Widget"}]}`))
	rec.Redacted = []string{"**.password", "**.qty_on_hand"}
	rep := diff.Compare(spot, rec)
	if !rep.Clean() {
		t.Fatalf("a value redacted on one side only must not be compared:\n%s", rep.Text())
	}
	if !rep.Widened() || len(rep.UnapprovedRedact) != 1 || rep.UnapprovedRedact[0] != "**.qty_on_hand" {
		t.Fatalf("the redact pattern the safe spot did not have must be named as unapproved: %+v", rep.UnapprovedRedact)
	}
	text := rep.Text()
	if !strings.Contains(text, "**.qty_on_hand") || !strings.Contains(text, "fetch_order results.0.qty_on_hand") {
		t.Fatalf("the report must name the pattern and the value it hid:\n%s", text)
	}
}

func TestRedactPatternsTheSafeSpotRunLackedAreUnapproved(t *testing.T) {
	spot := orderSpot(`{"name":"Widget"}`)
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"name":"Widget"}`))
	rec.Redacted = []string{"**.password", "**.pin"}
	rep := diff.Compare(spot, rec)
	rep.NoteApprovedRedact([]string{"**.password"}, rec)
	if len(rep.UnapprovedRedact) != 1 || rep.UnapprovedRedact[0] != "**.pin" {
		t.Fatalf("a pattern the safe spot's run did not have must be unapproved: %+v", rep.UnapprovedRedact)
	}
}

func TestRedactedInTheSafeSpotOnlyIsNeverCompared(t *testing.T) {
	spot := orderSpot(`{"qty_on_hand":"<redacted>","name":"Widget"}`)
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"qty_on_hand":7,"name":"Widget"}`))
	rep := diff.Compare(spot, rec)
	if !rep.Clean() || rep.Widened() {
		t.Fatalf("a value redacted in the safe spot only is not compared:\n%s", rep.Text())
	}
}

func TestRequestValueRedactedOnOneSideIsNotAnInputChange(t *testing.T) {
	spot := orderSpot(`{"name":"Widget"}`)
	spot.Steps[0].Request = []byte(`{"qty":5}`)
	got := stepAs("fetch_order", runner.StatusPassed, `{"name":"Widget"}`)
	got.Request = []byte(`{"qty":"<redacted>"}`)
	if changes := diff.CompareRequests(spot, runOf("run", got), nil); len(changes) != 0 {
		t.Fatalf("a request value redacted on one side only is not different input: %+v", changes)
	}
}

func TestAnEmptyValueTheSafeSpotHeldUnderARedactPatternIsAChangeNotANewPattern(t *testing.T) {
	spot := orderSpot(`{"access_token":"","name":"Widget"}`)
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"access_token":"<redacted>","name":"Widget"}`))
	rec.Redacted = []string{"**.access_token"}
	rep := diff.Compare(spot, rec)
	rep.NoteApprovedRedact([]string{"**.access_token"}, rec)
	if rep.Widened() || strings.Contains(rep.Text(), "did not have") {
		t.Fatalf("the pattern was there at approval; the redactor leaves an empty value in the clear:\n%s", rep.Text())
	}
	if rep.Clean() || !strings.Contains(rep.Text(), "access_token") {
		t.Fatalf("an empty value that now holds a secret is a change:\n%s", rep.Text())
	}
}

func TestASecretTheSafeSpotRedactedThatIsNowEmptyIsAChange(t *testing.T) {
	spot := orderSpot(`{"access_token":"<redacted>","name":"Widget"}`)
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"access_token":"","name":"Widget"}`))
	rec.Redacted = []string{"**.access_token"}
	if rep := diff.Compare(spot, rec); rep.Clean() || !strings.Contains(rep.Text(), "access_token") {
		t.Fatalf("a secret that is now empty is a change, not a redacted value:\n%s", rep.Text())
	}
}
