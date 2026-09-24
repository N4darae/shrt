package runner_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestRecordVarsRoundTripLargeIntegersExactly(t *testing.T) {
	in := &runner.Record{RunID: "r", Vars: map[string]any{"tag": int64(1790233617019699993), "small": int64(12345678), "ratio": 0.5, "name": "x"}}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out runner.Record
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.RunID != "r" {
		t.Fatalf("the other fields must still decode, run_id = %q", out.RunID)
	}
	if got := out.Vars["tag"]; got != int64(1790233617019699993) {
		t.Fatalf("a large integer var must come back exact, got %v (%T)", got, got)
	}
	if got := out.Vars["small"]; got != int64(12345678) {
		t.Fatalf("an integer var must not come back as a float, got %v (%T)", got, got)
	}
	if got := out.Vars["ratio"]; got != 0.5 {
		t.Fatalf("a fractional var stays a float, got %v (%T)", got, got)
	}
	if got := out.Vars["name"]; got != "x" {
		t.Fatalf("a text var stays text, got %v", got)
	}
}
