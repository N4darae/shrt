package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

const recordedCreate = `{"product":{"id":"p1","name":"w","created_at":"2026-09-01T10:00:00Z"}}`

func TestVolatileMaskStillReportsAValueThatVanishes(t *testing.T) {
	for _, got := range []string{
		`{"product":{"id":"p1","name":"w"}}`,
		`{"product":{"id":"p1","name":"w","created_at":null}}`,
		`{"product":{"id":"p1","name":"w","created_at":""}}`,
		`{"product":{"id":"p1","name":"w","created_at":"1970-01-01T00:00:00Z"}}`,
		`{"product":{"id":"p1","name":"w","created_at":"0001-01-01T00:00:00Z"}}`,
	} {
		spot := spotOf([]string{"**.created_at"}, step("create", recordedCreate))
		rep := diff.Compare(spot, recOf(step("create", got)))
		if rep.Clean() || len(rep.Changes) != 1 || rep.Changes[0].Path != "product.created_at" {
			t.Fatalf("%s: a volatile value that vanished must still be reported: %+v", got, rep.Changes)
		}
		if !strings.Contains(rep.Text(), "**.created_at") {
			t.Fatalf("%s: name the volatile pattern that did not hide it:\n%s", got, rep.Text())
		}
		a := runOf("a", stepAs("create", runner.StatusPassed, recordedCreate))
		b := runOf("b", stepAs("create", runner.StatusPassed, got))
		a.Volatile, b.Volatile = []string{"**.created_at"}, []string{"**.created_at"}
		if rr := diff.CompareRuns(a, b); rr.Same() {
			t.Fatalf("%s: shrt diff must report the vanished value too:\n%s", got, rr.Text())
		}
		if rr := diff.CompareRuns(b, a); rr.Same() {
			t.Fatalf("%s: a value appearing where the first run had none is reported too:\n%s", got, rr.Text())
		}
	}
}

func TestVolatileMaskStillHidesAChangedValue(t *testing.T) {
	spot := spotOf([]string{"**.created_at"}, step("create", recordedCreate))
	now := `{"product":{"id":"p1","name":"w","created_at":"2026-09-02T11:00:00Z"}}`
	if rep := diff.Compare(spot, recOf(step("create", now))); !rep.Clean() {
		t.Fatalf("a changed volatile value is not a difference:\n%s", rep.Text())
	}
}

func TestVolatileNullAndAbsentAreTheSameInVerifyAndDiff(t *testing.T) {
	withNull := `{"product":{"id":"p1","name":"w","created_at":null}}`
	absent := `{"product":{"id":"p1","name":"w"}}`
	for _, pair := range [][2]string{{withNull, absent}, {absent, withNull}} {
		spot := spotOf([]string{"**.created_at"}, step("create", pair[0]))
		rep := diff.Compare(spot, recOf(step("create", pair[1])))
		if !rep.Clean() || rep.VolatileMasked != 1 {
			t.Fatalf("%s -> %s: no value was lost or gained, so verify masks it as shrt diff does:\n%s", pair[0], pair[1], rep.Text())
		}
		if len(rep.VolatileValues) != 1 || rep.VolatileValues[0].Mask != "**.created_at" {
			t.Fatalf("%s -> %s: the masked value names its pattern: %+v", pair[0], pair[1], rep.VolatileValues)
		}
		if len(rep.UnapprovedMasked) > 0 {
			t.Fatalf("%s -> %s: the safe spot approved the pattern: %v", pair[0], pair[1], rep.UnapprovedMasked)
		}
		a := runOf("a", stepAs("create", runner.StatusPassed, pair[0]))
		b := runOf("b", stepAs("create", runner.StatusPassed, pair[1]))
		a.Volatile, b.Volatile = []string{"**.created_at"}, []string{"**.created_at"}
		if rr := diff.CompareRuns(a, b); !rr.Same() {
			t.Fatalf("%s -> %s: shrt diff masks it:\n%s", pair[0], pair[1], rr.Text())
		}
	}
	spot := spotOf([]string{"**.created_at"}, step("create", absent))
	if rep := diff.Compare(spot, recOf(step("create", `{"product":{"id":"p1","name":"w","created_at":"2026-09-01T10:00:00Z"}}`))); rep.Clean() {
		t.Fatalf("a value appearing where the safe spot had none is still reported:\n%s", rep.Text())
	}
}
