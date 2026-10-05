package chain

import (
	"fmt"
	"regexp"
	"slices"
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

func StaticLess(a, b string) (bool, bool) {
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
		for _, list := range SortedKeys(byList) {
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
	if len(idx) < 2 || mirrorsARequest(c, s, at, idx) {
		return Issue{}, false
	}
	steps, ids := []*Step{}, []string{}
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
		steps, ids = append(steps, st), append(ids, st.ID)
	}
	fields := SortedKeys(steps[0].Body)
	agree := []string{}
	for _, f := range fields {
		ascending := true
		for i := 0; i+1 < len(steps) && ascending; i++ {
			a, okA := staticText(c, steps[i].Body[f], 0)
			b, okB := staticText(c, steps[i+1].Body[f], 0)
			less, known := StaticLess(a, b)
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
	return Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindIndistinctOrder, Message: fmt.Sprintf(
		"asserts the order of %s (items from %s), but %s agree on it, so a wrong sort key passes; give 3+ fixtures "+
			"that sort differently under each key", list, strings.Join(ids, ", "), strings.Join(agree, ", ")),
		Why: "for example sku a < c < b, name b < a < c, price c < a < b, none in creation order"}, true
}

func mirrorsARequest(c *Chain, s *Step, at map[int]string, idx []int) bool {
	seen := map[string]bool{s.ID: true}
	queue := []*Step{s}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if echoesRequestList(cur.Body, at, idx) {
			return true
		}
		for _, id := range bodyRefSteps(cur.Body) {
			if seen[id] {
				continue
			}
			seen[id] = true
			if st, ok := c.Step(id); ok {
				queue = append(queue, st)
			}
		}
	}
	return false
}

func bodyRefSteps(v any) []string {
	out := []string{}
	walkText(v, "", func(_, s string) {
		for _, m := range refPattern.FindAllString(s, -1) {
			if id := refStepOf(m); id != "" && id != "vars" && id != "env" {
				out = append(out, id)
			}
		}
	})
	return out
}

func echoesRequestList(v any, at map[int]string, idx []int) bool {
	switch t := v.(type) {
	case map[string]any:
		for _, child := range t {
			if echoesRequestList(child, at, idx) {
				return true
			}
		}
	case []any:
		if !slices.ContainsFunc(idx, func(i int) bool { return i >= len(t) || !referencesStep(t[i], at[i]) }) {
			return true
		}
		for _, item := range t {
			if echoesRequestList(item, at, idx) {
				return true
			}
		}
	}
	return false
}

func referencesStep(v any, step string) bool {
	switch t := v.(type) {
	case string:
		return refStepOf(t) == step
	case map[string]any:
		for _, child := range t {
			if referencesStep(child, step) {
				return true
			}
		}
	}
	return false
}
