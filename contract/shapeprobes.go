package contract

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var (
	clauseSplit    = regexp.MustCompile(`(?i)\s*(?:,|;|\bor\b)\s*`)
	shapeEmpty     = regexp.MustCompile(`(?i)\b(?:empty|missing|blank|absent|unset|omitted|not set|not given|required|none)\b`)
	shapeSpace     = regexp.MustCompile(`(?i)\bwhitespace\b|\bspaces\b`)
	shapeZero      = regexp.MustCompile(`(?i)\bzero\b|(?:^|\s)0(?:\s|$)`)
	shapeNegative  = regexp.MustCompile(`(?i)\bnegative\b|\bbelow zero\b|\bless than zero\b`)
	shapeAt        = regexp.MustCompile(`(?i)@|\bat[- ]?(?:sign|symbol|character|char|mark)\b`)
	invalidArgCode = "invalid_argument"
)

type shapeCase struct {
	path  string
	field string
	kind  string
	value any
}

func (p *Plan) probeShapes(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || p.isLogin(st.Call) {
			continue
		}
		c, ok := lib.Get(st.Call)
		if !ok {
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		fields := catalog.DescribeMessage(m.Input()).Fields
		covered := map[string]bool{}
		declared := false
		for _, f := range lib.AllFailures(st.Call) {
			if f.ConnectCode != invalidArgCode || f.Code != 0 || f.Unreachable != "" {
				continue
			}
			declared = true
			if f.Field != "" {
				covered[namecase.Fold(stripIndexes(f.Field))] = true
			}
			cases, unread := shapeCases(st.Body, fields, f)
			if len(cases) == 0 {
				p.gap("step %s: %s's when: names no field and value the plan can build, so no malformed request was "+
					"planned for it: write one, or reword it (%s)", st.ID, f.Label(), shapeWording)
				continue
			}
			p.addShapeProbes(lib, st, m, c, f, cases)
			if len(unread) > 0 {
				p.gap("step %s: %s: %q names no field and value the plan can build, so no probe sends that case: "+
					"write one, or reword it (%s)", st.ID, f.Label(), strings.Join(unread, `", "`), shapeWording)
			}
		}
		required := []string{}
		for _, r := range c.Required {
			if IsRequiredLiteral(r) {
				continue
			}
			declared = true
			if !covered[namecase.Fold(stripIndexes(r))] {
				required = append(required, r)
			}
		}
		if len(required) > 0 {
			p.gap("step %s: %s %s required, but no invalid_argument failure has field: naming %s, so no probe sends a "+
				"request without %s: declare that failure", st.ID, strings.Join(required, ", "), pluralIs(len(required)),
				pluralVerb(len(required), "it", "them"), pluralVerb(len(required), "it", "them"))
		}
		if !declared && !c.IsUnfilled("required") && len(c.Required) == 0 {
			p.gap("step %s: nothing in its contract declares a required field or a format (required:, or a failure with "+
				"connect_code: invalid_argument), so no malformed request was planned: the plan does not guess what the "+
				"handler validates", st.ID)
		}
	}
}

const shapeWording = "empty, missing, blank, whitespace, zero, negative, an at sign"

func shapeCases(body map[string]any, fields []*catalog.Field, f Failure) ([]shapeCase, []string) {
	out := []shapeCase{}
	unread := []string{}
	seen := map[string]bool{}
	carry := f.Field
	for _, clause := range clauseSplit.Split(f.When, -1) {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		field := mentionedField(clause, fields, f.Field)
		if field == "" {
			field = carry
		}
		if field == "" {
			unread = append(unread, clause)
			continue
		}
		carry = field
		path, fd, cur, ok := shapeTarget(body, fields, field)
		if !ok {
			continue
		}
		kind, value, ok := shapeValue(clause, fd, cur)
		if !ok {
			unread = append(unread, clause)
			continue
		}
		if seen[path+"|"+kind] {
			continue
		}
		seen[path+"|"+kind] = true
		out = append(out, shapeCase{path: path, field: field, kind: kind, value: value})
	}
	return out, unread
}

func fieldWord(name string) *regexp.Regexp {
	parts := strings.Split(name, "_")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return regexp.MustCompile(`(?i)\b` + strings.Join(parts, `[_ ]`) + `\b`)
}

func singularWord(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(strings.TrimSuffix(name, "s")) + `s?\b`)
}

func mentionedField(clause string, fields []*catalog.Field, declared string) string {
	top := ""
	for _, fd := range fields {
		if fd.MapKey != "" {
			continue
		}
		hit := fieldWord(fd.Name).MatchString(clause) || (fd.Repeated && singularWord(fd.Name).MatchString(clause))
		if fd.Repeated && fd.Kind == "message" && (hit || namecase.Fold(declared) == namecase.Fold(fd.Name)) {
			for _, sub := range fd.Fields {
				if !sub.Repeated && sub.Kind != "message" && fieldWord(sub.Name).MatchString(clause) {
					return fd.Name + "." + sub.Name
				}
			}
		}
		if hit && top == "" {
			top = fd.Name
		}
	}
	return top
}

func shapeTarget(body map[string]any, fields []*catalog.Field, field string) (string, *catalog.Field, any, bool) {
	segs := chain.SplitPath(stripIndexes(field))
	fd, ok := catalog.FieldAt(fields, segs)
	if !ok || fd == nil {
		return "", nil, nil, false
	}
	path := strings.Join(segs, ".")
	if len(segs) == 2 {
		key, ok := namecase.LookupKey(body, segs[0])
		list, isList := body[key].([]any)
		if !ok || !isList || len(list) == 0 {
			return "", nil, nil, false
		}
		path = fmt.Sprintf("%s.%d.%s", key, len(list)-1, segs[1])
	} else if len(segs) != 1 {
		return "", nil, nil, false
	}
	cur, _ := bodyValue(body, path)
	return path, fd, cur, true
}

func shapeValue(clause string, fd *catalog.Field, cur any) (string, any, bool) {
	switch {
	case fd.Repeated:
		if shapeEmpty.MatchString(clause) {
			return "empty", []any{}, true
		}
	case fd.Kind == "string":
		text, _ := cur.(string)
		switch {
		case shapeAt.MatchString(clause) && strings.Contains(text, "@"):
			return "no_at", strings.ReplaceAll(text, "@", "."), true
		case shapeSpace.MatchString(clause):
			return "blank", "   ", true
		case shapeEmpty.MatchString(clause):
			return "empty", "", true
		}
	case chain.IsNumericKind(fd.Kind):
		switch {
		case shapeNegative.MatchString(clause):
			return "negative", "-1", true
		case shapeZero.MatchString(clause):
			return "zero", "0", true
		}
	}
	return "", nil, false
}

func (p *Plan) addShapeProbes(lib *Library, st *chain.Step, m *catalog.Method, c *RPCContract, f Failure, cases []shapeCase) {
	expect := refusalFor(m, f)
	ids := []string{}
	for _, sc := range cases {
		probe := probeStep(st, p.freeStepID(st.ID+"_"+leafName(sc.field)+"_"+sc.kind))
		if !chain.IsReadOnlyCall(st.Call) {
			p.freshen(lib, probe)
		}
		setBodyPath(probe.Body, sc.path, sc.value)
		probe.Expect = append([]chain.Expectation{}, expect...)
		probe.Description = fmt.Sprintf("%s %s: a malformed request, answered %s before any business rule runs.", sc.path, shapeWords(sc.kind), f.Label())
		p.addShape(lib, st, probe)
		ids = append(ids, probe.ID)
	}
	p.note("step %s: its contract declares %s (%s), so %s %s a malformed request and %s %s", st.ID, f.Label(),
		strings.TrimSpace(f.When), strings.Join(ids, ", "), pluralVerb(len(ids), "sends", "send"), pluralVerb(len(ids), "expects", "expect"),
		f.Label())
}

func (p *Plan) addShape(lib *Library, st *chain.Step, probe *chain.Step) {
	if chain.IsReadOnlyCall(st.Call) || len(referencedSteps(probe.Body)) == 0 {
		p.Chain.Steps = append(p.Chain.Steps, probe)
		return
	}
	p.Chain.Steps = append(p.Chain.Steps, p.guardUnchanged(lib, []*chain.Step{probe}, probe.ID)...)
}

func shapeWords(kind string) string {
	switch kind {
	case "empty":
		return "empty"
	case "blank":
		return "only whitespace"
	case "zero":
		return "zero"
	case "negative":
		return "negative"
	case "no_at":
		return "without an @"
	}
	return kind
}
