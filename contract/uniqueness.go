package contract

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var (
	duplicateReason = regexp.MustCompile(`^(?:Duplicate|NonUnique|NotUnique)([A-Z][A-Za-z0-9]*)$`)
	takenReason     = regexp.MustCompile(`^([A-Z][A-Za-z0-9]*?)(?:AlreadyExists|AlreadyTaken|AlreadyInUse|AlreadyUsed|Exists|Taken|Duplicate|Duplicated|InUse|NotUnique)$`)
	uniqueWhen      = regexp.MustCompile(`(?i)\bunique\b|\bduplicate\b`)
	caseIgnored     = regexp.MustCompile(`(?i)ignor\w*\s+(?:the\s+)?(?:letter\s+)?case|case[- ]?insensitiv|regardless\s+of\s+(?:letter\s+)?case|(?:in|of)\s+any\s+(?:letter\s+)?case`)
	freshValueRef   = regexp.MustCompile(`\$\{\s*(?:uuid|now|nowunix|today)(?:[+-][^}]*)?\s*\}`)
	anyValueRef     = regexp.MustCompile(`\$\{[^}]*\}`)
	varValueRef     = regexp.MustCompile(`\$\{\s*vars\.[A-Za-z0-9_]+\s*\}`)
)

func uniquenessNoun(f Failure) (string, bool) {
	if m := duplicateReason.FindStringSubmatch(f.Reason); m != nil {
		return m[1], true
	}
	if m := takenReason.FindStringSubmatch(f.Reason); m != nil {
		return m[1], true
	}
	if uniqueWhen.MatchString(f.When) {
		return "", true
	}
	return "", false
}

func (p *Plan) uniqueField(step *chain.Step, c *RPCContract, f Failure, noun string) string {
	if f.Field != "" {
		if _, ok := bodyValue(step.Body, f.Field); ok {
			return f.Field
		}
	}
	keys := make([]string, 0, len(step.Body))
	for k := range step.Body {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if noun != "" {
		want := namecase.Fold(noun)
		for _, k := range keys {
			if namecase.Fold(k) == want {
				return k
			}
		}
		for _, k := range keys {
			if fk := namecase.Fold(k); strings.HasSuffix(fk, want) || strings.HasPrefix(fk, want) {
				return k
			}
		}
	}
	marked := []string{}
	for _, name := range sortedFieldNames(c.Fields) {
		note := strings.ToLower(c.Fields[name].Note)
		if strings.Contains(note, "unique") || strings.Contains(note, "unused") {
			if _, ok := bodyValue(step.Body, name); ok {
				marked = append(marked, name)
			}
		}
	}
	if len(marked) == 1 {
		return marked[0]
	}
	return ""
}

func swapLiteralCase(v string) string {
	out := strings.Builder{}
	last := 0
	flip := func(s string) {
		for _, r := range s {
			switch {
			case unicode.IsLower(r):
				out.WriteRune(unicode.ToUpper(r))
			case unicode.IsUpper(r):
				out.WriteRune(unicode.ToLower(r))
			default:
				out.WriteRune(r)
			}
		}
	}
	for _, loc := range anyValueRef.FindAllStringIndex(v, -1) {
		flip(v[last:loc[0]])
		out.WriteString(v[loc[0]:loc[1]])
		last = loc[1]
	}
	flip(v[last:])
	return out.String()
}

func stableAcrossSteps(v string) string {
	if !freshValueRef.MatchString(v) {
		return v
	}
	if !varValueRef.MatchString(v) {
		return freshValueRef.ReplaceAllString(v, "${vars.tag}")
	}
	out := freshValueRef.ReplaceAllString(v, "")
	for _, sep := range []string{"-", "_", "."} {
		for strings.Contains(out, sep+sep) {
			out = strings.ReplaceAll(out, sep+sep, sep)
		}
	}
	return strings.Trim(out, "-_")
}

func leafName(path string) string {
	segs := chain.SplitPath(path)
	if len(segs) == 0 {
		return path
	}
	return segs[len(segs)-1]
}

func (p *Plan) probeUniqueness(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || chain.IsReadOnlyCall(st.Call) {
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
		done := map[string]bool{}
		for _, f := range lib.AllFailures(st.Call) {
			noun, unique := uniquenessNoun(f)
			if !unique {
				continue
			}
			field := p.uniqueField(st, c, f, noun)
			if field == "" {
				p.note("step %s: the contract declares %s, a uniqueness refusal, but names no field it is about "+
					"(set field: on the failure), so no duplicate attempt was planned", st.ID, f.Label())
				continue
			}
			if done[field] {
				continue
			}
			done[field] = true
			p.addDuplicateAttempts(st, m, c, f, field)
		}
	}
}

func (p *Plan) addDuplicateAttempts(st *chain.Step, m *catalog.Method, c *RPCContract, f Failure, field string) {
	expect, pinned := refusalExpectations(m, f)
	text := strings.Join([]string{f.When, f.Message, c.Summary}, " ")
	if fc := c.Fields[field]; fc != nil {
		text += " " + fc.Note
	}
	leaf := leafName(field)
	ref := "${steps." + st.ID + ".request." + field + "}"
	attempt := func(suffix, description string, value any) *chain.Step {
		body, _ := cloneBody(st.Body).(map[string]any)
		setBodyPath(body, field, value)
		dup := &chain.Step{
			ID:          uniqueStepID(p.Chain, st.ID+"_same_"+leaf+suffix),
			Description: description,
			Call:        st.Call,
			Auth:        st.Auth,
			Body:        body,
			Expect:      append([]chain.Expectation{}, expect...),
		}
		return dup
	}
	added := []*chain.Step{attempt("", fmt.Sprintf("the same %s again is refused with %s.", leaf, f.Label()), ref)}
	current, _ := bodyValue(st.Body, field)
	value, isText := current.(string)
	switch {
	case !ignoresCase(f, text):
		p.note("step %s: %s is refused as %s when it is taken; the plan sends the same value again. If the backend "+
			"ignores case when comparing it, set unique: {case: ignore} on the failure (or say \"ignoring case\" in its when:) and the plan adds "+
			"a case variant too — a backend that compares case-sensitively passes an exact duplicate", st.ID, field, f.Label())
	case !isText || swapLiteralCase(stableAcrossSteps(value)) == stableAcrossSteps(value):
		p.note("step %s: the contract says %s is compared ignoring case, but its value %v has no letters outside "+
			"references, so no case variant could be built: write one by hand", st.ID, field, current)
	default:
		stable := stableAcrossSteps(value)
		if stable != value {
			setBodyPath(st.Body, field, stable)
		}
		added = append(added, attempt("_case", fmt.Sprintf("the same %s in another letter case is the same %s, so it "+
			"is refused with %s too (the contract says it is compared ignoring case).", leaf, leaf, f.Label()),
			swapLiteralCase(stable)))
		note := ""
		if stable != value {
			note = fmt.Sprintf("; %s now reads ${vars.tag} instead of a fresh reference, so both steps send the same "+
				"value, and every run needs -var tag=<fresh>", field)
		}
		p.note("step %s: the contract says %s is unique ignoring case, so the plan sends a case variant "+
			"(step %s) as well as the exact duplicate%s", st.ID, field, added[len(added)-1].ID, note)
	}
	if isText && trimsSpace(f, text) {
		added = append(added, attempt("_space", fmt.Sprintf("the same %s with surrounding whitespace is the same %s "+
			"once trimmed, so it is refused with %s too.", leaf, leaf, f.Label()), " "+ref+" "))
	}
	if !pinned {
		p.note("step %s: the duplicate attempts assert only that the call was refused; the response declares no "+
			"code field (conventions.code_fields) to pin %s on, so pin it yourself where the refusal carries it", st.ID, f.Label())
	}
	at := 0
	for i, s := range p.Chain.Steps {
		if s == st {
			at = i + 1
		}
	}
	steps := append([]*chain.Step{}, p.Chain.Steps[:at]...)
	steps = append(steps, added...)
	p.Chain.Steps = append(steps, p.Chain.Steps[at:]...)
}

func isNumericKind(kind string) bool {
	switch kind {
	case "int32", "int64", "uint32", "uint64", "sint32", "sint64", "fixed32", "fixed64", "sfixed32", "sfixed64":
		return true
	}
	return false
}

func refusalExpectations(m *catalog.Method, f Failure) ([]chain.Expectation, bool) {
	out := []chain.Expectation{}
	fields := catalog.DescribeMessage(m.Output()).Fields
	root := ""
	if CarriesEnvelope(m) {
		out = append(out, chain.Expectation{Path: chain.EnvelopePath(), NotEqual: chain.EnvelopeOK()})
		root = chain.EnvelopeField()
	}
	codes := map[string]bool{}
	for _, name := range chain.CodeFields() {
		codes[name] = true
	}
	pinned := false
	seen := map[string]bool{}
	var walk func(prefix string, fs []*catalog.Field, depth int)
	walk = func(prefix string, fs []*catalog.Field, depth int) {
		for _, fd := range fs {
			path := prefix + fd.Name
			if fd.Kind == "message" {
				if depth < 3 && fd.MapKey == "" {
					next := path + "."
					if fd.Repeated {
						next = path + ".0."
					}
					walk(next, fd.Fields, depth+1)
				}
				continue
			}
			if fd.Repeated || !codes[fd.Name] || seen[fd.Name] {
				continue
			}
			switch {
			case fd.Kind == "string" && f.Reason != "":
				out = append(out, chain.Expectation{Path: path, Equals: f.Reason})
			case isNumericKind(fd.Kind) && f.Code != 0:
				out = append(out, chain.Expectation{Path: path, Equals: f.Code})
			default:
				continue
			}
			seen[fd.Name] = true
			pinned = true
		}
	}
	for _, fd := range fields {
		if root == "" || fd.Name == root {
			walk("", []*catalog.Field{fd}, 0)
		}
	}
	if !pinned && f.ConnectCode != "" && f.Code == 0 {
		out = append(out, chain.Expectation{Path: "transport.code", Equals: f.ConnectCode})
		pinned = true
	}
	carriers := []string{}
	for _, fd := range fields {
		if fd.Kind == "message" && !fd.Repeated && fd.MapKey == "" && fd.Name != root && !IsVerdictFieldName(fd.Name) {
			carriers = append(carriers, fd.Name)
		}
	}
	if len(carriers) == 1 {
		absent := false
		out = append(out, chain.Expectation{Path: carriers[0], Exists: &absent})
	}
	return out, pinned
}
