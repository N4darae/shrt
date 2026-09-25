package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
)

func TestAVolatileListStillReportsGoingEmptyOrFilling(t *testing.T) {
	full := `{"items":[{"id":"a"},{"id":"b"}],"status":"ok"}`
	for _, c := range []struct{ was, now string }{
		{full, `{"status":"ok"}`},
		{full, `{"items":[],"status":"ok"}`},
		{`{"status":"ok"}`, full},
		{`{"items":[],"status":"ok"}`, full},
	} {
		was, now := step("list", c.was), step("list", c.now)
		was.Volatile, now.Volatile = []string{"items"}, []string{"items"}
		rep := diff.Compare(spotOf(nil, was), recOf(now))
		if rep.Clean() || !strings.Contains(rep.Text(), "items") {
			t.Errorf("%s -> %s: a volatile list that went empty or filled is still a change:\n%s", c.was, c.now, rep.Text())
		}
	}
	was, now := step("list", full), step("list", `{"items":[{"id":"c"}],"status":"ok"}`)
	was.Volatile, now.Volatile = []string{"items"}, []string{"items"}
	if rep := diff.Compare(spotOf(nil, was), recOf(now)); !rep.Clean() {
		t.Errorf("other items in a volatile list are not a change:\n%s", rep.Text())
	}
}
