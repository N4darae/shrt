package chain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
)

func scopeAt(t time.Time) *chain.Scope {
	s := chain.NewScope(nil)
	s.Now = func() time.Time { return t }
	return s
}

const midAfternoon = "2026-09-18T15:04:05Z"

func at(t *testing.T, stamp string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func resolved(t *testing.T, s *chain.Scope, ref string) string {
	t.Helper()
	v, err := s.ResolveValue(ref)
	if err != nil {
		t.Fatalf("%s: %v", ref, err)
	}
	got, ok := v.(string)
	if !ok {
		t.Fatalf("%s resolved to %T, and every clock form is a string so an int64 proto field accepts it", ref, v)
	}
	return got
}

func TestTodayIsTheUTCMidnightOfTheRunNotTheWallClock(t *testing.T) {
	s := scopeAt(at(t, midAfternoon))

	if got, want := resolved(t, s, "${today}"), "1789689600"; got != want {
		t.Errorf("${today} = %s, want %s (2026-09-18T00:00:00Z): a business date is a multiple of 86400, "+
			"and the whole point is not having to hardcode one", got, want)
	}
	if got, want := resolved(t, s, "${today-86400}"), "1789603200"; got != want {
		t.Errorf("${today-86400} = %s, want %s (the day before)", got, want)
	}
	if got, want := resolved(t, s, "${today+86400}"), "1789776000"; got != want {
		t.Errorf("${today+86400} = %s, want %s (the day after)", got, want)
	}
}

func TestAnOffsetReachesABoundedFutureTimestamp(t *testing.T) {
	s := scopeAt(at(t, midAfternoon))
	base := resolved(t, s, "${nowunix}")

	if got, want := resolved(t, s, "${nowunix+3600}"), "1789747445"; got != want {
		t.Errorf("${nowunix+3600} = %s, want %s (an hour past %s)", got, want, base)
	}
	if got, want := resolved(t, s, "${now+259200}"), "2026-09-21T15:04:05Z"; got != want {
		t.Errorf("${now+259200} = %s, want %s: expires_at fields want three days ahead, not a literal "+
			"that goes stale", got, want)
	}
}

func TestTheClockIsPinnedForTheWholeRun(t *testing.T) {
	ticks := 0
	s := chain.NewScope(nil)
	s.Now = func() time.Time {
		ticks++
		return at(t, midAfternoon).Add(time.Duration(ticks) * time.Second)
	}

	first := resolved(t, s, "${nowunix}")
	second := resolved(t, s, "${nowunix}")
	day := resolved(t, s, "${today}")
	again := resolved(t, s, "${today}")

	if first != second || day != again {
		t.Fatalf("two reads of the same clock form drifted (%s vs %s, %s vs %s). A chain writes a "+
			"business date in one step and reads it in another; if they disagree the read matches "+
			"nothing and the step passes on an empty answer", first, second, day, again)
	}
	if ticks != 1 {
		t.Errorf("the clock was read %d times; pinning it once per scope is what makes the run reproducible", ticks)
	}
}

func TestAMalformedOffsetSaysSoRatherThanLookingLikeAMissingStep(t *testing.T) {
	_, err := scopeAt(at(t, midAfternoon)).ResolveValue("${today-oneday}")
	if err == nil {
		t.Fatal("${today-oneday} must not resolve")
	}
	if !strings.Contains(err.Error(), "seconds") {
		t.Errorf("the error has to name the unit, or the author tries ${today-1d} next: %q", err)
	}
}

func TestAHyphenatedStepIdIsNotReadAsAnOffset(t *testing.T) {
	s := chain.NewScope(nil)
	s.Record("create-thing", nil, map[string]any{"id": "x-1"})

	if got := resolved(t, s, "${create-thing.id}"); got != "x-1" {
		t.Errorf("a step id may contain a hyphen and only now/nowunix/today take an offset: got %q", got)
	}
}
