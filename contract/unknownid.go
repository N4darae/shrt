package contract

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

const unknownIDSuffix = "-unknown"

var (
	notFoundReason = lazyRegexp(`NotFound|NoSuch|DoesNotExist|NotExist|^(?:Unknown|Missing|No)[A-Z]|(?:Unknown|Missing)$`)
	notFoundWhen   = lazyRegexp(`(?i)\bno \w+ (?:has|with|matches|named|by)\b|\bdoes not exist\b|\bnot found\b|\bunknown\b|\bnames (?:no|an? (?:unknown|nonexistent|missing))\b|\bno such\b`)
)

func notFoundFailures(lib *Library, rpc string) []Failure {
	out := []Failure{}
	for _, f := range lib.AllFailures(rpc) {
		if !probeable(f) || f.ConnectCode == "invalid_argument" {
			continue
		}
		if notFoundReason().MatchString(f.Reason) || notFoundWhen().MatchString(f.When) {
			out = append(out, f)
		}
	}
	return out
}

func unknownIDFailure(failures []Failure, field string, ref Ref, only bool) (Failure, bool) {
	leaf := chain.PathLeaf(field)
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
			if noun != "" && (strings.Contains(namecase.Fold(f.Reason), noun) || mentionsField(f.When, noun)) {
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
		results, _, _ := p.perItemResults(lib, st.Call, c, m)
		lookups := []string{}
		for _, name := range chain.SortedKeys(c.Fields) {
			if results != nil && len(chain.SplitPath(name)) > 1 {
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
	probe := p.probeCopy(lib, st, "unknown_"+chain.PathLeaf(field))
	renameStepRefs(probe, st.ID, probe.ID)
	cur, _ := bodyValue(st.Body, path)
	text, isText := cur.(string)
	if !isText {
		return ""
	}
	unknown, wording := "no-such-"+strings.ReplaceAll(chain.PathLeaf(path), "_", "-"), "an id nothing created"
	if wholeReference(text) {
		unknown, wording = text+unknownIDSuffix, fmt.Sprintf("a real id with %q appended", unknownIDSuffix)
	}
	setBodyPath(probe.Body, path, unknown)
	probe.Expect = refusalFor(m, f, false)
	probe.Description = fmt.Sprintf("%s names no existing record (%s), so the answer is the not-found failure %s (%s).",
		path, wording, f.Label(), strings.TrimSpace(f.When))
	p.addShape(lib, st, probe)
	return fmt.Sprintf("%s (%s, expecting %s)", probe.ID, path, f.Label())
}
