package chain

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
)

func unorderedPathProblem(path string, m *catalog.Method) (why string, shapeOnly bool) {
	if strings.Contains(path, "*") {
		return "is a glob; an unordered path names one list exactly, as written in the response (products, orders.lines)", true
	}
	segs := SplitPath(path)
	for _, seg := range segs {
		if isIndexSegment(seg) {
			return fmt.Sprintf("carries the index %s; an unordered path names the list itself, without indices (products, orders.lines)", seg), true
		}
	}
	if len(segs) == 0 {
		return "is empty", true
	}
	if m == nil {
		return "", false
	}
	f, found := catalog.FieldAt(catalog.DescribeMessage(m.Output()).Fields, segs)
	switch {
	case !found || f == nil:
		return fmt.Sprintf("names no field of %s", m.Output().FullName()), false
	case f.Truncated:
		return "", false
	case !f.Repeated || f.MapKey != "":
		return fmt.Sprintf("names %s.%s, which is not a repeated field, so there is no list to compare as a multiset", m.Output().FullName(), path), false
	}
	return "", false
}

func lintUnorderedStep(s *Step, m *catalog.Method) []Issue {
	issues := []Issue{}
	for _, p := range s.Unordered {
		if why, _ := unorderedPathProblem(p, m); why != "" {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
				"unordered path %q %s; verify would compare nothing as unordered there", p, why)})
		}
	}
	return issues
}

func lintUnorderedChain(c *Chain, methods []*catalog.Method) []Issue {
	issues := []Issue{}
	for _, p := range c.Unordered {
		if why, shape := unorderedPathProblem(p, nil); shape {
			issues = append(issues, Issue{Severity: SeverityError, Message: fmt.Sprintf(
				"chain unordered path %q %s; verify would compare nothing as unordered there", p, why)})
			continue
		}
		if len(methods) == 0 {
			continue
		}
		matched := false
		for _, m := range methods {
			if why, _ := unorderedPathProblem(p, m); why == "" {
				matched = true
				break
			}
		}
		if !matched {
			issues = append(issues, Issue{Severity: SeverityError, Message: fmt.Sprintf(
				"chain unordered path %q names a repeated field of no step's response in this chain; verify would compare nothing as unordered there", p)})
		}
	}
	return issues
}
