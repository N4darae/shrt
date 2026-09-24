package diff

import (
	"strings"
	"testing"
)

func TestTextCollapsesIdenticalNotReachedLinesIntoOne(t *testing.T) {
	why := `not sent: the target http://127.0.0.1:1 is unreachable (connection refused at step "a")`
	r := &Report{SafeSpotID: "spot", Changes: []Change{
		{Step: "a", Path: "status", Kind: KindStatus, Want: "passed", Got: "error", Detail: "auth login: connection refused"},
	}}
	for _, id := range []string{"b", "c", "d", "e"} {
		r.Changes = append(r.Changes, Change{Step: id, Path: "status", Kind: KindNotReached, Want: "passed", Got: "skipped", Detail: why})
	}
	text := r.Text()
	if n := strings.Count(text, "not_reached"); n != 1 {
		t.Fatalf("four steps not sent for the same reason must be one line, got %d:\n%s", n, text)
	}
	if !strings.Contains(text, "4 step(s)") || !strings.Contains(text, "[b..e]") || !strings.Contains(text, why) {
		t.Fatalf("the summary must name the count, the steps and the reason:\n%s", text)
	}
	if !strings.Contains(text, "5 change(s)") {
		t.Fatalf("the change count is unchanged:\n%s", text)
	}
}

func TestTextKeepsASingleNotReachedLineAsItIs(t *testing.T) {
	r := &Report{SafeSpotID: "spot", Changes: []Change{
		{Step: "b", Path: "status", Kind: KindNotReached, Want: "passed", Got: "skipped", Detail: "held back"},
		{Step: "c", Path: "status", Kind: KindNotReached, Want: "passed", Got: "skipped", Detail: "another reason"},
	}}
	text := r.Text()
	if !strings.Contains(text, "[b] not_reached") || !strings.Contains(text, "[c] not_reached") {
		t.Fatalf("different reasons stay on their own lines:\n%s", text)
	}
}
