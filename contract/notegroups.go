package contract

import "strings"

type groupedNote struct {
	ids  []string
	rest string
}

func groupStepNotes(notes []string, steps map[string]bool) []string {
	var entries []*groupedNote
	var order []any
	byRest := map[string]*groupedNote{}
	for _, n := range notes {
		id, rest, ok := splitStepNote(n, steps)
		if !ok {
			order = append(order, n)
			continue
		}
		if g := byRest[rest]; g != nil {
			g.ids = append(g.ids, id)
			continue
		}
		g := &groupedNote{ids: []string{id}, rest: rest}
		byRest[rest] = g
		entries = append(entries, g)
		order = append(order, g)
	}
	tails := map[string]int{}
	for _, g := range entries {
		if _, tail, ok := strings.Cut(g.rest, ". "); ok {
			tails[tail]++
		}
	}
	told := map[string]bool{}
	var out []string
	for _, o := range order {
		g, ok := o.(*groupedNote)
		if !ok {
			out = append(out, o.(string))
			continue
		}
		rest := g.rest
		if head, tail, ok := strings.Cut(rest, ". "); ok && tails[tail] > 1 {
			if told[tail] {
				rest = head
			}
			told[tail] = true
		}
		label := "step " + g.ids[0]
		if len(g.ids) > 1 {
			label = "steps " + strings.Join(g.ids, ", ")
		}
		out = append(out, label+": "+rest)
	}
	return out
}

func splitStepNote(note string, steps map[string]bool) (string, string, bool) {
	after, ok := strings.CutPrefix(note, "step ")
	if !ok {
		return "", "", false
	}
	end := strings.IndexAny(after, ": ")
	if end < 0 || !steps[after[:end]] {
		return "", "", false
	}
	return after[:end], strings.TrimSpace(strings.TrimPrefix(after[end:], ":")), true
}
