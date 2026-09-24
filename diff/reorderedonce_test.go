package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
)

func TestAReorderedListIsNamedOnceAsStepThenPath(t *testing.T) {
	rep := diff.Compare(listSpot(), listRun(listRunReversed, nil))
	rep.SeparateInput(listSpot(), listRun(listRunReversed, nil), nil, diff.Fixtures{})
	text := rep.Text()
	if n := strings.Count(text, "same items in another order"); n != 1 {
		t.Fatalf("the reordered list must be named once, got %d:\n%s", n, text)
	}
	if !strings.Contains(text, "list products: same items in another order") {
		t.Fatalf("the line must read `<step> <path>: same items in another order`:\n%s", text)
	}
	if len(rep.Reordered) != 1 {
		t.Fatalf("one reordered list, got %v", rep.Reordered)
	}
}
