package main

import (
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestASuspectWriteIsTheWriteAReadObserves(t *testing.T) {
	step := func(id, call string, refs ...string) *runner.StepRecord {
		st := &runner.StepRecord{ID: id, Call: "x.v1.S/" + call, BodyRefs: map[string]string{}}
		for i, r := range refs {
			st.BodyRefs[string(rune('a'+i))] = "${" + r + ".id}"
		}
		return st
	}
	rec := &runner.Record{Steps: []*runner.StepRecord{
		step("create_item", "CreateItem"),
		step("create_box", "CreateBox", "create_item"),
		step("move_box", "MoveBox", "create_box"),
		step("fill_item", "FillItem", "create_item"),
		step("get_item", "GetItem", "create_item"),
		step("get_item_after_move_box", "GetItem", "create_item"),
		step("list_items", "ListItems"),
	}}
	for _, c := range []struct {
		read, bad, want string
		knock           bool
	}{
		{"get_item", "", "fill_item", false},
		{"get_item", "move_box", "move_box", false},
		{"get_item_after_move_box", "fill_item", "move_box", false},
		{"list_items", "create_box", "create_box", true},
		{"list_items", "", "", false},
		{"fill_item", "", "", false},
	} {
		i, knock := suspectWrite(rec, c.read, map[string]bool{c.bad: c.bad != ""})
		got := ""
		if i >= 0 {
			got = rec.Steps[i].ID
		}
		if got != c.want || knock != c.knock {
			t.Errorf("%s with %q failing: got %q knock-on %v, want %q %v", c.read, c.bad, got, knock, c.want, c.knock)
		}
	}
}
