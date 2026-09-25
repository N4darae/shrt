package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestAnUnapprovedVolatileThatHidOnlyAFixtureEchoHidNothing(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "spot", Steps: []*runner.StepRecord{
		{ID: "dup", Call: "S/Create", Status: runner.StatusPassed, Request: json.RawMessage(`{"name":"Dup t-first"}`),
			Response: json.RawMessage(`{"id":"x","message":"Dup t-first exists"}`)},
	}}
	rec := &runner.Record{RunID: "run", Chain: "c", Status: runner.StatusPassed, Volatile: []string{"**.message"}, Steps: []*runner.StepRecord{
		{ID: "dup", Call: "S/Create", Status: runner.StatusPassed, Request: json.RawMessage(`{"name":"Dup t-second"}`),
			Response: json.RawMessage(`{"id":"x","message":"Dup t-second exists"}`)},
	}}
	rep := diff.CompareMasking(spot, rec, nil)
	rep.RequestChanges = []diff.Change{{Step: "dup", Path: "name", Kind: diff.KindChanged, Want: "Dup t-first", Got: "Dup t-second"}}
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{Named: func(step, path string) bool { return path == "name" }})
	text := rep.Text()
	if !strings.Contains(text, "hid nothing this run") {
		t.Fatalf("the unapproved pattern hid only a fixture echo, so it hid nothing this run:\n%s", text)
	}
	if rep.VolatileMasked != 0 || len(rep.FixtureEchoed) != 1 {
		t.Fatalf("the echo is counted as a fixture echo, not as a value under volatile paths: volatile %d, echoed %d\n%s",
			rep.VolatileMasked, len(rep.FixtureEchoed), text)
	}
}
