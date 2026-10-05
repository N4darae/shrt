package contract

import (
	"fmt"
	"iter"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

const orderedItems = 3

var (
	sortedByText = lazyRegexp(`(?i)\b(?:sorted|ordered|sorts|orders|sort|order)\s+by\s+(?:its\s+|their\s+|the\s+)?([A-Za-z_][A-Za-z0-9_]*)`)
	newestFirst  = lazyRegexp(`(?i)\b(newest|latest|most recent)\s+first\b|\bdescending\b|\bdesc\b`)
	oldestFirst  = lazyRegexp(`(?i)\b(?:oldest|earliest|first created)\s+first\b|\b(?:in\s+)?(?:creation|insertion|chronological)\s+order\b|\bchronologically\b|\bin the order (?:they were|it was) created\b`)
	creationWord = lazyRegexp(`(?i)^(creation|created|created_at|insertion|inserted|oldest|time)$`)
)

type listTarget struct {
	step      *chain.Step
	listPath  string
	itemMsg   string
	itemID    string
	producers []*chain.Step
	extra     []*chain.Step
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
		if !slices.ContainsFunc(t.producers, func(prod *chain.Step) bool { return prod.ID == src }) {
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
	var fields []*catalog.Field
	pm, err := p.cat.Lookup(first.Call)
	if err == nil {
		fields = catalog.DescribeMessage(pm.Input()).Fields
	}
	names := orderableFields(first, fields, t.anchor)
	keys := len(names)
	lists := itemNumbers([]*chain.Step{first}, fields, nil)
	if len(lists) > 0 {
		keys++
	}
	if t.anchor != "" {
		keys++
	}
	items := orderedItems
	if keys > len(orderRanks(items)) {
		items++
	}
	for len(t.producers) < items {
		last := t.producers[len(t.producers)-1]
		id := p.freeStepID(first.ID)
		clone := copyStep(first, id)
		p.insertAfter(last.ID, clone)
		t.producers = append(t.producers, clone)
	}
	if t.anchor == "" {
		t.extra = append([]*chain.Step{}, t.producers[items:]...)
	}
	t.producers = t.producers[:items]
	if err != nil {
		return
	}
	perms := orderRanks(items)
	ranks := map[string][]int{}
	next := 0
	if t.anchor != "" {
		if seed, ok := first.Body[t.anchor].(string); ok && seed != "" && runPrefix(seed) == seed {
			first.Body[t.anchor] = seed + "-a"
		}
		base := "${steps." + first.ID + ".request." + t.anchor + "}"
		for k, prod := range t.producers[1:] {
			setBodyPath(prod.Body, t.anchor, base+"-"+string(rune('a'+perms[0][k+1]-1)))
		}
		ranks[t.anchor] = perms[0]
		next++
	}
	for _, name := range names {
		f := fieldByName(fields, name)
		key, _ := namecase.LookupKey(first.Body, name)
		perm := perms[next%len(perms)]
		original := first.Body[key]
		for k, prod := range t.producers {
			prod.Body[key], _ = orderedValue(original, f.Name, f.Kind, perm[k])
		}
		ranks[name] = perm
		next++
	}
	if len(lists) > 0 {
		ranks[strings.Join(lists, ", ")] = varyItemNumbers(t.producers, fields, perms[next%len(perms)])
	}
	p.noteOrder(t, ranks, lib)
}

func orderableFields(first *chain.Step, fields []*catalog.Field, anchor string) []string {
	out := []string{}
	for _, f := range fields {
		if f.Name == anchor || f.Repeated || f.MapKey != "" || len(f.EnumValues) > 0 || f.Kind == "bool" || f.Kind == "message" || f.Oneof != "" {
			continue
		}
		key, ok := namecase.LookupKey(first.Body, f.Name)
		if !ok {
			continue
		}
		if _, ok := orderedValue(first.Body[key], f.Name, f.Kind, 0); ok {
			out = append(out, f.Name)
		}
	}
	sort.Strings(out)
	return out
}

func orderRanks(n int) [][]int {
	used := map[string]bool{}
	class := func(order []int) {
		back := slices.Clone(order)
		slices.Reverse(back)
		used[fmt.Sprint(order)], used[fmt.Sprint(back)] = true, true
	}
	identity := make([]int, n)
	for i := range identity {
		identity[i] = i
	}
	class(identity)
	out := [][]int{}
	var walk func(prefix []int, left []int)
	walk = func(prefix, left []int) {
		if len(left) == 0 {
			if !used[fmt.Sprint(prefix)] {
				class(prefix)
				out = append(out, inverse(prefix))
			}
			return
		}
		for i, v := range left {
			rest := append(append([]int{}, left[:i]...), left[i+1:]...)
			walk(append(append([]int{}, prefix...), v), rest)
		}
	}
	walk(nil, identity)
	return out
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
	if loc := planVarRef().FindStringIndex(text); loc != nil {
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
		if m := sortedByText().FindStringSubmatch(text); m != nil {
			return m[1], newestFirst().MatchString(text), true
		}
		if newestFirst().MatchString(text) {
			return "creation", true, true
		}
		if oldestFirst().MatchString(text) {
			return "creation", false, true
		}
	}
	return "", false, false
}

func (p *Plan) noteOrder(t *listTarget, ranks map[string][]int, lib *Library) {
	ids := stepIDs(t.producers)
	members := append(append([]*chain.Step{}, t.producers...), t.extra...)
	if t.unscoped {
		p.noteUnscopedList(t, len(members))
		return
	}
	keys := chain.SortedKeys(ranks)
	orders := []string{}
	for _, k := range keys {
		ordered := make([]string, len(ids))
		for i, r := range ranks[k] {
			ordered[r] = ids[i]
		}
		orders = append(orders, k+": "+strings.Join(ordered, " < "))
	}
	orders = append(orders, "creation: "+strings.Join(ids, ", "))
	listRPC := shortRPC(t.step.Call)
	c, _ := lib.Get(canonicalCall(p.cat, t.step.Call))
	key, desc, stated := stateOrder(c, t.listPath)
	if len(keys) == 0 && !(stated && creationWord().MatchString(key)) {
		p.note("step %s: %s lists what %s create, but their fixtures have no scalar field shrt could vary, so every candidate "+
			"sort key but creation order agrees; give them values that sort differently before asserting an order",
			t.step.ID, listRPC, strings.Join(ids, ", "))
		p.assertMembers(t)
		t.step.Expect = append(t.step.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, len(members)), Exists: boolPtr(false)})
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
		t.step.Expect = append(t.step.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, len(members)), Exists: boolPtr(false)})
		return
	}
	if len(t.extra) > 0 {
		p.orderAllMembers(t, members, key, desc)
		return
	}
	var order []int
	switch {
	case creationWord().MatchString(key):
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
	t.assertPositions(t.producers, order, desc)
}

func (t *listTarget) assertPositions(members []*chain.Step, order []int, desc bool) {
	if desc {
		slices.Reverse(order)
	}
	for i, k := range order {
		t.step.Expect = append(t.step.Expect, chain.Expectation{
			Path:   fmt.Sprintf("%s.%d.%s", t.listPath, i, t.itemID),
			Equals: "${" + members[k].ID + "." + t.carrier + "." + t.itemID + "}",
		})
	}
	t.step.Expect = append(t.step.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, len(order)), Exists: boolPtr(false)})
}

func boolPtr(b bool) *bool { return &b }

func (p *Plan) orderAllMembers(t *listTarget, members []*chain.Step, key string, desc bool) {
	ids := stepIDs(members)
	order, why := memberOrder(members, key)
	if order == nil {
		p.note("step %s: %d steps create what %s lists (%s), and the %d beyond the %d shrt varied for the order %s, "+
			"so no position is asserted, only that each fixture is in it by id (includes:) and that it holds exactly %d; "+
			"give them values that sort apart under %s, or drop the extra creates, to have the order asserted",
			t.step.ID, len(members), shortRPC(t.step.Call), strings.Join(ids, ", "), len(t.extra), len(t.producers), why, len(members), key)
		p.assertMembers(t)
		t.step.Expect = append(t.step.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, len(members)), Exists: boolPtr(false)})
		return
	}
	t.assertPositions(members, order, desc)
}

func memberOrder(members []*chain.Step, key string) ([]int, string) {
	order := make([]int, len(members))
	for i := range order {
		order[i] = i
	}
	if creationWord().MatchString(key) {
		return order, ""
	}
	values := make([]any, len(members))
	for i, prod := range members {
		k, ok := namecase.LookupKey(prod.Body, key)
		if !ok {
			return nil, fmt.Sprintf("do not all send %s", key)
		}
		values[i] = prod.Body[k]
	}
	nums := make([]float64, len(values))
	numeric := true
	for i, v := range values {
		n, err := strconv.ParseFloat(fmt.Sprint(v), 64)
		if err != nil {
			numeric = false
			break
		}
		nums[i] = n
	}
	texts := make([]string, len(values))
	if !numeric {
		for i, v := range values {
			text, ok := v.(string)
			if !ok {
				return nil, fmt.Sprintf("send a %s that is neither a number nor text", key)
			}
			texts[i] = text
		}
		common := texts[0]
		for _, text := range texts[1:] {
			for !strings.HasPrefix(text, common) {
				common = common[:len(common)-1]
			}
		}
		if open := strings.LastIndex(common, "${"); open > strings.LastIndex(common, "}") {
			common = common[:open]
		}
		for i := range texts {
			texts[i] = texts[i][len(common):]
			if strings.Contains(texts[i], "${") {
				return nil, fmt.Sprintf("send %s values whose order depends on what their references resolve to", key)
			}
		}
	}
	less := func(a, b int) bool {
		if numeric {
			return nums[a] < nums[b]
		}
		return texts[a] < texts[b]
	}
	sort.SliceStable(order, func(i, j int) bool { return less(order[i], order[j]) })
	for i := 1; i < len(order); i++ {
		if !less(order[i-1], order[i]) {
			return nil, fmt.Sprintf("send %s values that tie (%s and %s), which the backend may list either way", key, members[order[i-1]].ID, members[order[i]].ID)
		}
	}
	return order, ""
}

func (p *Plan) assertMembers(t *listTarget) {
	for _, prod := range append(append([]*chain.Step{}, t.producers...), t.extra...) {
		want := "${" + prod.ID + "." + t.carrier + "." + t.itemID + "}"
		if !slices.ContainsFunc(t.step.Expect, func(e chain.Expectation) bool {
			m, ok := e.Includes.(map[string]any)
			return ok && e.Path == t.listPath && m[t.itemID] == want
		}) {
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

func (p *Plan) contractOf(lib *Library, call string) (*RPCContract, *catalog.Method, bool) {
	c, ok := lib.Get(call)
	m, err := p.cat.Lookup(call)
	return c, m, ok && err == nil
}

func (p *Plan) targets(isTarget func(*chain.Step) bool, skips ...func(*chain.Step) bool) iter.Seq[*chain.Step] {
	return func(yield func(*chain.Step) bool) {
		for _, st := range slices.Clone(p.Chain.Steps) {
			if isTarget(st) && !slices.ContainsFunc(skips, func(skip func(*chain.Step) bool) bool { return skip(st) }) && !yield(st) {
				return
			}
		}
	}
}

func (p *Plan) contracted(lib *Library, isTarget func(*chain.Step) bool, skips ...func(*chain.Step) bool) iter.Seq[*chain.Step] {
	return func(yield func(*chain.Step) bool) {
		for st := range p.targets(isTarget, skips...) {
			if _, _, ok := p.contractOf(lib, st.Call); ok && !yield(st) {
				return
			}
		}
	}
}

func (p *Plan) withMethods(steps iter.Seq[*chain.Step]) iter.Seq2[*chain.Step, *catalog.Method] {
	return func(yield func(*chain.Step, *catalog.Method) bool) {
		for st := range steps {
			if m, err := p.cat.Lookup(st.Call); err == nil && !yield(st, m) {
				return
			}
		}
	}
}

func (p *Plan) called(skip func(*chain.Step) bool) iter.Seq2[*chain.Step, *catalog.Method] {
	return p.withMethods(func(yield func(*chain.Step) bool) {
		for _, st := range p.Chain.Steps {
			if !skip(st) && !yield(st) {
				return
			}
		}
	})
}

func isRead(st *chain.Step) bool { return chain.IsReadOnlyCall(st.Call) }

func isWrite(st *chain.Step) bool { return !chain.IsReadOnlyCall(st.Call) }

func skipsAuth(st *chain.Step) bool { return st.SkipAuth }

func unsuccessful(st *chain.Step) bool { return effectOutcome(st) != outcomeSuccess }

func (p *Plan) loginStep(st *chain.Step) bool { return p.isLogin(st.Call) }

func canonicalCall(cat *catalog.Catalog, call string) string {
	if m, err := cat.Lookup(call); err == nil {
		return m.FullName
	}
	return call
}
