package diff

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

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
