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

var (
	prefixCaseSensitive = regexp.MustCompile(`(?i)case[- ]sensitive|exactly as sent|exact case`)
	toState             = regexp.MustCompile(`(?i)\b(?:to|status|now|becomes|is)\s+([A-Z][A-Z_]+)\b`)
)

type listScope struct {
	parent    *chain.Step
	parentKey string
	prefixKey string
	prefix    string
	target    string
}

func (p *Plan) probeListFilters(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) {
			continue
		}
		t := p.listTargetFor(st, true)
		if t == nil || len(t.producers) == 0 {
			continue
		}
		scope := p.scopeOf(t)
		if scope.parent == nil && scope.prefixKey == "" {
			continue
		}
		said := []string{}
		if scope.parent != nil {
			said = append(said, p.otherParent(lib, t, scope)...)
		}
		if scope.prefixKey != "" && scope.target != "" {
			p.terminatePrefix(t, &scope)
			said = append(said, p.prefixExclusions(lib, t, scope)...)
		}
		if scope.prefixKey != "" {
			p.probeEmptyFilter(lib, t, scope.prefixKey)
		}
		if !hasExistsFalse(st, t.listPath) {
			st.Expect = append(st.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, len(t.producers)), Exists: boolPtr(false)})
		}
		assertLowerBound(st, t.listPath)
		if n, ok := assertedLength(st, t.listPath); ok && len(said) > 0 {
			p.note("step %s: %s must not appear in %s, which asserts it holds exactly %d item(s): a filter that lets them through fails",
				st.ID, strings.Join(said, " and "), st.ID, n)
		}
		p.filterByState(lib, t)
	}
}

func assertedLength(st *chain.Step, listPath string) (int, bool) {
	for _, e := range st.Expect {
		if e.Exists == nil || *e.Exists || !strings.HasPrefix(e.Path, listPath+".") {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimPrefix(e.Path, listPath+".")); err == nil {
			return n, true
		}
	}
	return 0, false
}

func hasExistsFalse(st *chain.Step, listPath string) bool {
	for _, e := range st.Expect {
		if e.Exists != nil && !*e.Exists && strings.HasPrefix(e.Path, listPath+".") && isIndexSegment(strings.TrimPrefix(e.Path, listPath+".")) {
			return true
		}
	}
	return false
}

func (p *Plan) scopeOf(t *listTarget) listScope {
	scope := listScope{}
	for _, key := range sortedKeys(t.step.Body) {
		text, ok := t.step.Body[key].(string)
		if !ok || text == "" {
			continue
		}
		if strings.Contains(namecase.Fold(key), "prefix") {
			scope.prefixKey, scope.prefix = key, text
			scope.target = t.anchor
			if shared := p.sharedAnchorPrefix(t); shared != "" {
				scope.prefix = shared
				t.step.Body[key] = shared
				p.note("step %s: %s is %q, the start every fixture's %s shares, not %s's whole %s, so the prefix probes "+
					"below have letters to vary", t.step.ID, key, shared, t.anchor, t.producers[0].ID, t.anchor)
			}
			if scope.target == "" {
				want := namecase.Fold(strings.TrimSuffix(strings.TrimPrefix(strings.ReplaceAll(strings.ToLower(key), "prefix", ""), "_"), "_"))
				for k := range t.producers[0].Body {
					if namecase.Fold(k) == want {
						scope.target = k
					}
				}
			}
			continue
		}
		src, isRef := refSource(text)
		if !isRef {
			continue
		}
		parent := p.stepByID(src)
		if parent == nil || chain.IsReadOnlyCall(parent.Call) || containsStep(t.producers, parent) {
			continue
		}
		all := true
		for _, prod := range t.producers {
			if !containsString(referencedSteps(prod.Body), src) {
				all = false
			}
		}
		if all {
			scope.parent, scope.parentKey = parent, key
		}
	}
	return scope
}

func (p *Plan) anchorValue(t *listTarget, prod *chain.Step) string {
	v, _ := prod.Body[t.anchor].(string)
	base := "${steps." + t.producers[0].ID + ".request." + t.anchor + "}"
	if seed, ok := t.producers[0].Body[t.anchor].(string); ok && prod != t.producers[0] && strings.HasPrefix(v, base) {
		return seed + strings.TrimPrefix(v, base)
	}
	return v
}

func (p *Plan) sharedAnchorPrefix(t *listTarget) string {
	if t.anchor == "" {
		return ""
	}
	prefix := runPrefix(p.anchorValue(t, t.producers[0]))
	if prefix == "" {
		return ""
	}
	for _, prod := range t.producers {
		if !strings.HasPrefix(p.anchorValue(t, prod), prefix) {
			return ""
		}
	}
	return prefix
}

func containsStep(list []*chain.Step, st *chain.Step) bool {
	for _, s := range list {
		if s == st {
			return true
		}
	}
	return false
}

func (p *Plan) insertBefore(id string, steps ...*chain.Step) {
	for i, x := range p.Chain.Steps {
		if x.ID == id {
			out := append([]*chain.Step{}, p.Chain.Steps[:i]...)
			out = append(out, steps...)
			p.Chain.Steps = append(out, p.Chain.Steps[i:]...)
			return
		}
	}
	p.Chain.Steps = append(p.Chain.Steps, steps...)
}

func (p *Plan) otherParent(lib *Library, t *listTarget, scope listScope) []string {
	noun := strings.TrimPrefix(scope.parent.ID, "create_")
	parent := copyStep(scope.parent, p.freeStepID(scope.parent.ID+"_other"))
	parent.Export = nil
	parent.Description = fmt.Sprintf("another %s, whose items %s must not list.", noun, t.step.ID)
	p.freshen(lib, parent)
	renameStepRefs(parent, scope.parent.ID, parent.ID)
	item := copyStep(t.producers[0], p.freeStepID(t.producers[0].ID+"_other_"+noun))
	item.Export = nil
	item.Description = fmt.Sprintf("an item of %s, not of %s, so %s must not list it.", parent.ID, scope.parent.ID, t.step.ID)
	p.freshen(lib, item)
	renameStepRefs(item, scope.parent.ID, parent.ID)
	renameStepRefs(item, t.producers[0].ID, item.ID)
	p.insertBefore(t.step.ID, parent, item)
	return []string{fmt.Sprintf("%s, which belongs to %s (another %s)", item.ID, parent.ID, noun)}
}

var endsInVar = regexp.MustCompile(`\$\{\s*vars\.[^}]+\}$`)

func (p *Plan) terminatePrefix(t *listTarget, scope *listScope) {
	if !endsInVar.MatchString(scope.prefix) {
		return
	}
	next := byte(0)
	for _, prod := range t.producers {
		v, _ := prod.Body[scope.target].(string)
		if scope.target == t.anchor {
			v = p.anchorValue(t, prod)
		}
		if !strings.HasPrefix(v, scope.prefix) || len(v) == len(scope.prefix) {
			p.note("step %s: %s %q ends in a var with nothing after it, so a run whose tag is a prefix of another run's "+
				"(cp-1, cp-10) also lists that run's items; %s's %s %q carries no terminator after the prefix to end it with",
				t.step.ID, scope.prefixKey, scope.prefix, prod.ID, scope.target, v)
			return
		}
		c := v[len(scope.prefix)]
		if next != 0 && c != next || strings.IndexByte(prefixTerminators, c) < 0 {
			return
		}
		next = c
	}
	if next == 0 {
		return
	}
	was := scope.prefix
	scope.prefix += string(next)
	t.step.Body[scope.prefixKey] = scope.prefix
	p.note("step %s: %s is %q, not %q: the terminator %q after the var keeps a run whose tag is a prefix of another run's "+
		"(cp-1, cp-10) from listing that run's items too, and every fixture's %s carries it", t.step.ID, scope.prefixKey,
		scope.prefix, was, string(next), scope.target)
}

func (p *Plan) prefixExclusions(lib *Library, t *listTarget, scope listScope) []string {
	first := t.producers[0]
	said := []string{}
	inside := copyStep(first, p.freeStepID(first.ID+"_prefix_inside"))
	inside.Export = nil
	renameStepRefs(inside, first.ID, inside.ID)
	inside.Body[scope.target] = "x-" + scope.prefix
	inside.Description = fmt.Sprintf("its %s contains the %s %q but does not start with it, so %s must not list it.", scope.target, scope.prefixKey, scope.prefix, t.step.ID)
	added := []*chain.Step{inside}
	said = append(said, fmt.Sprintf("%s (%s contains the prefix, not at the start)", inside.ID, scope.target))
	text := ""
	if c, ok := lib.Get(canonicalCall(p.cat, t.step.Call)); ok {
		if fc := c.Fields[scope.prefixKey]; fc != nil {
			text += fc.Note + " "
		}
		text += c.Summary + " "
	}
	if c, ok := lib.Get(canonicalCall(p.cat, first.Call)); ok {
		if fc := c.Fields[scope.target]; fc != nil {
			text += fc.Note
		}
	}
	swapped := swapLiteralCase(scope.prefix)
	switch {
	case caseIgnored.MatchString(text):
		p.gap("step %s: the contracts do not say %s is compared case-sensitively, so no fixture with the prefix in "+
			"another letter case was planned; say \"case-sensitive\" in the note of %s or %s to have one", t.step.ID, scope.target, scope.prefixKey, scope.target)
	case swapped == scope.prefix:
		p.note("step %s: the prefix %q has no letters outside references, so no case variant could be built", t.step.ID, scope.prefix)
	case !prefixCaseSensitive.MatchString(text):
		p.listInOtherCase(t, scope, swapped)
	default:
		cased := copyStep(first, p.freeStepID(first.ID+"_prefix_case"))
		cased.Export = nil
		renameStepRefs(cased, first.ID, cased.ID)
		cased.Body[scope.target] = strings.TrimRight(swapped, "-_./:#|~") + "-case"
		cased.Description = fmt.Sprintf("its %s starts with the %s in another letter case; the comparison is case-sensitive, so %s must not list it.", scope.target, scope.prefixKey, t.step.ID)
		added = append(added, cased)
		said = append(said, fmt.Sprintf("%s (the prefix in another case)", cased.ID))
	}
	p.insertBefore(t.step.ID, added...)
	return said
}

func (p *Plan) listInOtherCase(t *listTarget, scope listScope, swapped string) {
	m, err := p.cat.Lookup(t.step.Call)
	if err != nil {
		return
	}
	n, ok := assertedLength(t.step, t.listPath)
	if !ok {
		n = len(t.producers)
	}
	probe := copyStep(t.step, p.freeStepID(t.step.ID+"_prefix_case"))
	probe.Export = nil
	probe.Body[scope.prefixKey] = swapped
	probe.Expect = append(SuccessExpectation(m), chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, n), Exists: boolPtr(false)})
	probe.Description = fmt.Sprintf("%s with the %s in another letter case: the contract does not say whether case counts, "+
		"so which items it lists is left to the safe spot, and verify reports a change.", t.step.ID, scope.prefixKey)
	p.insertAfter(t.step.ID, probe)
	p.gap("step %s: %s's case rule is unstated, so %s can assert only that it lists at most %s's items: say "+
		"\"case-sensitive\" or \"case-insensitive\" in %s's note", t.step.ID, scope.target, probe.ID, t.step.ID, scope.prefixKey)
}

type transition struct {
	method   *catalog.Method
	contract *RPCContract
	field    string
	value    string
}

func enumShort(values []string) map[string]string {
	out := map[string]string{}
	prefix := ""
	if len(values) > 1 {
		prefix = values[0]
		for _, v := range values[1:] {
			for !strings.HasPrefix(v, prefix) {
				prefix = prefix[:len(prefix)-1]
			}
		}
		if i := strings.LastIndex(prefix, "_"); i >= 0 {
			prefix = prefix[:i+1]
		} else {
			prefix = ""
		}
	}
	for _, v := range values {
		out[v] = strings.TrimPrefix(v, prefix)
	}
	return out
}

func stateIn(texts []string, values []string, short map[string]string) string {
	for _, text := range texts {
		if text == "" {
			continue
		}
		for _, m := range toState.FindAllStringSubmatch(text, -1) {
			for _, v := range values {
				if strings.EqualFold(m[1], short[v]) || m[1] == v {
					return v
				}
			}
		}
		last, at := "", -1
		for _, v := range values {
			re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(short[v]) + `\b`)
			if loc := re.FindAllStringIndex(text, -1); len(loc) > 0 && loc[len(loc)-1][0] > at {
				last, at = v, loc[len(loc)-1][0]
			}
		}
		if last != "" {
			return last
		}
	}
	return ""
}

func (p *Plan) filterByState(lib *Library, t *listTarget) {
	lm, err := p.cat.Lookup(t.step.Call)
	if err != nil {
		return
	}
	list := repeatedMessageField(lm)
	if list == nil {
		return
	}
	var filter, stateField *catalog.Field
	for _, rf := range catalog.DescribeMessage(lm.Input()).Fields {
		if len(rf.EnumValues) == 0 || rf.Repeated {
			continue
		}
		for _, item := range list.Fields {
			if len(item.EnumValues) > 0 && !item.Repeated && sameValues(item.EnumValues, rf.EnumValues) {
				filter, stateField = rf, item
			}
		}
	}
	if filter == nil {
		return
	}
	filterKey, ok := namecase.LookupKey(t.step.Body, filter.Name)
	if !ok {
		filterKey = filter.Name
	}
	values := filter.EnumValues[1:]
	short := enumShort(filter.EnumValues)
	first := t.producers[0]
	initial := ""
	if c, ok := lib.Get(canonicalCall(p.cat, first.Call)); ok {
		initial = stateIn([]string{c.Exports[t.carrier], c.Summary}, values, short)
	}
	transitions, blocked := p.transitionsFor(lib, t, first, values, short, initial)
	moved := p.statesBefore(lib, t, values, short)
	assigned, moves := assignStates(t.producers, moved, initial, transitions)
	p.ownMovedResources(lib, t, moves)
	states := map[string][]*chain.Step{}
	order := []string{}
	if initial != "" {
		order = append(order, initial)
	}
	for _, tr := range transitions {
		order = append(order, tr.value)
	}
	for _, prod := range t.producers {
		if v := moved[prod.ID]; v != "" && !containsString(order, v) {
			order = append(order, v)
		}
	}
	added := []*chain.Step{}
	for _, prod := range t.producers {
		v, known := assigned[prod]
		if !known {
			continue
		}
		states[v] = append(states[v], prod)
		tr := moves[prod]
		if tr == nil {
			continue
		}
		suffix := strings.TrimPrefix(prod.ID, first.ID)
		body := catalog.ScaffoldWith(tr.method.Input(), catalog.ScaffoldOptions{})
		setBodyPath(body, tr.field, "${"+prod.ID+"."+t.carrier+"."+t.itemID+"}")
		step := &chain.Step{
			ID:          p.freeStepID(defaultID(tr.method.Name) + suffix),
			Description: fmt.Sprintf("moves %s to %s, so the fixtures sit in different states for the filtered lists.", prod.ID, short[tr.value]),
			Call:        tr.method.FullName,
			Auth:        tr.contract.Auth,
			Body:        body,
			Expect:      append(SuccessExpectation(tr.method), chain.Expectation{Path: t.carrier + "." + stateField.Name, Equals: tr.value}),
		}
		added = append(added, step)
	}
	creation := false
	if c, ok := lib.Get(canonicalCall(p.cat, t.step.Call)); ok {
		key, desc, stated := stateOrder(c, t.listPath)
		creation = stated && !desc && creationWord.MatchString(key)
	}
	moveIDs := []string{}
	for _, s := range added {
		moveIDs = append(moveIDs, s.ID)
	}
	var after *chain.Step
	if len(moveIDs) > 0 {
		after = copyStep(t.step, p.freeStepID(t.step.ID+"_after_moves"))
		after.Export = nil
		after.Body[filterKey] = filter.EnumValues[0]
		after.Description = fmt.Sprintf("the list as before, with no filter, after %s: every fixture is still listed, in the state it was left in.", strings.Join(moveIDs, ", "))
		after.Expect = SuccessExpectation(lm)
		for i, prod := range t.producers {
			id := "${" + prod.ID + "." + t.carrier + "." + t.itemID + "}"
			v, known := assigned[prod]
			if creation {
				after.Expect = append(after.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d.%s", t.listPath, i, t.itemID), Equals: id})
				if known {
					after.Expect = append(after.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d.%s", t.listPath, i, stateField.Name), Equals: v})
				}
				continue
			}
			want := map[string]any{t.itemID: id}
			if known {
				want[stateField.Name] = v
			}
			after.Expect = append(after.Expect, chain.Expectation{Path: t.listPath, Includes: want})
		}
		after.Expect = append(after.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, len(t.producers)), Exists: boolPtr(false)})
		added = append(added, after)
	}
	ids := []string{}
	for _, v := range order {
		matching := states[v]
		if len(matching) == 0 {
			continue
		}
		filtered := copyStep(t.step, p.freeStepID(t.step.ID+"_"+strings.ToLower(short[v])))
		filtered.Export = nil
		filtered.Body[filterKey] = v
		filtered.Description = fmt.Sprintf("filtered to %s: only %s, and nothing else of the scope.", short[v], stepIDList(matching))
		filtered.Expect = SuccessExpectation(lm)
		for i, prod := range matching {
			if creation || len(matching) == 1 {
				filtered.Expect = append(filtered.Expect, chain.Expectation{
					Path: fmt.Sprintf("%s.%d.%s", t.listPath, i, t.itemID), Equals: "${" + prod.ID + "." + t.carrier + "." + t.itemID + "}"})
			}
			filtered.Expect = append(filtered.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d.%s", t.listPath, i, stateField.Name), Equals: v})
		}
		filtered.Expect = append(filtered.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, len(matching)), Exists: boolPtr(false)})
		added = append(added, filtered)
		ids = append(ids, filtered.ID)
	}
	if len(ids) == 0 {
		return
	}
	at := t.step.ID
	for _, s := range added {
		p.insertAfter(at, s)
		at = s.ID
	}
	unreached, waiting := []string{}, []string{}
	why := []string{}
	for _, v := range values {
		switch {
		case len(states[v]) > 0:
		case blocked[v] != "":
			waiting = append(waiting, v)
			why = append(why, blocked[v])
		default:
			unreached = append(unreached, v)
		}
	}
	msg := fmt.Sprintf("step %s: %s filters on %s; after it, the fixtures are moved into different states and %s each assert only "+
		"the fixtures in that state come back, so a filter that is ignored or matches the wrong value fails", t.step.ID, shortRPC(t.step.Call),
		filter.Name, strings.Join(ids, ", "))
	if len(moved) > 0 {
		msg += ". " + movedNote(t.producers, moved, short)
	}
	if len(unreached) > 0 {
		msg += fmt.Sprintf(". No producer in the contracts reaches %s (a write whose response carries the %s and whose summary or "+
			"exports name the state it leaves it in), so the filter on it is not probed", strings.Join(unreached, ", "), t.carrier)
	}
	if len(waiting) > 0 {
		msg += fmt.Sprintf(". The filter on %s is not probed either: %s, which this plan does not call",
			strings.Join(waiting, ", "), strings.Join(why, "; "))
	}
	if after != nil {
		msg += fmt.Sprintf(". %s lists them again with no filter after %s and asserts every fixture by id in the state it "+
			"was left in, so a write that drops its record from the list (a cancelled order no longer listed) fails there, "+
			"whatever the filtered lists do", after.ID, strings.Join(moveIDs, ", "))
	}
	p.note("%s", msg)
}

func stepIDList(steps []*chain.Step) string {
	ids := []string{}
	for _, s := range steps {
		ids = append(ids, s.ID)
	}
	return strings.Join(ids, ", ")
}

func sameValues(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (p *Plan) transitionsFor(lib *Library, t *listTarget, producer *chain.Step, values []string, short map[string]string, initial string) ([]transition, map[string]string) {
	blocked := map[string]string{}
	pm, err := p.cat.Lookup(producer.Call)
	if err != nil {
		return nil, blocked
	}
	idPath := t.carrier + "." + t.itemID
	out := []transition{}
	seen := map[string]bool{initial: true}
	rpcs := lib.RPCs()
	sort.Strings(rpcs)
	for _, rpc := range rpcs {
		if chain.IsReadOnlyCall(rpc) || rpc == pm.FullName {
			continue
		}
		m, err := p.cat.Lookup(rpc)
		if err != nil || m.Streaming() {
			continue
		}
		carrier := carrierField(m, t.itemMsg)
		if carrier == "" {
			continue
		}
		c, _ := lib.Get(rpc)
		field := ""
		for _, name := range sortedKeys(c.Fields) {
			if ref, err := ParseRef(c.Fields[name].From); err == nil && canonicalCall(p.cat, ref.RPC) == pm.FullName && ref.Path == idPath {
				field = name
			}
		}
		if field == "" {
			continue
		}
		value := stateIn([]string{c.Exports[carrier], c.Summary}, values, short)
		if value == "" || seen[value] {
			continue
		}
		if missing := p.missingDependencies(c); len(missing) > 0 {
			p.note("step %s: %s would move a fixture to %s, but it needs %s, which this plan does not call, so no fixture is "+
				"put in that state: plan %s together with %s to have it", t.step.ID, shortRPC(rpc), short[value],
				strings.Join(missing, ", "), shortRPC(t.step.Call), strings.Join(missing, " "))
			blocked[value] = fmt.Sprintf("%s reaches it but needs %s", shortRPC(rpc), strings.Join(missing, ", "))
			continue
		}
		seen[value] = true
		delete(blocked, value)
		out = append(out, transition{method: m, contract: c, field: field, value: value})
	}
	return out, blocked
}

func (p *Plan) missingDependencies(c *RPCContract) []string {
	called := map[string]bool{}
	for _, st := range p.Chain.Steps {
		called[canonicalCall(p.cat, st.Call)] = true
	}
	out := []string{}
	for _, dep := range c.DependenciesFor("") {
		rpc, _ := SplitNode(dep)
		if !called[canonicalCall(p.cat, rpc)] && !p.fixtureNeed(c, canonicalCall(p.cat, rpc), called) {
			out = append(out, shortRPC(rpc))
		}
	}
	return out
}

func (p *Plan) fixtureNeed(c *RPCContract, rpc string, called map[string]bool) bool {
	declared := false
	for _, n := range c.Needs {
		need, _ := SplitNode(n)
		declared = declared || canonicalCall(p.cat, need) == rpc
	}
	if !declared || chain.IsReadOnlyCall(rpc) || p.lib == nil {
		return false
	}
	nc, ok := p.lib.Get(rpc)
	if !ok {
		return false
	}
	if m, err := p.cat.Lookup(rpc); err != nil || m.Streaming() {
		return false
	}
	for name, ref := range topFrom(nc, p.cat) {
		if !strings.Contains(name, ".") && called[ref.RPC] && !chain.IsReadOnlyCall(ref.RPC) {
			return true
		}
	}
	return false
}
