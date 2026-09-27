package contract

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var statedLimit = regexp.MustCompile(`(?i)\b(?:at most|up to|no more than|maximum of|limited to|first|page (?:size )?of)\s+(\d+)\b|\b(\d+)\s+(?:per page|at a time)\b`)

const largestPlannedLimit = 50

func listLimit(c *RPCContract, listPath string) (int, bool) {
	if c == nil {
		return 0, false
	}
	for _, text := range []string{c.Summary, c.Note, c.Exports[listPath]} {
		if m := statedLimit.FindStringSubmatch(text); m != nil {
			n, err := strconv.Atoi(m[1] + m[2])
			return n, err == nil && n > 0
		}
	}
	return 0, false
}

func sizesPage(fields []*catalog.Field) bool {
	for _, f := range fields {
		words := strings.ToLower(strings.Join(namecase.Words(f.Name), " "))
		if IsPagingFieldName(f.Name) || strings.Contains(words, "limit") || strings.Contains(words, "page") || strings.HasPrefix(words, "max") {
			return true
		}
	}
	return false
}

func (p *Plan) probeListCaps(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || !chain.IsReadOnlyCall(st.Call) || effectOutcome(st) != outcomeSuccess {
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		list := repeatedMessageField(m)
		if list == nil {
			continue
		}
		members, itemID := listedMembers(st, list.Name)
		if len(members) == 0 {
			continue
		}
		first := p.stepByID(members[0][0])
		if first == nil {
			continue
		}
		c, _ := lib.Get(canonicalCall(p.cat, st.Call))
		limit, stated := listLimit(c, list.Name)
		if !stated && sizesPage(catalog.DescribeMessage(m.Input()).Fields) {
			continue
		}
		want := lotsOfItems
		if stated {
			want = limit + 1
		}
		if want > largestPlannedLimit || len(members) >= want {
			continue
		}
		p.addListCap(st, list.Name, itemID, first, members, want, stated)
	}
}

func listedMembers(st *chain.Step, listPath string) ([][2]string, string) {
	out := [][2]string{}
	itemID := ""
	seen := map[string]bool{}
	add := func(k string, v any) {
		text, _ := v.(string)
		if src, isRef := refSource(text); isRef && !seen[src] && IsEntityIDField(k) && strings.HasSuffix(text, "."+k+"}") && !strings.HasPrefix(text, "${steps.") {
			seen[src] = true
			out = append(out, [2]string{src, text})
			itemID = k
		}
	}
	for _, e := range st.Expect {
		if inc, ok := e.Includes.(map[string]any); ok && e.Path == listPath && len(inc) == 1 {
			for k, v := range inc {
				add(k, v)
			}
			continue
		}
		index, field, nested := strings.Cut(strings.TrimPrefix(e.Path, listPath+"."), ".")
		if nested && e.Equals != nil && isIndexSegment(index) && strings.HasPrefix(e.Path, listPath+".") && !strings.Contains(field, ".") {
			add(field, e.Equals)
		}
	}
	return out, itemID
}

func (p *Plan) addListCap(st *chain.Step, listPath, itemID string, first *chain.Step, members [][2]string, want int, stated bool) {
	var fields []*catalog.Field
	if pm, err := p.cat.Lookup(first.Call); err == nil {
		fields = catalog.DescribeMessage(pm.Input()).Fields
	}
	carrierRef := strings.TrimPrefix(members[0][1], "${"+first.ID)
	prefixes := p.listPrefixes(st, first)
	refs := []string{}
	for _, mb := range members {
		refs = append(refs, mb[1])
	}
	made := []string{}
	for n := len(members) + 1; n <= want; n++ {
		id := p.freeStepID(first.ID)
		clone := copyStep(first, id)
		clone.Export = nil
		suffix := strings.TrimPrefix(id, first.ID+"_")
		distinctProducerAt(clone.Body, fields, suffix, n-1)
		for key, prefix := range prefixes {
			if v, _ := clone.Body[key].(string); !strings.HasPrefix(v, prefix) {
				clone.Body[key] = fmt.Sprint(first.Body[key]) + "-" + suffix
			}
		}
		renameStepRefs(clone, first.ID, id)
		clone.Description = fmt.Sprintf("another %s with its own values, fixture %d of %d for %s.", shortRPC(first.Call), n, want, st.ID)
		p.Chain.Steps = append(p.Chain.Steps, clone)
		made = append(made, id)
		refs = append(refs, "${"+id+carrierRef)
	}
	v := copyStep(st, p.freeStepID(fmt.Sprintf("%s_%d_%s", st.ID, want, listPath)))
	v.Export = nil
	exact := hasExistsFalse(st, listPath)
	kept := v.Expect[:0]
	for _, e := range v.Expect {
		if e.Path != listPath && !strings.HasPrefix(e.Path, listPath+".") {
			kept = append(kept, e)
		}
	}
	v.Expect = kept
	listed := want
	if stated {
		listed = want - 1
		exact = true
	} else {
		for _, ref := range refs {
			v.Expect = append(v.Expect, chain.Expectation{Path: listPath, Includes: map[string]any{itemID: ref}})
		}
	}
	v.Expect = append(v.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", listPath, listed-1), Exists: boolPtr(true)})
	if exact {
		v.Expect = append(v.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", listPath, listed), Exists: boolPtr(false)})
	}
	v.Description = fmt.Sprintf("as %s, after %s bring its fixtures to %d, so a cap on the number of items listed shows.", st.ID, rangeOf(made), want)
	p.Chain.Steps = append(p.Chain.Steps, v)
	if stated {
		p.note("step %s: its contract states a limit of %d, so %s lists after %s bring the fixtures to %d and asserts exactly %d",
			st.ID, listed, v.ID, rangeOf(made), want, listed)
		return
	}
	p.note("step %s: its contract states no limit, so %s lists after %s bring the fixtures to %d and asserts each is listed: "+
		"a backend that caps the list (at 10, say) fails; state one (\"at most N\") to have it planned against", st.ID, v.ID, rangeOf(made), want)
}

func (p *Plan) listPrefixes(st, first *chain.Step) map[string]string {
	out := map[string]string{}
	keys := make([]string, 0, len(st.Body))
	for k := range st.Body {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		text, ok := st.Body[key].(string)
		if !ok || text == "" || !strings.Contains(namecase.Fold(key), "prefix") {
			continue
		}
		target := prefixTargetKey(key, first)
		if target == "" {
			continue
		}
		if src, isRef := refSource(text); isRef {
			if prod := p.stepByID(src); prod != nil {
				_, field, _ := strings.Cut(text, ".request.")
				text = fmt.Sprint(prod.Body[strings.TrimSuffix(field, "}")])
			}
		}
		out[target] = text
	}
	return out
}

func rangeOf(ids []string) string {
	if len(ids) == 1 {
		return ids[0]
	}
	return ids[0] + ".." + ids[len(ids)-1]
}
