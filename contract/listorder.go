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

const orderedItems = 3

var (
	anchorRanks  = []int{0, 2, 1}
	fieldRanks   = [][]int{{1, 0, 2}, {2, 0, 1}, {1, 2, 0}}
	looseRanks   = [][]int{{0, 2, 1}, {1, 0, 2}, {2, 0, 1}, {1, 2, 0}}
	sortedByText = regexp.MustCompile(`(?i)\b(?:sorted|ordered|sorts|orders|sort|order)\s+by\s+(?:its\s+|their\s+|the\s+)?([A-Za-z_][A-Za-z0-9_]*)`)
	newestFirst  = regexp.MustCompile(`(?i)\b(newest|latest|most recent)\s+first\b|\bdescending\b|\bdesc\b`)
	oldestFirst  = regexp.MustCompile(`(?i)\b(?:oldest|earliest|first created)\s+first\b|\b(?:in\s+)?(?:creation|insertion|chronological)\s+order\b|\bchronologically\b|\bin the order (?:they were|it was) created\b`)
	creationWord = regexp.MustCompile(`(?i)^(creation|created|created_at|insertion|inserted|oldest|time)$`)
)

type listTarget struct {
	step      *chain.Step
	listPath  string
	itemMsg   string
	itemID    string
	producers []*chain.Step
	carrier   string
	anchor    string
	unscoped  bool
}

func (p *Plan) discriminateListOrder(lib *Library, grow func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		t := p.listTargetFor(st, grow != nil && grow(st))
		if t == nil {
			continue
		}
		p.scopeUnscopedList(t)
		p.orderFixtures(t, lib)
	}
}

func repeatedMessageField(m *catalog.Method) *catalog.Field {
	for _, f := range catalog.DescribeMessage(m.Output()).Fields {
		if f.Repeated && f.Kind == "message" && f.MapKey == "" && !IsPagingFieldName(f.Name) {
			return f
		}
	}
	return nil
}

func carrierField(m *catalog.Method, msg string) string {
	for _, f := range catalog.DescribeMessage(m.Output()).Fields {
		if !f.Repeated && f.Kind == "message" && f.Message == msg {
			return f.Name
		}
	}
	return ""
}

func (p *Plan) listTargetFor(st *chain.Step, grow bool) *listTarget {
	if st == nil || !chain.IsReadOnlyCall(st.Call) {
		return nil
	}
	m, err := p.cat.Lookup(st.Call)
	if err != nil {
		return nil
	}
	list := repeatedMessageField(m)
	if list == nil {
		return nil
	}
	t := &listTarget{step: st, listPath: list.Name, itemMsg: list.Message}
	for _, f := range list.Fields {
		if IsEntityIDField(f.Name) && f.Kind == "string" {
			t.itemID = f.Name
			break
		}
	}
	var call string
	for _, prod := range p.Chain.Steps {
		if prod == st {
			break
		}
		if chain.IsReadOnlyCall(prod.Call) || isRefusalStep(prod) {
			continue
		}
		pm, err := p.cat.Lookup(prod.Call)
		if err != nil {
			continue
		}
		carrier := carrierField(pm, list.Message)
		if carrier == "" {
			continue
		}
		if call != "" && prod.Call != call {
			continue
		}
		call, t.carrier = prod.Call, carrier
		t.producers = append(t.producers, prod)
	}
	if len(t.producers) == 0 || t.itemID == "" || (!grow && len(t.producers) < 2) {
		return nil
	}
	for key, v := range st.Body {
		text, ok := v.(string)
		if !ok {
			continue
		}
		src, rest, ok := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(text, "${steps."), "}"), ".request.")
		if !ok || !wholeReference(text) || !strings.HasPrefix(text, "${steps.") {
			for _, prod := range t.producers {
				if strings.Contains(text, "${"+prod.ID+".") || strings.Contains(text, "${steps."+prod.ID+".") {
					return nil
				}
			}
			continue
		}
		producer := false
		for _, prod := range t.producers {
			producer = producer || prod.ID == src
		}
		if !producer {
			continue
		}
		if !strings.Contains(strings.ToLower(key), "prefix") {
			return nil
		}
		t.anchor = rest
		st.Body[key] = "${steps." + t.producers[0].ID + ".request." + rest + "}"
	}
	return t
}

func (p *Plan) orderFixtures(t *listTarget, lib *Library) {
	first := t.producers[0]
	for len(t.producers) < orderedItems {
		last := t.producers[len(t.producers)-1]
		id := p.freeStepID(first.ID)
		clone := copyStep(first, id)
		p.insertAfter(last.ID, clone)
		t.producers = append(t.producers, clone)
	}
	t.producers = t.producers[:orderedItems]
	pm, err := p.cat.Lookup(first.Call)
	if err != nil {
		return
	}
	fields := catalog.DescribeMessage(pm.Input()).Fields
	ranks := map[string][]int{}
	if t.anchor != "" {
		base := "${steps." + first.ID + ".request." + t.anchor + "}"
		for k, prod := range t.producers[1:] {
			setBodyPath(prod.Body, t.anchor, base+"-"+string(rune('a'+anchorRanks[k+1]-1)))
		}
		ranks[t.anchor] = anchorRanks
	}
	perms := fieldRanks
	if t.anchor == "" {
		perms = looseRanks
	}
	next := 0
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	for _, name := range names {
		f := fieldByName(fields, name)
		if f == nil || name == t.anchor || f.Repeated || f.MapKey != "" || len(f.EnumValues) > 0 || f.Kind == "bool" || f.Kind == "message" || f.Oneof != "" {
			continue
		}
		key, ok := namecase.LookupKey(first.Body, name)
		if !ok {
			continue
		}
		perm := perms[next%len(perms)]
		original := first.Body[key]
		varied := true
		for k, prod := range t.producers {
			v, ok := orderedValue(original, f.Name, f.Kind, perm[k])
			if !ok {
				varied = false
				break
			}
			prod.Body[key] = v
		}
		if !varied {
			for _, prod := range t.producers {
				prod.Body[key] = original
			}
			continue
		}
		ranks[name] = perm
		next++
	}
	varyItemNumbers(t.producers, fields)
	p.noteOrder(t, ranks, lib)
}

func fieldByName(fields []*catalog.Field, name string) *catalog.Field {
	for _, f := range fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func orderedValue(v any, name, kind string, rank int) (any, bool) {
	marker := string(rune('a' + rank))
	if chain.IsNumericKind(kind) {
		base := int64(0)
		switch t := v.(type) {
		case string:
			n, err := strconv.ParseInt(t, 10, 64)
			if err != nil {
				return v, false
			}
			base = n
		case float64:
			base = int64(t)
		case int:
			base = int64(t)
		default:
			return v, false
		}
		return strconv.FormatInt(spreadValue(base, rank, isQuantityName(name)), 10), true
	}
	text, ok := v.(string)
	if !ok || text == "" || wholeReference(text) {
		return v, false
	}
	if loc := planVarRef.FindStringIndex(text); loc != nil {
		if strings.Contains(text[:loc[0]], "${") {
			return v, false
		}
		return markAfterVar(text, loc, marker), true
	}
	if strings.Contains(text, "${") {
		return v, false
	}
	return text + " " + strings.ToUpper(marker), true
}

func stateOrder(c *RPCContract, listPath string) (string, bool, bool) {
	if c == nil {
		return "", false, false
	}
	texts := []string{c.Summary, c.Note, c.Exports[listPath], c.Terminal[listPath]}
	for _, text := range texts {
		if m := sortedByText.FindStringSubmatch(text); m != nil {
			return m[1], newestFirst.MatchString(text), true
		}
		if newestFirst.MatchString(text) {
			return "creation", true, true
		}
		if oldestFirst.MatchString(text) {
			return "creation", false, true
		}
	}
	return "", false, false
}

func (p *Plan) noteOrder(t *listTarget, ranks map[string][]int, lib *Library) {
	ids := make([]string, 0, len(t.producers))
	for _, prod := range t.producers {
		ids = append(ids, prod.ID)
	}
	if t.unscoped {
		p.noteUnscopedList(t, len(ids))
		return
	}
	keys := make([]string, 0, len(ranks))
	for k := range ranks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	orders := []string{}
	for _, k := range keys {
		orders = append(orders, fmt.Sprintf("%s: %s", k, orderOf(ids, ranks[k])))
	}
	orders = append(orders, "creation: "+strings.Join(ids, ", "))
	listRPC := shortRPC(t.step.Call)
	c, _ := lib.Get(canonicalCall(p.cat, t.step.Call))
	key, desc, stated := stateOrder(c, t.listPath)
	if len(keys) == 0 && !(stated && creationWord.MatchString(key)) {
		p.note("step %s: %s lists what %s create, but their fixtures have no scalar field shrt could vary, so every candidate "+
			"sort key but creation order agrees; give them values that sort differently before asserting an order",
			t.step.ID, listRPC, strings.Join(ids, ", "))
		p.assertMembers(t)
		t.step.Expect = append(t.step.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, len(ids)), Exists: boolPtr(false)})
		return
	}
	if len(keys) > 0 {
		p.note("step %s: %s, %s give %s %d %s whose candidate sort keys disagree (%s), so an order assertion can tell which key "+
			"the backend sorts by: fixtures that sort the same under every key pass a backend sorting by the wrong one",
			t.step.ID, strings.Join(ids[:len(ids)-1], ", "), ids[len(ids)-1], listRPC, len(ids), t.listPath, strings.Join(orders, "; "))
	}
	if !stated {
		p.note("step %s: the contract for %s states no order for %s, so no position is asserted, only that each fixture is "+
			"in it by id (includes:); if it promises one, say so in its summary (\"sorted by <field>\", \"newest first\", "+
			"\"oldest first\") and plan again", t.step.ID, listRPC, t.listPath)
		p.assertMembers(t)
		t.step.Expect = append(t.step.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, len(ids)), Exists: boolPtr(false)})
		return
	}
	var order []int
	switch {
	case creationWord.MatchString(key):
		order = []int{0, 1, 2}
	default:
		for name, r := range ranks {
			if namecase.Fold(name) == namecase.Fold(key) {
				order = inverse(r)
			}
		}
	}
	if order == nil {
		p.note("step %s: the contract says %s is sorted by %q, which is no field shrt varied among %s, so no position is asserted",
			t.step.ID, t.listPath, key, strings.Join(keys, ", "))
		return
	}
	if desc {
		for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
			order[i], order[j] = order[j], order[i]
		}
	}
	for i, k := range order {
		prod := t.producers[k]
		t.step.Expect = append(t.step.Expect, chain.Expectation{
			Path:   fmt.Sprintf("%s.%d.%s", t.listPath, i, t.itemID),
			Equals: "${" + prod.ID + "." + t.carrier + "." + t.itemID + "}",
		})
	}
	t.step.Expect = append(t.step.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, len(order)), Exists: boolPtr(false)})
}

func boolPtr(b bool) *bool { return &b }

func (p *Plan) assertMembers(t *listTarget) {
	for _, prod := range t.producers {
		want := "${" + prod.ID + "." + t.carrier + "." + t.itemID + "}"
		held := false
		for _, e := range t.step.Expect {
			if m, ok := e.Includes.(map[string]any); ok && e.Path == t.listPath && m[t.itemID] == want {
				held = true
			}
		}
		if !held {
			t.step.Expect = append(t.step.Expect, chain.Expectation{Path: t.listPath, Includes: map[string]any{t.itemID: want}})
		}
	}
}

func inverse(ranks []int) []int {
	out := make([]int, len(ranks))
	for k, r := range ranks {
		out[r] = k
	}
	return out
}

func orderOf(ids []string, ranks []int) string {
	ordered := make([]string, len(ids))
	for k, r := range ranks {
		ordered[r] = ids[k]
	}
	return strings.Join(ordered, " < ")
}

func canonicalCall(cat *catalog.Catalog, call string) string {
	if m, err := cat.Lookup(call); err == nil {
		return m.FullName
	}
	return call
}
