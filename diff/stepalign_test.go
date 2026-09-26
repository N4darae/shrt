package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func steps(spec string) []*runner.StepRecord {
	out := []*runner.StepRecord{}
	for _, part := range strings.Fields(spec) {
		id, call, _ := strings.Cut(part, ":")
		out = append(out, &runner.StepRecord{ID: id, Call: "S/" + call})
	}
	return out
}

func renameList(rs []diff.StepRename) string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.String())
	}
	return strings.Join(out, ",")
}

func TestARenameIsPairedAcrossAnInsertedOrDeletedStep(t *testing.T) {
	was := steps("mka:Create mkb:Create sta:Add stb:Add ga:Get gb:Get gc:Get")
	for _, tc := range []struct{ now, want string }{
		{"top:List mka:Create mkb:Create sta:Add stb:Add xa:Get gb:Get gc:Get", "ga -> xa"},
		{"mka:Create mkb:Create sta:Add stb:Add xa:Get gb:Get", "ga -> xa"},
		{"mka:Create mkb:Create sta:Add stb:Add ga:Get gb:Get", ""},
		{"mka:Create mkb:Create sta:Add stb:Add ga:Get xb:Get", "gb -> xb"},
	} {
		if got := renameList(diff.StepRenames(was, steps(tc.now))); got != tc.want {
			t.Errorf("now %s: want renames %q, got %q", tc.now, tc.want, got)
		}
	}
}
