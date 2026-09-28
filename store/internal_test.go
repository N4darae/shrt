package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAlsoBaselinedLeavesOutAStepsVolatileListAndCountsAnUnorderedOne(t *testing.T) {
	body := []byte(`{"status":{"code":"OK"},"products":[{"name":"a","price_minor":100},{"name":"b","price_minor":200}],"total":2}`)
	rec := &runner.Record{}
	masked := alsoBaselined(rec, &runner.StepRecord{ID: "l", Response: body, Volatile: []string{"products"}})
	if strings.Contains(masked, "products") || !strings.Contains(masked, "total=2") {
		t.Errorf("a list under the step's volatile is not baselined, so it is not listed: %q", masked)
	}
	set := alsoBaselined(rec, &runner.StepRecord{ID: "l", Response: body, Unordered: []string{"products"}})
	if strings.Contains(set, "products.0") || !strings.Contains(set, "products=2 item(s) in any order") {
		t.Errorf("an unordered list is compared as a multiset, not by index: %q", set)
	}
}

func TestTheAnsweredCellShowsEveryAssertedPairInFullNeverCutsAPath(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	t.Cleanup(func() { chain.SetEnvelope("", "") })
	st := &runner.StepRecord{ID: "add_stock_batch", Status: runner.StatusPassed,
		Response: []byte(`{"status":{"code":"SUCCESS"},"results":[{"status":{"code":"SUCCESS"},"qtyOnHand":8},` +
			`{"status":{"code":"REJECTED","details":[{"reason":"InvalidQty","appCode":1203}]},"qtyOnHand":0}]}`),
		Expect: []chain.ExpectResult{
			{Path: "status.code", Rule: "equals", Got: "SUCCESS", Passed: true},
			{Path: "results.0.status.code", Rule: "equals", Got: "SUCCESS", Passed: true},
			{Path: "results.0.qty_on_hand", Rule: "equals", Got: 8, Passed: true},
			{Path: "results.1.status.code", Rule: "equals", Got: "REJECTED", Passed: true},
			{Path: "results.1.status.details.0.reason", Rule: "equals", Got: "InvalidQty", Passed: true},
			{Path: "results.1.status.details.0.app_code", Rule: "equals", Got: 1203, Passed: true},
		}}
	cell := answeredSummary(st)
	if strings.Contains(cell, "…") {
		t.Fatalf("the answered cell must not cut inside a path or value: %q", cell)
	}
	for _, want := range []string{"results.0.qty_on_hand=8", "results.1.status.details.0.reason=InvalidQty", "results.1.status.details.0.app_code=1203"} {
		if !strings.Contains(cell, want) {
			t.Fatalf("every asserted value is what the approver approves, so %s is shown: %q", want, cell)
		}
	}
}

func TestShortValueClipsAtWordAndSegmentBoundariesAndKeepsTheTail(t *testing.T) {
	for _, tc := range []struct {
		in, keep, mustNot, endsWith string
	}{
		{in: "order is already confirmed", keep: "confirmed", mustNot: "alr…"},
		{in: "cust-pre2-order-flow@example.test", keep: "cust-", endsWith: "@example.test"},
		{in: "ord-9f8e7d6c-5b4a-3210-aaaa-bbbbccccdddd", endsWith: "bbbbccccdddd"},
		{in: "sku-t3-catalog-stock-flow-a", keep: "sku-t3", endsWith: "-a"},
		{in: "sku-t3-catalog-stock-flow-b", keep: "sku-t3", endsWith: "-b"},
		{in: "sku-t3-catalog-stock-flow-clerk", keep: "sku-t3", endsWith: "clerk"},
	} {
		got := shortValue(tc.in)
		if utf8.RuneCountInString(got) > summaryValue+1 || !strings.Contains(got, "…") {
			t.Errorf("shortValue(%q) = %q, want it clipped to the cell", tc.in, got)
		}
		if !strings.Contains(got, tc.keep) || (tc.mustNot != "" && strings.Contains(got, tc.mustNot)) || !strings.HasSuffix(got, tc.endsWith) {
			t.Errorf("shortValue(%q) = %q, want the head %q and tail %q kept, no word cut", tc.in, got, tc.keep, tc.endsWith)
		}
	}
	if got := shortValue("short"); got != "short" {
		t.Errorf("a value that fits is kept as it is, got %q", got)
	}
	in := "lines.0.qty=5 lines.1.qty=4 lines.0.id_product=prd-a273c881ff55 lines.1.id_product=prd-3b0a1b2c3d4e"
	got := clip(in, 90)
	if !strings.HasSuffix(got, "…") || strings.Contains(got, "prd-3b0…") || !strings.HasPrefix(in, strings.TrimSuffix(got, "…")) {
		t.Fatalf("clip(%q) = %q, want a prefix cut before the value it cannot hold whole", in, got)
	}
}

func approvedSpot() *SafeSpot {
	at := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	spot := &SafeSpot{Chain: "c", RunID: "r", Target: "t", Volatile: []string{"**.sku"}, ConfirmedBy: "alice@example.test", ConfirmedAt: at, Note: "checked", ProposedBy: "agent", ProposedAt: &at,
		Steps: []*runner.StepRecord{{ID: "a", Call: "S/A", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)}}}
	spot.Digest = spot.ComputeDigest()
	return spot
}

func TestSafeSpotDigestCatchesAnyEditAndAcceptsOlderSpots(t *testing.T) {
	for name, edit := range map[string]func(*SafeSpot){
		"volatile":     func(s *SafeSpot) { s.Volatile = append(s.Volatile, "**") },
		"step status":  func(s *SafeSpot) { s.Steps[0].Status = runner.StatusFailed },
		"confirmed_by": func(s *SafeSpot) { s.ConfirmedBy = "mallory@example.test" },
		"confirmed_at": func(s *SafeSpot) { s.ConfirmedAt = s.ConfirmedAt.Add(time.Hour) },
		"note":         func(s *SafeSpot) { s.Note = "rubber stamp" },
		"proposed_by":  func(s *SafeSpot) { s.ProposedBy = "someone" },
		"supersedes":   func(s *SafeSpot) { s.Supersedes = "r0" },
	} {
		spot := approvedSpot()
		if !spot.DigestMatches() || spot.DigestKind() != DigestCurrent {
			t.Fatalf("a freshly sealed spot must match as the current kind, got %q", spot.DigestKind())
		}
		if edit(spot); spot.DigestMatches() {
			t.Errorf("editing %s after approval must not match", name)
		}
	}
	spot := approvedSpot()
	spot.Digest = hashJSON(struct {
		Chain, RunID, Target, Build string
		Volatile                    []string
		Steps                       []*runner.StepRecord
	}{spot.Chain, spot.RunID, spot.Target, spot.Build, spot.Volatile, spot.Steps})
	if !spot.DigestMatches() || spot.DigestKind() != DigestWithoutApproval {
		t.Fatalf("a spot sealed before the digest covered the approval must load and say so, got %q", spot.DigestKind())
	}
	legacy := &SafeSpot{Chain: "c", Steps: []*runner.StepRecord{{ID: "a", Call: "S/A", Response: json.RawMessage(`{"n":1}`)}}}
	legacy.Digest = legacyDigest(legacy.Steps)
	if !legacy.DigestMatches() {
		t.Fatal("a safe spot approved before the digest covered everything must still load")
	}
	for _, s := range []*SafeSpot{spot, legacy} {
		if s.Steps[0].Response = json.RawMessage(`{"n":2}`); s.DigestMatches() {
			t.Fatal("an older safe spot with an edited response must not match")
		}
	}
}
