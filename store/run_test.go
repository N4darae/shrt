package store_test

import (
	"testing"
	"time"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestLatestRunOrdersSameSecondRunsByStartTimeNotBySuffix(t *testing.T) {
	s := store.New(t.TempDir(), t.TempDir())
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	first := &runner.Record{RunID: "20260923T100000Z-ffffffff", Chain: "thing-flow", StartedAt: base.Add(100 * time.Millisecond)}
	second := &runner.Record{RunID: "20260923T100000Z-00000000", Chain: "thing-flow", StartedAt: base.Add(700 * time.Millisecond)}
	for _, r := range []*runner.Record{first, second} {
		if _, err := s.SaveRun(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.LatestRun("thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	if got.RunID != second.RunID {
		t.Fatalf("latest = %s, want %s: both ids share a second and the random suffix is not an order",
			got.RunID, second.RunID)
	}
	ids, err := s.ListRuns("thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != first.RunID || ids[1] != second.RunID {
		t.Fatalf("ListRuns = %v, want oldest first", ids)
	}
}
