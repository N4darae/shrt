package contract

import (
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

func scaffoldPlan(name, noun string, refs, ids []string, lib *Library, cat *catalog.Catalog) (*Plan, error) {
	p := &Plan{stepOf: map[string]string{}, cat: cat, reserved: map[string]bool{}, noun: noun}
	for _, id := range ids {
		p.reserved[id] = true
	}
	p.Chain = &chain.Chain{APIVersion: chain.APIVersion, Name: name}
	methods := make([]*catalog.Method, len(refs))
	for i, ref := range refs {
		m, err := cat.Lookup(ref)
		if err != nil {
			return nil, err
		}
		methods[i] = m
		if _, seen := p.stepOf[m.FullName]; !seen {
			p.stepOf[m.FullName] = ids[i]
		}
	}
	producers := map[string][]string{}
	rpcOf := map[string]string{}
	used := map[string]int{}
	readRun := map[string]int{}
	for i, m := range methods {
		read := chain.IsReadOnlyCall(m.FullName)
		if !read {
			readRun = map[string]int{}
		}
		choose := func(choices []string) string {
			if read {
				return choices[readRun[m.FullName]%len(choices)]
			}
			return leastUsed(choices, used)
		}
		for rpc, ids := range producers {
			p.stepOf[rpc] = choose(ids)
		}
		step := p.buildStep(ids[i], "", m, lib)
		p.rewireProducers(step, producers, rpcOf, used, choose)
		if read {
			readRun[m.FullName]++
		}
		if n := len(producers[m.FullName]); n > 0 && p.creates(step, m) {
			distinctProducerAt(step.Body, catalog.DescribeMessage(m.Input()).Fields, strings.TrimPrefix(ids[i], producers[m.FullName][0]+"_"), n)
		} else if n > 0 {
			distinguishFixtures(step, ids[i], producers[m.FullName][0])
		}
		for _, c := range p.splitSharedProducers(step, p.grown) {
			if rpcOf[c.original] != "" {
				producers[c.call] = append(producers[c.call], c.id)
				rpcOf[c.id] = c.call
			}
		}
		producers[m.FullName] = append(producers[m.FullName], ids[i])
		rpcOf[ids[i]] = m.FullName
		p.Chain.Steps = append(p.Chain.Steps, step)
	}
	p.noteUnevenPreparation(producers, rpcOf)
	return p, nil
}

func (p *Plan) rewireProducers(step *chain.Step, producers map[string][]string, rpcOf map[string]string, used map[string]int,
	choose func([]string) string) {
	var walk func(v any, item int) any
	walk = func(v any, item int) any {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				t[k] = walk(x, item)
			}
		case []any:
			for i, x := range t {
				t[i] = walk(x, i)
			}
		case string:
			if !wholeReference(t) {
				return t
			}
			inner := strings.TrimSuffix(strings.TrimPrefix(t, "${"), "}")
			src, path, ok := strings.Cut(inner, ".")
			if !ok {
				return t
			}
			choices := producers[rpcOf[src]]
			if len(choices) < 2 {
				used[src]++
				return t
			}
			pick := choose(choices)
			if item >= 0 {
				pick = choices[item%len(choices)]
			}
			used[pick]++
			return "${" + pick + "." + path + "}"
		}
		return v
	}
	walk(step.Body, -1)
}

func leastUsed(choices []string, used map[string]int) string {
	pick := choices[0]
	for _, c := range choices[1:] {
		if used[c] < used[pick] {
			pick = c
		}
	}
	return pick
}

func distinguishFixtures(step *chain.Step, id, first string) {
	suffix := strings.TrimPrefix(id, first+"_")
	if suffix == id {
		suffix = id
	}
	var walk func(v any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				t[k] = walk(x)
			}
		case []any:
			for i, x := range t {
				t[i] = walk(x)
			}
		case string:
			loc := planVarRef.FindStringIndex(t)
			if loc == nil || (loc[0] == 0 && loc[1] == len(t)) {
				return t
			}
			return markAfterVar(t, loc, suffix)
		}
		return v
	}
	step.Body, _ = walk(step.Body).(map[string]any)
}

func (p *Plan) noteUnevenPreparation(producers map[string][]string, rpcOf map[string]string) {
	readers := map[string]map[string]bool{}
	for _, st := range p.Chain.Steps {
		if chain.IsReadOnlyCall(st.Call) {
			continue
		}
		for _, ref := range st.References() {
			src, _, _ := strings.Cut(strings.TrimSpace(ref), ".")
			if rpcOf[src] == "" {
				continue
			}
			if readers[src] == nil {
				readers[src] = map[string]bool{}
			}
			readers[src][shortRPC(st.Call)] = true
		}
	}
	for _, rpc := range sortedKeys(producers) {
		ids := producers[rpc]
		if len(ids) < 2 {
			continue
		}
		all := map[string]bool{}
		for _, id := range ids {
			for r := range readers[id] {
				all[r] = true
			}
		}
		for _, id := range ids {
			missing := []string{}
			for r := range all {
				if !readers[id][r] {
					missing = append(missing, r)
				}
			}
			if len(missing) == 0 {
				continue
			}
			sort.Strings(missing)
			p.note("step %s: another %s step is read by %s and this one is not: if it needs the same preparation, "+
				"add one more %s step that reads it", id, shortRPC(rpc), strings.Join(missing, ", "), strings.Join(missing, " / "))
		}
	}
}

func shortRPC(full string) string { return full[strings.LastIndex(full, "/")+1:] }

func ScaffoldChain(name, description string, refs, ids []string, lib *Library, cat *catalog.Catalog) ([]byte, []string, error) {
	p, err := scaffoldPlan(name, "the chain", refs, ids, lib, cat)
	if err != nil {
		return nil, nil, err
	}
	p.Chain.Description = description
	p.nameBySituation()
	for _, st := range p.Chain.Steps {
		dropPlaceholderEnums(st, lib, cat)
		if rc, ok := lib.Get(canonicalCall(cat, st.Call)); ok && st.Description == FirstSentence(rc.Summary) {
			st.Description = ""
		}
	}
	p.discriminateListOrder(lib, nil)
	p.assertContracts(lib)
	p.noteRequirements()
	if missing, _ := chain.ExternalInputs(p.Chain); len(missing) > 0 {
		p.declareInterpolatedVars(missing)
	}
	raw, err := p.YAML()
	if err != nil {
		return nil, nil, fmt.Errorf("render chain %s: %w", name, err)
	}
	steps := map[string]bool{}
	for _, st := range p.Chain.Steps {
		steps[st.ID] = true
	}
	return raw, groupStepNotes(p.Notes, steps), nil
}

func (p *Plan) assertContracts(lib *Library) {
	if lib == nil {
		return
	}
	notes := len(p.Notes)
	held := p.refuseByState(lib)
	p.echoNumbers()
	p.assertOutcomes(lib)
	p.Notes = p.Notes[:notes]
	p.assertStates(lib)
	p.assertReadBack(lib)
	p.assertHeldStates(lib, held)
	r := p.effectRules(lib)
	if len(r.increase) == 0 && len(r.total) == 0 {
		return
	}
	if p.met == nil {
		p.met = map[[2]string]bool{}
	}
	asserted, _, _ := p.effectPass(lib, r, true)
	ids := []string{}
	for _, st := range p.Chain.Steps {
		for _, kind := range []string{"zero", "increase", "batch", "total", "read"} {
			if containsString(asserted[kind], st.ID) && !containsString(ids, st.ID) {
				ids = append(ids, st.ID)
			}
		}
	}
	if len(ids) > 0 {
		p.note("%s %s: %s levels and totals worked out from the quantities and prices sent, by the contracts' effects, so they change if those do",
			pluralVerb(len(ids), "step", "steps"), strings.Join(ids, ", "), pluralVerb(len(ids), "asserts", "assert"))
	}
}

func dropPlaceholderEnums(st *chain.Step, lib *Library, cat *catalog.Catalog) {
	m, err := cat.Lookup(st.Call)
	if err != nil {
		return
	}
	rc, _ := lib.Get(m.FullName)
	for _, f := range catalog.DescribeMessage(m.Input()).Fields {
		key, ok := namecase.LookupKey(st.Body, f.Name)
		if !ok || len(f.EnumValues) == 0 || st.Body[key] != f.EnumValues[0] || !IsPlaceholderEnumValue(f.EnumValues[0]) || contractRequiresField(rc, f.Name) {
			continue
		}
		if rc != nil && rc.Fields[f.Name] != nil && rc.Fields[f.Name].Value != "" {
			continue
		}
		delete(st.Body, key)
	}
}

func (p *Plan) createdState(lib *Library, prod *chain.Step, carrier string, state *catalog.Field) string {
	c, ok := lib.Get(canonicalCall(p.cat, prod.Call))
	if !ok {
		return ""
	}
	return stateIn([]string{c.Exports[carrier], c.Summary}, state.EnumValues[1:], enumShort(state.EnumValues))
}

func (p *Plan) refuseByState(lib *Library) map[string]map[string]string {
	held, by := map[string]string{}, map[string]string{}
	before := map[string]map[string]string{}
	for _, st := range p.Chain.Steps {
		snap := make(map[string]string, len(held))
		for k, v := range held {
			snap[k] = v
		}
		before[st.ID] = snap
		if st.AllowFail || chain.IsReadOnlyCall(st.Call) {
			continue
		}
		c, m, ok := p.contractOf(lib, canonicalCall(p.cat, st.Call))
		if !ok {
			continue
		}
		for _, e := range p.entityStates(lib, st, c) {
			values, short := e.state.EnumValues[1:], enumShort(e.state.EnumValues)
			cur := held[e.producer.ID]
			if !isRefusalStep(st) && cur != "" && cur != p.createdState(lib, e.producer, e.carrier, e.state) {
				for _, f := range lib.AllFailures(m.FullName) {
					if probeable(f) && failureState(f, values, short) == cur {
						st.Expect = refusalOf(m, f)
						st.Description = fmt.Sprintf("refused with %s: %s left the %s %s.", f.Label(), by[e.producer.ID], e.carrier, short[cur])
						break
					}
				}
			}
			if cur == "" {
				held[e.producer.ID] = p.createdState(lib, e.producer, e.carrier, e.state)
			}
			carrier := carrierField(m, e.itemMsg)
			if isRefusalStep(st) || carrier == "" {
				continue
			}
			if v := stateIn([]string{c.Exports[carrier], c.Summary}, values, short); v != "" {
				held[e.producer.ID], by[e.producer.ID] = v, st.ID
			}
		}
	}
	return before
}

func (p *Plan) assertHeldStates(lib *Library, before map[string]map[string]string) {
	for _, st := range p.Chain.Steps {
		if st.AllowFail || !chain.IsReadOnlyCall(st.Call) || p.streams(st) || effectOutcome(st) != outcomeSuccess {
			continue
		}
		c, m, ok := p.contractOf(lib, canonicalCall(p.cat, st.Call))
		if !ok {
			continue
		}
		state := func(prod *chain.Step, carrier string, field *catalog.Field) string {
			if v := before[st.ID][prod.ID]; v != "" {
				return v
			}
			return p.createdState(lib, prod, carrier, field)
		}
		if car := singleCarrier(m); car != nil {
			for _, e := range p.entityStates(lib, st, c) {
				path := car.Name + "." + e.state.Name
				if v := state(e.producer, e.carrier, e.state); car.Message == e.itemMsg && v != "" && !hasExpectOn(st, path) {
					st.Expect = append(st.Expect, chain.Expectation{Path: path, Equals: v})
				}
			}
			continue
		}
		p.assertListed(c, st, m, state)
	}
}

func (p *Plan) assertListed(c *RPCContract, st *chain.Step, m *catalog.Method, state func(*chain.Step, string, *catalog.Field) string) {
	for key, v := range st.Body {
		if text, _ := v.(string); text != "" && strings.Contains(namecase.Fold(key), "prefix") {
			return
		}
	}
	t := p.listTargetFor(st, true)
	if t == nil {
		return
	}
	for _, e := range st.Expect {
		if e.Path == t.listPath || strings.HasPrefix(e.Path, t.listPath+".") {
			return
		}
	}
	scope := p.scopeOf(t)
	if scope.parent == nil || c.Fields[scope.parentKey] == nil {
		return
	}
	if ref, err := ParseRef(c.Fields[scope.parentKey].From); err != nil || canonicalCall(p.cat, ref.RPC) != canonicalCall(p.cat, scope.parent.Call) {
		return
	}
	list := repeatedMessageField(m)
	var field *catalog.Field
	for _, f := range list.Fields {
		if len(f.EnumValues) > 1 && !f.Repeated {
			field = f
		}
	}
	for _, rf := range catalog.DescribeMessage(m.Input()).Fields {
		key, sent := namecase.LookupKey(st.Body, rf.Name)
		if sent && field != nil && sameValues(rf.EnumValues, field.EnumValues) && st.Body[key] != rf.EnumValues[0] {
			return
		}
	}
	key, desc, stated := stateOrder(c, t.listPath)
	positional := stated && !desc && creationWord.MatchString(key)
	for i, prod := range t.producers {
		id := "${" + prod.ID + "." + t.carrier + "." + t.itemID + "}"
		v := ""
		if field != nil {
			v = state(prod, t.carrier, field)
		}
		if !positional {
			want := map[string]any{t.itemID: id}
			if v != "" {
				want[field.Name] = v
			}
			st.Expect = append(st.Expect, chain.Expectation{Path: t.listPath, Includes: want})
			continue
		}
		st.Expect = append(st.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d.%s", t.listPath, i, t.itemID), Equals: id})
		if v != "" {
			st.Expect = append(st.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d.%s", t.listPath, i, field.Name), Equals: v})
		}
	}
	st.Expect = append(st.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, len(t.producers)), Exists: boolPtr(false)})
}
