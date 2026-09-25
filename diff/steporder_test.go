package diff

import (
	"strings"
	"testing"
)

func TestAStepOrderChangeNamesTheMovedStepsWithoutAStrayDash(t *testing.T) {
	order := Change{Step: "-", Path: "steps", Kind: KindOrder, Want: "a, b, c, d", Got: "a, c, b, d"}
	r := &Report{SafeSpotID: "run-1", RequestChanges: []Change{order}, Changes: []Change{order}}
	text := r.Text()
	if strings.Contains(text, "at - steps") || strings.Contains(text, "[-]") {
		t.Fatalf("the order change prints a stray dash:\n%s", text)
	}
	for _, want := range []string{"step order", "b step 2 -> 3", "c step 3 -> 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the order change lacks %q:\n%s", want, text)
		}
	}
}
