package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestAPrincipalSwapIsAnInputChange(t *testing.T) {
	step := func(profile string) *runner.StepRecord {
		return &runner.StepRecord{ID: "create_customer", Call: "CustomerService/CreateCustomer", Status: runner.StatusPassed,
			AuthProfile: profile, Request: json.RawMessage(`{"name":"Carol"}`), Response: json.RawMessage(`{"status":{"code":"SUCCESS"}}`)}
	}
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{step("default")}}
	rec := runOf("run", step("clerk"))
	changes := diff.CompareRequests(spot, rec, nil)
	if len(changes) != 1 || changes[0].Path != "auth_profile" || changes[0].Want != "default" || changes[0].Got != "clerk" {
		t.Fatalf("the step now runs as another principal, which is a change of input: %+v", changes)
	}
	rep := diff.CompareWithRequests(spot, rec, nil, nil)
	if !strings.Contains(rep.Text(), "auth_profile (default -> clerk)") {
		t.Fatalf("name the profile change:\n%s", rep.Text())
	}
	if !rep.PrincipalChanged() {
		t.Fatal("a principal swap must stop verify from reporting no drift")
	}
	if got := diff.CompareRequests(spot, runOf("run", step("default")), nil); len(got) != 0 {
		t.Fatalf("same principal, no change: %+v", got)
	}
	if got := diff.CompareRequests(spot, runOf("run", step("")), nil); len(got) != 0 {
		t.Fatalf("a record that does not say which profile ran is not evidence of a swap: %+v", got)
	}
}
