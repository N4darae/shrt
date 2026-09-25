package transport

import (
	"io"
	"strings"
	"testing"
)

func TestStillUpDropsTheCrashGuessFromAClosedConnection(t *testing.T) {
	got := StillUp("POST x: " + closedError(io.EOF, true).Error())
	for _, wrong := range []string{"stopped or crashed", "check the backend is up"} {
		if strings.Contains(got, wrong) {
			t.Fatalf("%q left in %q", wrong, got)
		}
	}
	if !strings.Contains(got, ClosedAfterSending) || !strings.Contains(got, "whether the call took effect is unknown") || strings.HasSuffix(got, ".") {
		t.Fatalf("the rest is kept: %q", got)
	}
	if other := "no answer before target.timeout."; StillUp(other) != other {
		t.Fatalf("another message is left alone: %q", StillUp(other))
	}
}
