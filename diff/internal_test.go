package diff

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func TestAStringWithEdgeSpacesOrNoTextIsShownQuoted(t *testing.T) {
	for _, c := range []struct {
		change Change
		want   string
	}{
		{Change{Kind: KindChanged, Want: "  name  ", Got: "  name"}, `want="  name  " got="  name"`},
		{Change{Kind: KindChanged, Want: "", Got: "name"}, `want="" got=name`},
		{Change{Kind: KindChanged, Want: 3.0, Got: "a b"}, `want=3 got=a b`},
	} {
		if got := c.change.describe(); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

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
	if !strings.Contains(text, "4 steps") || !strings.Contains(text, "[b..e]") || !strings.Contains(text, why) {
		t.Fatalf("the summary must name the count, the steps and the reason:\n%s", text)
	}
	if r.Counted() != 1 {
		t.Fatalf("collapsing changes no count, and the steps not reached are not counted as changes: %d\n%s", r.Counted(), text)
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

func latStep(id, call string, ms int64, resent ...int64) *runner.StepRecord {
	return &runner.StepRecord{ID: id, Call: call, Status: runner.StatusPassed, HTTPStatus: 200, LatencyMS: ms, LatencyResent: resent}
}

func TestLatencyFlagsAStepSlowerThanBothTheFloorAndTheRatio(t *testing.T) {
	spot := []*runner.StepRecord{latStep("create", "P/CreateProduct", 2), latStep("list", "P/ListProducts", 1)}
	rec := &runner.Record{Steps: []*runner.StepRecord{latStep("create", "P/CreateProduct", 3), latStep("list", "P/ListProducts", 702, 701, 703)}}
	flags := LatencyRegressions(spot, rec, nil, LatencyPolicyFrom(nil))
	if len(flags) != 1 || flags[0].Step != "list" || !flags[0].Confirmed || flags[0].AfterMS != 701 || flags[0].BeforeMS != 1 {
		t.Fatalf("want one confirmed flag on list judged on its fastest sample, got %+v", flags)
	}
	line := flags[0].Line()
	for _, want := range []string{"LATENCY", "ListProducts", "list", "1ms", "701ms", "+700ms"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q lacks %q", line, want)
		}
	}
}

func TestLatencyIgnoresASlowCallThatAReMeasurementAnsweredFast(t *testing.T) {
	spot := []*runner.StepRecord{latStep("list", "P/ListProducts", 1)}
	rec := &runner.Record{Steps: []*runner.StepRecord{latStep("list", "P/ListProducts", 900, 2)}}
	if flags := LatencyRegressions(spot, rec, nil, LatencyPolicyFrom(nil)); len(flags) != 0 {
		t.Fatalf("one slow call answered fast when re-sent is noise, got %+v", flags)
	}
}

func TestLatencyNeedsBothTheFloorAndTheRatio(t *testing.T) {
	p := LatencyPolicyFrom(nil)
	if p.Exceeds(1, 200) {
		t.Fatal("under the 250ms floor is not flagged")
	}
	if p.Exceeds(400, 900) {
		t.Fatal("+500ms but under 3x is not flagged")
	}
	if !p.Exceeds(100, 400) {
		t.Fatal("+300ms and 4x is flagged")
	}
	floor, ratio := int64(50), 2.0
	p = LatencyPolicyFrom(&config.Latency{FloorMS: &floor, Ratio: &ratio})
	if !p.Exceeds(40, 100) {
		t.Fatal("configured floor and ratio apply")
	}
}

func TestLatencyOnAWriteIsConfirmedOnlyByThePreviousRun(t *testing.T) {
	spot := []*runner.StepRecord{latStep("create", "P/CreateProduct", 2)}
	rec := &runner.Record{Steps: []*runner.StepRecord{latStep("create", "P/CreateProduct", 800)}}
	flags := LatencyRegressions(spot, rec, nil, LatencyPolicyFrom(nil))
	if len(flags) != 1 || flags[0].Confirmed {
		t.Fatalf("a write measured once is reported unconfirmed, got %+v", flags)
	}
	prev := &runner.Record{RunID: "r1", Steps: []*runner.StepRecord{latStep("create", "P/CreateProduct", 790)}}
	flags = LatencyRegressions(spot, rec, prev, LatencyPolicyFrom(nil))
	if len(flags) != 1 || !flags[0].Confirmed || !strings.Contains(flags[0].Line(), "r1") {
		t.Fatalf("a previous run slow at the same step confirms it, got %+v", flags)
	}
}

func TestLatencyOffFlagsNothing(t *testing.T) {
	spot := []*runner.StepRecord{latStep("list", "P/ListProducts", 1)}
	rec := &runner.Record{Steps: []*runner.StepRecord{latStep("list", "P/ListProducts", 900, 900, 900)}}
	if flags := LatencyRegressions(spot, rec, nil, LatencyPolicyFrom(&config.Latency{Off: true})); len(flags) != 0 {
		t.Fatalf("latency: {off: true} turns the check off, got %+v", flags)
	}
}

func pairItemsAllPairs(want, got []any, r *strings.Replacer) []int {
	type cand struct{ i, j, score int }
	cands := []cand{}
	for i := range want {
		for j := range got {
			cands = append(cands, cand{i, j, likeness(want[i], got[j], r)})
		}
	}
	sort.Slice(cands, func(a, b int) bool {
		x, y := cands[a], cands[b]
		if x.score != y.score {
			return x.score > y.score
		}
		if dx, dy := absInt(x.i-x.j), absInt(y.i-y.j); dx != dy {
			return dx < dy
		}
		if x.i != y.i {
			return x.i < y.i
		}
		return x.j < y.j
	})
	order := make([]int, len(want))
	for i := range order {
		order[i] = -1
	}
	used := make([]bool, len(got))
	for _, c := range cands {
		if order[c.i] < 0 && !used[c.j] {
			order[c.i], used[c.j] = c.j, true
		}
	}
	return order
}

func randomItem(rng *rand.Rand) any {
	item := map[string]any{"id": fmt.Sprintf("id-%d", rng.Intn(6)), "status": []string{"A", "B"}[rng.Intn(2)]}
	if rng.Intn(2) == 0 {
		item["qty"] = float64(rng.Intn(3))
	}
	if rng.Intn(3) == 0 {
		item["lines"] = []any{map[string]any{"sku": fmt.Sprintf("s-%d", rng.Intn(3))}, fmt.Sprintf("x-%d", rng.Intn(2))}
	}
	if rng.Intn(5) == 0 {
		return fmt.Sprintf("id-%d", rng.Intn(6))
	}
	return item
}

func TestPairItemsMatchesTheAllPairsGreedyPairing(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	renamer := strings.NewReplacer("id-1", "id-4", "id-2", "id-5")
	for n := 0; n < 3000; n++ {
		want, got := []any{}, []any{}
		for range rng.Intn(7) {
			want = append(want, randomItem(rng))
		}
		for range rng.Intn(7) {
			got = append(got, randomItem(rng))
		}
		r := renamer
		if n%2 == 0 {
			r = nil
		}
		if a, b := pairItems(want, got, r), pairItemsAllPairs(want, got, r); !reflect.DeepEqual(a, b) {
			t.Fatalf("case %d: pairItems %v, all pairs %v\nwant %v\ngot  %v", n, a, b, want, got)
		}
	}
}

func TestPairItemsOnALongListIsNotQuadraticInTime(t *testing.T) {
	items := func(n int) []any {
		out := make([]any, n)
		for i := range out {
			out[i] = map[string]any{"id_product": fmt.Sprintf("prd-%06d", i), "sku": fmt.Sprintf("sku-%06d", i), "status": "ACTIVE", "qty": float64(i % 7)}
		}
		return out
	}
	want, got := items(3000), items(3000)
	start := time.Now()
	order := pairItems(want, got, nil)
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("pairing 3000 items took %s", took)
	}
	for i, j := range order {
		if i != j {
			t.Fatalf("identical lists pair item %d with %d", i, j)
		}
	}
}

func likeness(want, got any, r *strings.Replacer) int {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return 0
		}
		n := 0
		for k, wv := range w {
			if gv, ok := g[k]; ok {
				n += likeness(wv, gv, r)
			}
		}
		return n
	case []any:
		g, ok := got.([]any)
		if !ok {
			return 0
		}
		n := 0
		for i := range min(len(w), len(g)) {
			n += likeness(w[i], g[i], r)
		}
		return n
	case string:
		g, ok := got.(string)
		if !ok {
			return 0
		}
		if w == g {
			return 1
		}
		if r != nil && r.Replace(w) == g {
			return 2
		}
		return 0
	}
	if jsonKind(want) == jsonKind(got) && sameScalar(want, got) {
		return 1
	}
	return 0
}
