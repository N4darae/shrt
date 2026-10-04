package contract

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

const unknownIDSuffix = "-unknown"

var (
	notFoundReason = regexp.MustCompile(`NotFound|NoSuch|DoesNotExist|NotExist|^(?:Unknown|Missing|No)[A-Z]|(?:Unknown|Missing)$`)
	notFoundWhen   = regexp.MustCompile(`(?i)\bno \w+ (?:has|with|matches|named|by)\b|\bdoes not exist\b|\bnot found\b|\bunknown\b|\bnames (?:no|an? (?:unknown|nonexistent|missing))\b|\bno such\b`)
)

func notFoundFailures(lib *Library, rpc string) []Failure {
	out := []Failure{}
	for _, f := range lib.AllFailures(rpc) {
		if !probeable(f) || f.ConnectCode == "invalid_argument" {
			continue
		}
		if notFoundReason.MatchString(f.Reason) || notFoundWhen.MatchString(f.When) {
			out = append(out, f)
		}
	}
	return out
}

func unknownIDFailure(failures []Failure, field string, ref Ref, only bool) (Failure, bool) {
	leaf := leafName(field)
	for _, f := range failures {
		if f.Field != "" && stripIndexes(f.Field) == stripIndexes(field) {
			return f, true
		}
	}
	for _, f := range failures {
		if f.Field == "" && (mentionsField(f.When, field) || mentionsField(f.When, leaf)) {
			return f, true
		}
	}
	nouns := []string{namecase.Fold(chain.SplitPath(ref.Path)[0]),
		namecase.Fold(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(leaf, "id_"), "id"), "_id"))}
	for _, f := range failures {
		if f.Field != "" {
			continue
		}
		for _, noun := range nouns {
			if noun != "" && (strings.Contains(namecase.Fold(f.Reason), noun) || regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(noun)+`\b`).MatchString(f.When)) {
				return f, true
			}
		}
	}
	if only && len(failures) == 1 && failures[0].Field == "" {
		return failures[0], true
	}
	return Failure{}, false
}

func (p *Plan) probeUnknownIDs(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || p.isLogin(st.Call) {
			continue
		}
		c, m, ok := p.contractOf(lib, st.Call)
		if !ok {
			continue
		}
		failures := notFoundFailures(lib, st.Call)
		if len(failures) == 0 {
			continue
		}
		batch := p.perItemResults(lib, st.Call, c, m) != nil
		lookups := []string{}
		for _, name := range sortedKeys(c.Fields) {
			if batch && len(chain.SplitPath(name)) > 1 {
				continue
			}
			if f := c.Fields[name]; f != nil && f.From != "" && f.CheckedBy != CheckedByNone {
				lookups = append(lookups, name)
			}
		}
		said := []string{}
		for _, name := range lookups {
			ref, err := ParseRef(c.Fields[name].From)
			if err != nil {
				continue
			}
			f, found := unknownIDFailure(failures, name, ref, len(lookups) == 1)
			if !found {
				continue
			}
			if probe := p.addUnknownID(lib, st, m, name, f); probe != "" {
				said = append(said, probe)
			}
		}
		if len(said) > 0 {
			p.note("step %s: %s %s an id no record has (a real one with %q appended, so its format still passes) and "+
				"%s exactly the not-found failure the contract declares, a write between reads proving nothing moved: a "+
				"backend that answers another code, another record or an empty success fails", st.ID, strings.Join(said, "; "),
				pluralVerb(len(said), "sends", "send"), unknownIDSuffix, pluralVerb(len(said), "expects", "expect"))
		}
	}
}

func (p *Plan) addUnknownID(lib *Library, st *chain.Step, m *catalog.Method, field string, f Failure) string {
	segs := chain.SplitPath(field)
	path := field
	if len(segs) > 1 {
		key, ok := namecase.LookupKey(st.Body, segs[0])
		list, isList := st.Body[key].([]any)
		if !ok || !isList || len(list) == 0 {
			return ""
		}
		path = fmt.Sprintf("%s.%d.%s", key, len(list)-1, strings.Join(segs[1:], "."))
	} else if key, ok := namecase.LookupKey(st.Body, field); ok {
		path = key
	}
	probe := p.probeCopy(lib, st, "unknown_"+leafName(field))
	renameStepRefs(probe, st.ID, probe.ID)
	return p.unknownAt(lib, st, m, probe, path, f)
}

func (p *Plan) unknownAt(lib *Library, st *chain.Step, m *catalog.Method, probe *chain.Step, path string, f Failure) string {
	cur, ok := bodyValue(st.Body, path)
	if !ok {
		return ""
	}
	text, isText := cur.(string)
	if !isText {
		return ""
	}
	unknown := "no-such-" + strings.ReplaceAll(leafName(path), "_", "-")
	if wholeReference(text) {
		unknown = text + unknownIDSuffix
	}
	setBodyPath(probe.Body, path, unknown)
	probe.Expect = refusalOf(m, f)
	probe.Description = fmt.Sprintf("%s names no existing record (%s), so the answer is the not-found failure %s (%s).",
		path, unknownValueWording(text), f.Label(), strings.TrimSpace(f.When))
	steps := []*chain.Step{probe}
	if !chain.IsReadOnlyCall(st.Call) && len(referencedSteps(probe.Body)) > 0 {
		steps = p.guardUnchanged(lib, steps, probe.ID)
	}
	p.Chain.Steps = append(p.Chain.Steps, steps...)
	return fmt.Sprintf("%s (%s, expecting %s)", probe.ID, path, f.Label())
}

func unknownValueWording(current string) string {
	if wholeReference(current) {
		return fmt.Sprintf("a real id with %q appended", unknownIDSuffix)
	}
	return "an id nothing created"
}
