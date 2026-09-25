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
	notFoundReason = regexp.MustCompile(`^(?:Unknown|Missing|No)[A-Z]|(?:NotFound|Unknown|Missing|DoesNotExist|NotExist)$`)
	notFoundWhen   = regexp.MustCompile(`(?i)\bno \w+ (?:has|with|matches|by) (?:this|that|the|its|id_\w+|\w+_id)\b|\bdoes not exist\b|\bnot found\b|\bunknown (?:id|\w+)\b|\bnames an? (?:unknown|nonexistent|missing)\b`)
)

func notFoundFailure(lib *Library, rpc, field string, only bool) (Failure, bool) {
	candidates := []Failure{}
	for _, f := range lib.AllFailures(rpc) {
		if isUnauthenticated(f) {
			continue
		}
		if notFoundReason.MatchString(f.Reason) || notFoundWhen.MatchString(f.When) {
			candidates = append(candidates, f)
		}
	}
	noun := namecase.Fold(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(field, "id_"), "id"), "_id"))
	for _, f := range candidates {
		if f.Field == field || mentionsField(f.When, field) {
			return f, true
		}
	}
	if noun != "" {
		for _, f := range candidates {
			if strings.Contains(namecase.Fold(f.Reason), noun) || strings.Contains(namecase.Fold(f.When), noun) {
				return f, true
			}
		}
	}
	if only && len(candidates) == 1 {
		return candidates[0], true
	}
	return Failure{}, false
}

func (p *Plan) probeUnknownIDs(lib *Library, isTarget func(*chain.Step) bool) {
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
		ids := []string{}
		for _, f := range catalog.DescribeMessage(m.Input()).Fields {
			if f.Kind != "string" || f.Repeated || !IsEntityIDField(f.Name) {
				continue
			}
			fc := c.Fields[f.Name]
			if fc == nil || fc.From == "" || fc.CheckedBy == CheckedByNone {
				continue
			}
			if key, ok := namecase.LookupKey(st.Body, f.Name); ok {
				if v, isText := st.Body[key].(string); isText && wholeReference(v) {
					ids = append(ids, key)
				}
			}
		}
		said := []string{}
		for _, key := range ids {
			failure, ok := notFoundFailure(lib, st.Call, key, len(ids) == 1)
			if !ok {
				continue
			}
			probe := p.probeCopy(lib, st, "unknown_"+key)
			renameStepRefs(probe, st.ID, probe.ID)
			probe.Body[key] = st.Body[key].(string) + unknownIDSuffix
			probe.Expect = refusalFor(m, failure)
			probe.Description = fmt.Sprintf("%s names no existing record (a real id with %q appended), so the answer is the not-found failure %s.",
				key, unknownIDSuffix, failure.Label())
			p.Chain.Steps = append(p.Chain.Steps, probe)
			said = append(said, fmt.Sprintf("%s (%s, expecting %s)", probe.ID, key, failure.Label()))
		}
		if len(said) > 0 {
			p.note("step %s: %s %s an id no record has, the real one with %q appended so its format still passes, "+
				"expecting the not-found failure the contract declares: a backend that answers another record, an empty "+
				"success or an unnamed error fails", st.ID, strings.Join(said, "; "), pluralVerb(len(said), "sends", "send"), unknownIDSuffix)
		}
	}
}
