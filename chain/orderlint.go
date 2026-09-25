package chain

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const KindIndistinctOrder = "indistinct-order"

var itemFieldPath = regexp.MustCompile(`^(.+)\.(\d+)\.[^.]+$`)

func refStepOf(v any) string {
	text, ok := v.(string)
	if !ok || !strings.HasPrefix(text, "${") || !strings.HasSuffix(text, "}") || strings.Count(text, "${") != 1 {
		return ""
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(text, "${"), "}")
	head, rest, ok := strings.Cut(inner, ".")
	if !ok {
		return ""
	}
	if head == "steps" {
		head, _, _ = strings.Cut(rest, ".")
	}
	return head
}

func staticText(c *Chain, v any, depth int) (string, bool) {
	switch t := v.(type) {
	case string:
		if depth > 4 {
			return t, true
		}
		out := refPattern.ReplaceAllStringFunc(t, func(m string) string {
			ref := strings.TrimSpace(m[2 : len(m)-1])
			if name, ok := strings.CutPrefix(ref, "vars."); ok {
				if val, found := c.Vars[name]; found {
					return fmt.Sprint(val)
				}
				return m
			}
			if rest, ok := strings.CutPrefix(ref, "steps."); ok {
				id, field, _ := strings.Cut(rest, ".request.")
				if s, found := c.Step(id); found && field != "" && !strings.Contains(field, ".") {
					if text, ok := staticText(c, s.Body[field], depth+1); ok {
						return text
					}
				}
			}
			return m
		})
		return out, true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case int:
		return strconv.Itoa(t), true
	}
	return "", false
}

func staticLess(a, b string) (bool, bool) {
	x, errX := strconv.ParseFloat(a, 64)
	y, errY := strconv.ParseFloat(b, 64)
	if errX == nil && errY == nil {
		return x < y, x != y
	}
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	if strings.Contains(a[:i], "${") || strings.Contains(a[i:min(len(a), i+2)], "${") || strings.Contains(b[i:min(len(b), i+2)], "${") {
		return false, false
	}
	if strings.LastIndex(a[:i], "${") > strings.LastIndex(a[:i], "}") {
		return false, false
	}
	if a == b {
		return false, false
	}
	return a < b, true
}

func lintIndistinctOrder(c *Chain) []Issue {
	issues := []Issue{}
	position := map[string]int{}
	for i, s := range c.Steps {
		position[s.ID] = i
	}
	for _, s := range c.Steps {
		byList := map[string]map[int]string{}
		for _, e := range s.Expect {
			m := itemFieldPath.FindStringSubmatch(strings.Join(SplitPath(e.Path), "."))
			if m == nil {
				continue
			}
			src := refStepOf(e.Equals)
			if src == "" || src == s.ID {
				continue
			}
			if _, known := position[src]; !known {
				continue
			}
			idx, _ := strconv.Atoi(m[2])
			if byList[m[1]] == nil {
				byList[m[1]] = map[int]string{}
			}
			if prev, seen := byList[m[1]][idx]; seen && prev != src {
				byList[m[1]][idx] = ""
				continue
			}
			byList[m[1]][idx] = src
		}
		lists := make([]string, 0, len(byList))
		for l := range byList {
			lists = append(lists, l)
		}
		sort.Strings(lists)
		for _, list := range lists {
			if issue, ok := indistinctOrderIssue(c, s, list, byList[list], position); ok {
				issues = append(issues, issue)
			}
		}
	}
	return issues
}

func indistinctOrderIssue(c *Chain, s *Step, list string, at map[int]string, position map[string]int) (Issue, bool) {
	idx := []int{}
	for i, src := range at {
		if src != "" {
			idx = append(idx, i)
		}
	}
	sort.Ints(idx)
	if len(idx) < 2 {
		return Issue{}, false
	}
	steps := []*Step{}
	seen := map[string]bool{}
	for _, i := range idx {
		src := at[i]
		if seen[src] {
			return Issue{}, false
		}
		seen[src] = true
		st, _ := c.Step(src)
		if st == nil || (len(steps) > 0 && st.Call != steps[0].Call) {
			return Issue{}, false
		}
		steps = append(steps, st)
	}
	fields := []string{}
	for k := range steps[0].Body {
		fields = append(fields, k)
	}
	sort.Strings(fields)
	agree := []string{}
	for _, f := range fields {
		ascending := true
		for i := 0; i+1 < len(steps) && ascending; i++ {
			a, okA := staticText(c, steps[i].Body[f], 0)
			b, okB := staticText(c, steps[i+1].Body[f], 0)
			less, known := staticLess(a, b)
			ascending = okA && okB && known && less
		}
		if ascending {
			agree = append(agree, f)
		}
	}
	asc, desc := true, true
	for i := 0; i+1 < len(steps); i++ {
		asc = asc && position[steps[i].ID] < position[steps[i+1].ID]
		desc = desc && position[steps[i].ID] > position[steps[i+1].ID]
	}
	if asc {
		agree = append(agree, "creation order")
	}
	if desc {
		agree = append(agree, "newest-first creation order")
	}
	if len(agree) < 2 {
		return Issue{}, false
	}
	ids := make([]string, 0, len(steps))
	for _, st := range steps {
		ids = append(ids, st.ID)
	}
	return Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindIndistinctOrder, Message: fmt.Sprintf(
		"asserts the order of %s (items from %s, in that order), but %s all put those items in the same order, so the "+
			"assertion cannot tell which key the backend sorts by: one sorting by the wrong key passes it too. Give the "+
			"fixtures values that sort differently under each key, with three or more items (sku a < c < b, name b < a < c, "+
			"price c < a < b, none in creation order)",
		list, strings.Join(ids, ", "), strings.Join(agree, ", "))}, true
}
