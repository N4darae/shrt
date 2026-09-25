package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestAKeptRedChainWhoseDefectIsGoneSaysSoInItsHeadline(t *testing.T) {
	rec := &runner.Record{Chain: "red", Status: runner.StatusPassed, KeptRed: runner.KeptRedGone,
		KeptRedNote: "every step passed, so the defect kept_red pins (s p) is gone: check the fix is the one intended, then remove kept_red and assert the corrected behaviour"}
	out := summary(rec, false)
	head, _, _ := strings.Cut(out, "\n")
	if strings.Contains(head, "PASSED in") || !strings.Contains(head, "PINNED DEFECT GONE") {
		t.Fatalf("a kept-red chain that passed exits 1, so its headline must not read PASSED: %q", head)
	}
}

func TestAKeptRedNoteOnACollisionIsOneShortSentence(t *testing.T) {
	note := keptRedNotJudged(`kept_red pins restock_a qty_on_hand got=10, but step "create_product_a" failed where nothing is pinned`)
	if !strings.HasPrefix(note, "not judged: kept_red pins restock_a qty_on_hand got=10,") || strings.Contains(note, "create_product_a") {
		t.Fatalf("after a fixture collision the step-by-step note says nothing about the defect: %q", note)
	}
}
