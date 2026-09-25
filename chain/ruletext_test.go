package chain

import "testing"

func TestAFailureLineCarriesItsRule(t *testing.T) {
	for _, c := range []struct {
		r    ExpectResult
		want string
	}{
		{ExpectResult{Path: "status.code", Rule: "not_equal", Want: "SUCCESS", Got: "SUCCESS"}, "FAIL status.code want≠SUCCESS got=SUCCESS"},
		{ExpectResult{Path: "n", Rule: "equals", Want: 3, Got: 1}, "FAIL n want=3 got=1"},
		{ExpectResult{Path: "n", Rule: "gte", Want: "3", Got: 1}, "FAIL n want≥3 got=1"},
		{ExpectResult{Path: "id", Rule: "exists", Want: true, Got: false}, "FAIL id want present got absent"},
		{ExpectResult{Path: "msg", Rule: "contains", Want: "x", Got: "y"}, "FAIL msg want contains x got=y"},
	} {
		if got := c.r.String(); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
	if got := DescribeFailure(ExpectResult{Path: "status.code", Rule: "not_equal", Want: "SUCCESS", Got: "SUCCESS"}); got != "status.code want≠SUCCESS got=SUCCESS" {
		t.Errorf("DescribeFailure: %q", got)
	}
}
