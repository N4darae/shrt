package doctor

import "testing"

func TestSummaryPluralizesWarnings(t *testing.T) {
	cases := []struct {
		fails, warns, oks int
		want              string
	}{
		{0, 1, 2, "1 warning, 2 ok"},
		{0, 3, 2, "3 warnings, 2 ok"},
		{1, 3, 0, "1 failing, 3 warnings, 0 ok"},
		{2, 1, 1, "2 failing, 1 warning, 1 ok"},
	}
	for _, c := range cases {
		r := &Report{}
		for i := 0; i < c.fails; i++ {
			r.add(CheckAuth, LevelError, "f", "")
		}
		for i := 0; i < c.warns; i++ {
			r.add(CheckAuth, LevelWarn, "w", "")
		}
		for i := 0; i < c.oks; i++ {
			r.add(CheckAuth, LevelOK, "o", "")
		}
		if got := r.Summary(); got != c.want {
			t.Errorf("Summary() = %q, want %q", got, c.want)
		}
	}
}
