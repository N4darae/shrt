package namecase

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

func Closest(name string, candidates []string, limit int) []string {
	target := Fold(name)
	budget := max(2, len(target)/3)
	type scored struct {
		name string
		dist int
	}
	found := []scored{}
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		if d := distance(target, Fold(c)); d <= budget {
			found = append(found, scored{c, d})
		}
	}
	slices.SortFunc(found, func(a, b scored) int {
		return cmp.Or(cmp.Compare(a.dist, b.dist), cmp.Compare(a.name, b.name))
	})
	out := []string{}
	for _, s := range found {
		if limit > 0 && len(out) == limit {
			break
		}
		out = append(out, s.name)
	}
	return out
}

func Suggest(names []string) string {
	if len(names) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, strconv.Quote(n))
	}
	return " (did you mean " + strings.Join(quoted, " or ") + "?)"
}

func distance(a, b string) int {
	x, y := []rune(a), []rune(b)
	prev := make([]int, len(y)+1)
	cur := make([]int, len(y)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(x); i++ {
		cur[0] = i
		for j := 1; j <= len(y); j++ {
			cost := 1
			if x[i-1] == y[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(y)]
}
