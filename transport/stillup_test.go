package transport

import (
	"io"
	"strings"
	"testing"
)

func TestStillUpDropsTheCrashGuessFromAClosedConnection(t *testing.T) {
	closed := "POST x: " + closedError(io.EOF, true).Error()
	for _, got := range []string{StillUp(closed), StillUp(AnsweredLater(closed))} {
		for _, wrong := range []string{"stopped or crashed", "check the backend is up", "likely broke it"} {
			if strings.Contains(got, wrong) {
				t.Fatalf("%q left in %q", wrong, got)
			}
		}
		if !strings.Contains(got, ClosedAfterSending) || !strings.Contains(got, "whether the call took effect is unknown") || strings.HasSuffix(got, ".") {
			t.Fatalf("the rest is kept: %q", got)
		}
	}
	if notSent := closedError(io.EOF, false).Error(); AnsweredLater(notSent) != notSent {
		t.Fatalf("a request never written cannot have broken the backend: %q", AnsweredLater(notSent))
	}
	if other := "no answer before target.timeout."; StillUp(other) != other {
		t.Fatalf("another message is left alone: %q", StillUp(other))
	}
}
