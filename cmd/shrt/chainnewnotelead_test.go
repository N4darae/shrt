package main

import "testing"

func TestChainNewPrintsTheLeadOfEachNote(t *testing.T) {
	for note, want := range map[string]string{
		"steps a, b: x.created_at is asserted within 300s of ${nowunix}: stamped by this call; verify masks it": "steps a, b: x.created_at is asserted within 300s of ${nowunix}",
		"step c: asserts only the verdict, though the contract for S/C declares what its response carries (x). The verdict says": "step c: asserts only the verdict, though the contract for S/C declares what its response carries",
		"step c: id wants S/P but that rpc is not in the plan": "step c: id wants S/P but that rpc is not in the plan",
	} {
		if got := noteLead(note); got != want {
			t.Errorf("noteLead(%q) = %q, want %q", note, got, want)
		}
	}
}
