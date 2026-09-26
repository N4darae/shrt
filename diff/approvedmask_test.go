package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestVolatileAddedAfterApprovalIsReportedAndCounted(t *testing.T) {
	spot := orderSpot(`{"order":{"total_minor":"500","sku":"sku-a"}}`)
	spot.Volatile = []string{"**.sku"}
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"order":{"total_minor":"999","sku":"sku-b"}}`))
	rec.Volatile = []string{"**.sku", "**.total_minor"}
	rep := diff.CompareMasking(spot, rec, []string{"**"})
	if got := strings.Join(rep.UnapprovedVolatile, ","); got != "**.total_minor,**" {
		t.Fatalf("unapproved patterns = %q, want **.total_minor,**", got)
	}
	if rep.VolatileMasked != 2 {
		t.Errorf("volatile-masked values = %d, want 2 (sku approved, total_minor not)", rep.VolatileMasked)
	}
	if strings.Join(rep.UnapprovedMasked, ",") != "fetch_order order.total_minor" {
		t.Errorf("values hidden only by unapproved patterns = %v", rep.UnapprovedMasked)
	}
	text := rep.Text()
	for _, want := range []string{"did not approve", "**.total_minor", "fetch_order order.total_minor", "2 value(s) under volatile"} {
		if !strings.Contains(text, want) {
			t.Errorf("report must say %q:\n%s", want, text)
		}
	}
	if strings.HasPrefix(text, "no drift") {
		t.Errorf("a replay masked by patterns the safe spot did not approve is not plain no drift:\n%s", text)
	}
}

func TestApprovedVolatileIsCountedButNotFlagged(t *testing.T) {
	spot := orderSpot(`{"order":{"total_minor":"500","sku":"sku-a"}}`)
	spot.Volatile = []string{"**.sku"}
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"order":{"total_minor":"500","sku":"sku-b"}}`))
	rec.Volatile = []string{"**.sku"}
	rep := diff.CompareMasking(spot, rec, []string{"**.sku"})
	if len(rep.UnapprovedVolatile) != 0 || !rep.Clean() || rep.VolatileMasked != 1 {
		t.Fatalf("approved mask: unapproved=%v clean=%v volatile-masked=%d\n%s", rep.UnapprovedVolatile, rep.Clean(), rep.VolatileMasked, rep.Text())
	}
	if strings.Join(rep.VolatilePaths, ",") != "fetch_order order.sku" {
		t.Errorf("volatile paths = %v", rep.VolatilePaths)
	}
}
