package contract

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

const largeValue = 12345

var (
	positiveOnly = lazyRegexp(`(?i)zero or negative|not positive|non-positive|must be positive|is positive|greater than zero|greater than 0\b|less than 1\b|below 1\b|at least 1\b|> ?0\b|<= ?0\b`)
	atLeastN     = lazyRegexp(`(?i)(?:at least|minimum(?: is| of)?|no less than)\s+(\d+)`)
	belowN       = lazyRegexp(`(?i)(?:less than|below|under|fewer than)\s+(\d+)`)
)

func spreadValue(base int64, rank int, quantity bool) int64 {
	if base <= 0 {
		base = 1
	}
	if rank <= 0 {
		return base
	}
	if quantity {
		return base + int64(rank)
	}
	mid := base + 1000
	if rank == 1 {
		return mid
	}
	if rank > 2 {
		return spreadValue(base, 2, false) + int64(rank-2)*1000
	}
	if largeValue > mid {
		return largeValue
	}
	return mid*10 + 5
}

func itemNumbers(producers []*chain.Step, fields []*catalog.Field, visit func(rank int, item map[string]any, key string, sub *catalog.Field, n int64)) []string {
	names := []string{}
	for _, f := range fields {
		if !f.Repeated || f.Kind != "message" || f.MapKey != "" {
			continue
		}
		found := false
		for rank, prod := range producers {
			key, ok := namecase.LookupKey(prod.Body, f.Name)
			if !ok {
				continue
			}
			list, _ := prod.Body[key].([]any)
			for _, raw := range list {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				for _, sub := range f.Fields {
					k, ok := namecase.LookupKey(item, sub.Name)
					if !ok || sub.Repeated || !chain.IsNumericKind(sub.Kind) || idLike(sub.Name) {
						continue
					}
					n, ok := numericValue(item[k])
					if !ok || n == 0 {
						continue
					}
					found = true
					if visit != nil {
						visit(rank, item, k, sub, n)
					}
				}
			}
		}
		if found {
			names = append(names, f.Name)
		}
	}
	return names
}

func varyItemNumbers(producers []*chain.Step, fields []*catalog.Field, perm []int) []int {
	down, shift := true, int64(0)
	itemNumbers(producers, fields, func(_ int, _ map[string]any, _ string, sub *catalog.Field, n int64) {
		down = down && isQuantityName(sub.Name)
		shift = max(shift, int64(len(producers))-n)
	})
	itemNumbers(producers, fields, func(k int, item map[string]any, key string, sub *catalog.Field, n int64) {
		rank := perm[k]
		switch {
		case down:
			item[key] = strconv.FormatInt(n+shift-int64(rank), 10)
		case isQuantityName(sub.Name):
			item[key] = strconv.FormatInt(n+int64(rank), 10)
		default:
			item[key] = strconv.FormatInt(spreadValue(n, rank, false), 10)
		}
	})
	if !down {
		return perm
	}
	sorted := make([]int, len(perm))
	for k, r := range perm {
		sorted[k] = len(perm) - 1 - r
	}
	return sorted
}

func parseMinimum(text string) (int64, bool) {
	if positiveOnly().MatchString(text) {
		return 1, true
	}
	for _, re := range []*regexp.Regexp{atLeastN(), belowN()} {
		if m := re.FindStringSubmatch(text); m != nil {
			if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func mentionsField(text, name string) bool {
	re, err := regexp.Compile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
	return err == nil && re.MatchString(text)
}

func statedBound(lib *Library, rpc string, c *RPCContract, name string, parse func(string) (int64, bool)) (int64, *Failure, bool) {
	for _, f := range lib.AllFailures(rpc) {
		if f.Field != name && !mentionsField(f.When, name) {
			continue
		}
		if n, ok := parse(f.When); ok {
			return n, &f, true
		}
	}
	if fc := c.Fields[name]; fc != nil {
		if n, ok := parse(fc.Note); ok {
			return n, nil, true
		}
	}
	return 0, nil, false
}

var largeQuantities = []int64{1250, largeValue}

func (p *Plan) probeBoundaries(lib *Library, isTarget func(*chain.Step) bool) {
	rules := p.effectRules(lib)
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || chain.IsReadOnlyCall(st.Call) {
			continue
		}
		c, m, ok := p.contractOf(lib, st.Call)
		if !ok {
			continue
		}
		said := []string{}
		quantities, unbounded := []string{}, []string{}
		rpc := canonicalCall(p.cat, st.Call)
		if b := rules.batch[rpc]; b != nil {
			if probe := p.largeBatchQuantities(lib, st, b); probe != "" {
				said = append(said, probe)
			}
		} else if probe := p.largeBatchLine(lib, rules, st, c, m); probe != "" {
			said = append(said, probe)
		} else {
			said = append(said, p.largeItemFields(lib, rules, st, c, m)...)
		}
		for _, f := range catalog.DescribeMessage(m.Input()).Fields {
			if f.Repeated || f.MapKey != "" || !chain.IsNumericKind(f.Kind) || idLike(f.Name) || len(f.EnumValues) > 0 {
				continue
			}
			key, ok := namecase.LookupKey(st.Body, f.Name)
			if !ok {
				continue
			}
			if _, literal := numericValue(st.Body[key]); !literal {
				continue
			}
			quantity := isQuantityName(f.Name)
			min, failure, stated := statedBound(lib, st.Call, c, f.Name, parseMinimum)
			if quantity && !(rules.increase[rpc] != nil && rules.increase[rpc].qtyField == f.Name) {
				quantities = append(quantities, f.Name)
			}
			if !stated {
				unbounded = append(unbounded, f.Name)
			}
			if stated {
				probe := p.probeCopy(lib, st, f.Name+"_min")
				probe.Body[key] = strconv.FormatInt(min, 10)
				renameStepRefs(probe, st.ID, probe.ID)
				probe.Description = fmt.Sprintf("%s at its stated minimum, %d, is accepted.", f.Name, min)
				p.Chain.Steps = append(p.Chain.Steps, probe)
				below := p.probeCopy(lib, st, f.Name+"_below_min")
				below.Body[key] = strconv.FormatInt(min-1, 10)
				if failure != nil {
					below.Expect = refusalFor(m, *failure)
					below.Description = fmt.Sprintf("%s at %d, one below its minimum, is refused with %s.", f.Name, min-1, failure.Label())
				} else {
					below.Expect = []chain.Expectation{{Path: chain.EnvelopePath(), NotEqual: chain.EnvelopeOK()}}
					below.Description = fmt.Sprintf("%s at %d, one below its minimum, is refused.", f.Name, min-1)
				}
				p.Chain.Steps = append(p.Chain.Steps, p.guardUnchanged(lib, []*chain.Step{below}, below.ID)...)
				from := "the field's note"
				if failure != nil {
					from = failure.Label()
				}
				said = append(said, fmt.Sprintf("%s (%s = %d, accepted) and %s (%d, refused), from %s", probe.ID, f.Name, min, below.ID, min-1, from))
				if neg := p.negativeProbe(lib, st, m, f, key, min, failure); neg != "" {
					said = append(said, neg)
				}
			}
			if inc := rules.increase[rpc]; quantity && inc != nil && inc.qtyField == f.Name {
				ids := []string{}
				for _, q := range largeQuantities {
					large := p.probeCopy(lib, st, fmt.Sprintf("%s_large_%d", f.Name, q))
					large.Body[key] = strconv.FormatInt(q, 10)
					renameStepRefs(large, st.ID, large.ID)
					large.Description = fmt.Sprintf("%s at %d, far above what the fixtures add: %s is the level before plus all of it.", f.Name, q, inc.moved)
					p.Chain.Steps = append(p.Chain.Steps, large)
					ids = append(ids, large.ID)
				}
				said = append(said, fmt.Sprintf("%s (%s = %s, an addition the fixtures' small quantities never make, so a cap or "+
					"overflow on it shows in %s, asserted as the level before plus %s)", strings.Join(ids, ", "), f.Name,
					joinInts(largeQuantities), inc.moved, f.Name))
				quantity = false
			} else if !quantity {
				large := p.probeCopy(lib, st, f.Name+"_large")
				large.Body[key] = strconv.Itoa(largeValue)
				renameStepRefs(large, st.ID, large.ID)
				large.Description = fmt.Sprintf("%s at %d, a magnitude the fixtures do not reach, is accepted and stored as sent.", f.Name, largeValue)
				p.Chain.Steps = append(p.Chain.Steps, large)
				said = append(said, fmt.Sprintf("%s (%s = %d, a magnitude where rounding and scaling bugs show)", large.ID, f.Name, largeValue))
			}
		}
		if len(said) > 0 {
			msg := fmt.Sprintf("step %s: boundary and magnitude probes %s.", st.ID, strings.Join(said, "; "))
			if len(quantities) > 0 {
				msg += fmt.Sprintf(" %s %s not probed large, since a large quantity runs into stock rules rather than arithmetic.",
					strings.Join(quantities, ", "), pluralVerb(len(quantities), "is", "are"))
			}
			if len(unbounded) > 0 {
				msg += fmt.Sprintf(" %s %s no stated minimum; declare one in a failure's when: (\"qty is zero or negative\") or "+
					"the field's note to have it probed at it and one below", strings.Join(unbounded, ", "), pluralVerb(len(unbounded), "has", "have"))
			}
			p.note("%s", strings.TrimSuffix(msg, "."))
		}
	}
}

func isRefusalStep(st *chain.Step) bool {
	for _, e := range st.Expect {
		if e.Path == "transport.code" || (chain.IsEnvelopePath(e.Path) && e.NotEqual != nil) {
			return true
		}
	}
	return false
}

func (p *Plan) echoNumbers() {
	for _, st := range p.Chain.Steps {
		if chain.IsReadOnlyCall(st.Call) || isRefusalStep(st) {
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		carriers := []*catalog.Field{}
		for _, fd := range catalog.DescribeMessage(m.Output()).Fields {
			if fd.Kind == "message" && !fd.Repeated && fd.MapKey == "" && fd.Name != chain.EnvelopeField() && !IsVerdictFieldName(fd.Name) {
				carriers = append(carriers, fd)
			}
		}
		if len(carriers) != 1 {
			continue
		}
		for _, f := range catalog.DescribeMessage(m.Input()).Fields {
			if f.Repeated || f.MapKey != "" || !chain.IsNumericKind(f.Kind) {
				continue
			}
			key, ok := namecase.LookupKey(st.Body, f.Name)
			if !ok {
				continue
			}
			if _, literal := numericValue(st.Body[key]); !literal {
				continue
			}
			for _, out := range carriers[0].Fields {
				path := carriers[0].Name + "." + out.Name
				if out.Name != f.Name || !chain.IsNumericKind(out.Kind) || out.Repeated || hasExpectOn(st, path) {
					continue
				}
				st.Expect = append(st.Expect, chain.Expectation{Path: path, Equals: "${steps." + st.ID + ".request." + key + "}"})
			}
		}
	}
}

func hasExpectOn(st *chain.Step, path string) bool {
	return slices.ContainsFunc(st.Expect, func(e chain.Expectation) bool { return e.Path == path })
}

func joinInts(ns []int64) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.FormatInt(n, 10)
	}
	return strings.Join(parts, " and ")
}

func (p *Plan) largeBatchQuantities(lib *Library, st *chain.Step, b *batchRule) string {
	key, ok := namecase.LookupKey(st.Body, b.list)
	if !ok {
		return ""
	}
	items, _ := st.Body[key].([]any)
	if len(items) == 0 {
		return ""
	}
	probe := p.probeCopy(lib, st, b.stock.qtyField+"_large")
	renameStepRefs(probe, st.ID, probe.ID)
	lines, _ := probe.Body[key].([]any)
	for i, raw := range lines {
		item, ok := raw.(map[string]any)
		if !ok {
			return ""
		}
		k, ok := namecase.LookupKey(item, b.stock.qtyField)
		if !ok {
			return ""
		}
		item[k] = strconv.FormatInt(largeQuantities[i%len(largeQuantities)], 10)
	}
	probe.Description = fmt.Sprintf("as %s, each line's %s far above what the fixtures add (%s): each %s.N.%s is the level before plus all of it.",
		st.ID, b.stock.qtyField, joinInts(largeQuantities), b.results, b.stock.moved)
	p.Chain.Steps = append(p.Chain.Steps, probe)
	return fmt.Sprintf("%s (%s.%s = %s, additions the fixtures' small quantities never make, so a cap or overflow shows in each %s.N.%s)",
		probe.ID, b.list, b.stock.qtyField, joinInts(largeQuantities), b.results, b.stock.moved)
}

func (p *Plan) largeBatchLine(lib *Library, rules *effectRules, st *chain.Step, c *RPCContract, m *catalog.Method) string {
	results := p.perItemResults(lib, st.Call, c, m)
	listPath, verdict, ok := strings.Cut(chain.ItemEnvelope(), "[].")
	if results == nil || !ok {
		return ""
	}
	for _, rf := range catalog.DescribeMessage(m.Input()).Fields {
		if !rf.Repeated || rf.Kind != "message" || rf.MapKey != "" {
			continue
		}
		key, first, ok := firstLine(st.Body, rf.Name)
		if !ok {
			continue
		}
		items := st.Body[key].([]any)
		last, ok := items[len(items)-1].(map[string]any)
		if !ok {
			continue
		}
		single, field, inc := p.singleItemLarge(lib, rules, st.Call, rf)
		if single == "" {
			continue
		}
		subKey, ok := namecase.LookupKey(first, field)
		if !ok {
			continue
		}
		large, _ := cloneBody(first).(map[string]any)
		large[subKey] = strconv.Itoa(largeValue)
		probe, reads := p.batchProbe(lib, st, m, key, results, listPath, verdict, field+"_large",
			fmt.Sprintf("%s.0 has %s %d, a magnitude the fixtures do not reach, as %s is probed; %s.1 is a normal line. Both are applied.",
				rf.Name, field, largeValue, shortRPC(single), rf.Name),
			[]batchLine{{item: large}, {item: cloneBody(last)}})
		grew := ""
		if inc != nil {
			for _, f := range results.Fields {
				if f.Name == inc.moved && chain.IsNumericKind(f.Kind) && !f.Repeated {
					probe.Expect = append(probe.Expect, chain.Expectation{Path: fmt.Sprintf("%s.0.%s", listPath, f.Name), Gte: strconv.Itoa(largeValue)})
					grew = fmt.Sprintf(", %s.0.%s at least %d", listPath, f.Name, largeValue)
				}
			}
		}
		return fmt.Sprintf("%s (%s.0.%s = %d beside a normal line, as %s gets, each line applied%s, and %s read back what each line "+
			"reported, so a cap or overflow on the batch line shows)", probe.ID, rf.Name, field, largeValue, shortRPC(single), grew, stepIDList(reads))
	}
	return ""
}

func (rules *effectRules) movesStock(rpc string) bool {
	if rules.increase[rpc] != nil || rules.batch[rpc] != nil || rules.reserve[rpc] != nil {
		return true
	}
	for _, sp := range rules.specs[rpc] {
		if sp.form == "increase" || sp.form == "reserve" || sp.form == "batch" {
			return true
		}
	}
	return false
}

func (p *Plan) largeItemFields(lib *Library, rules *effectRules, st *chain.Step, c *RPCContract, m *catalog.Method) []string {
	said := []string{}
	rpc := canonicalCall(p.cat, st.Call)
	for _, rf := range catalog.DescribeMessage(m.Input()).Fields {
		if !rf.Repeated || rf.Kind != "message" || rf.MapKey != "" {
			continue
		}
		key, first, ok := firstLine(st.Body, rf.Name)
		if !ok {
			continue
		}
		for _, sub := range rf.Fields {
			if sub.Repeated || !chain.IsNumericKind(sub.Kind) || idLike(sub.Name) || len(sub.EnumValues) > 0 {
				continue
			}
			k, ok := namecase.LookupKey(first, sub.Name)
			if !ok {
				continue
			}
			if _, literal := numericValue(first[k]); !literal {
				continue
			}
			if isQuantityName(sub.Name) && rules.movesStock(rpc) {
				continue
			}
			if _, capped := statedNumericMaximum(lib, rpc, c, rf.Name+"."+sub.Name); capped {
				continue
			}
			if _, capped := statedNumericMaximum(lib, rpc, c, sub.Name); capped {
				continue
			}
			probe := p.probeCopy(lib, st, sub.Name+"_large")
			list, _ := probe.Body[key].([]any)
			item, _ := list[0].(map[string]any)
			item[k] = strconv.Itoa(largeValue)
			renameStepRefs(probe, st.ID, probe.ID)
			probe.Description = fmt.Sprintf("as %s, but %s.0.%s at %d, a magnitude the fixtures do not reach, the other items as they were: accepted, and stored as sent.",
				st.ID, rf.Name, sub.Name, largeValue)
			p.Chain.Steps = append(p.Chain.Steps, probe)
			said = append(said, fmt.Sprintf("%s (%s.0.%s = %d beside normal items, a magnitude where caps and overflow show)", probe.ID, rf.Name, sub.Name, largeValue))
		}
	}
	return said
}

func firstItem(items []any) (map[string]any, bool) {
	if len(items) == 0 {
		return nil, false
	}
	m, ok := items[0].(map[string]any)
	return m, ok
}

func firstLine(body map[string]any, list string) (string, map[string]any, bool) {
	key, ok := namecase.LookupKey(body, list)
	if !ok {
		return "", nil, false
	}
	items, _ := body[key].([]any)
	first, ok := firstItem(items)
	return key, first, ok
}

func (p *Plan) singleItemLarge(lib *Library, rules *effectRules, batch string, rf *catalog.Field) (string, string, *stockRule) {
	for _, rpc := range lib.RPCs() {
		if rpc == batch || chain.IsReadOnlyCall(rpc) {
			continue
		}
		m, err := p.cat.Lookup(rpc)
		if err != nil || m.Streaming() {
			continue
		}
		in := map[string]*catalog.Field{}
		for _, f := range catalog.DescribeMessage(m.Input()).Fields {
			if !f.Repeated && f.MapKey == "" {
				in[f.Name] = f
			}
		}
		mirrors, idShared := true, false
		for _, sub := range rf.Fields {
			f := in[sub.Name]
			if f == nil || f.Kind != sub.Kind {
				mirrors = false
				break
			}
			idShared = idShared || idLike(sub.Name)
		}
		if !mirrors || !idShared {
			continue
		}
		for _, sub := range rf.Fields {
			if sub.Repeated || !chain.IsNumericKind(sub.Kind) || idLike(sub.Name) || len(sub.EnumValues) > 0 {
				continue
			}
			if inc := rules.increase[rpc]; inc != nil && inc.qtyField == sub.Name {
				return rpc, sub.Name, inc
			}
			if !isQuantityName(sub.Name) {
				return rpc, sub.Name, nil
			}
		}
	}
	return "", "", nil
}

const (
	widePrice = int64(1500000000)
	wideLimit = int64(1) << 31
)

var lengthUnit = lazyRegexp(`(?i)^\s*(?:characters|chars|char|letters|runes|code points|bytes|items|lines|entries)\b`)

var aboveN = lazyRegexp(`(?i)(?:at most|no more than|up to|maximum(?: is| of)?|not exceed|exceeds?|more than|greater than|above|over)\s+(\d+)`)

func statedNumericMaximum(lib *Library, rpc string, c *RPCContract, name string) (int64, bool) {
	texts := []string{}
	for _, f := range lib.AllFailures(rpc) {
		if f.Field == name || mentionsField(f.When, name) {
			texts = append(texts, f.When)
		}
	}
	if c != nil {
		if fc := c.Fields[name]; fc != nil {
			texts = append(texts, fc.Note)
		}
	}
	best, found := int64(0), false
	for _, t := range texts {
		for _, at := range aboveN().FindAllStringSubmatchIndex(t, -1) {
			n, err := strconv.ParseInt(t[at[2]:at[3]], 10, 64)
			if err != nil || n <= 0 || lengthUnit().MatchString(t[at[1]:]) {
				continue
			}
			if !found || n < best {
				best, found = n, true
			}
		}
	}
	return best, found
}

func is64BitKind(kind string) bool {
	return strings.Contains(kind, "64")
}

func (p *Plan) probeWideTotals(lib *Library, isTarget func(*chain.Step) bool) {
	rules := p.effectRules(lib)
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		rpc := canonicalCall(p.cat, st.Call)
		t := rules.total[rpc]
		if t == nil || !isTarget(st) || effectOutcome(st) != outcomeSuccess {
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		kind := ""
		fields := catalog.DescribeMessage(m.Output()).Fields
		if t.carrier != "" {
			fields = carrierFields(m, t.carrier)
		}
		for _, sf := range fields {
			if sf.Name == t.field {
				kind = sf.Kind
			}
		}
		if !is64BitKind(kind) {
			continue
		}
		key, first, ok := firstLine(st.Body, t.list)
		if !ok {
			continue
		}
		ref, _ := first[t.itemID].(string)
		entity := p.stepByID(stepRefIn(ref))
		if entity == nil || canonicalCall(p.cat, entity.Call) != t.entity {
			continue
		}
		priceKey, ok := namecase.LookupKey(entity.Body, t.price)
		if !ok {
			continue
		}
		if _, literal := numericValue(entity.Body[priceKey]); !literal {
			continue
		}
		price := widePrice
		ec, _ := lib.Get(t.entity)
		if max, ok := statedNumericMaximum(lib, t.entity, ec, t.price); ok && max < price {
			price = max
		}
		qty := (wideLimit*2)/price + 1
		sc, _ := lib.Get(rpc)
		if max, ok := statedNumericMaximum(lib, rpc, sc, t.list+"."+t.itemQty); ok && max < qty {
			qty = max
		} else if max, ok := statedNumericMaximum(lib, rpc, sc, t.itemQty); ok && max < qty {
			qty = max
		}
		if price*qty <= wideLimit {
			p.gap("step %s: %s is a 64-bit number, but the bounds its contract states on %s and %s keep %s × %s at or below 2^31, "+
				"so no probe checks the sum past 32 bits", st.ID, join(t.carrier, t.field), t.price, t.itemQty, t.price, t.itemQty)
			continue
		}
		id := p.freeStepID(st.ID + "_wide_total")
		prod := probeStep(entity, p.freeStepID(entity.ID+"_for_"+id))
		p.freshen(lib, prod)
		prod.Body[priceKey] = strconv.FormatInt(price, 10)
		renameStepRefs(prod, entity.ID, prod.ID)
		prod.Description = fmt.Sprintf("as %s, but priced at %d, for %s.", entity.ID, price, id)
		probe := probeStep(st, id)
		p.freshen(lib, probe)
		line := cloneBody(first).(map[string]any)
		line[t.itemID] = strings.Replace(ref, "${"+entity.ID+".", "${"+prod.ID+".", 1)
		qtyKey, ok := namecase.LookupKey(line, t.itemQty)
		if !ok {
			qtyKey = t.itemQty
		}
		line[qtyKey] = strconv.FormatInt(qty, 10)
		probe.Body[key] = []any{line}
		renameStepRefs(probe, st.ID, probe.ID)
		probe.Expect = SuccessExpectation(m)
		p.assertEcho(probe)
		probe.Description = fmt.Sprintf("one %s of %d at %d: %s is %d, past 2^31 and 2^32, so a sum kept in 32 bits wraps and fails.",
			strings.TrimSuffix(t.list, "s"), qty, price, join(t.carrier, t.field), price*qty)
		p.Chain.Steps = append(p.Chain.Steps, prod, probe)
		p.note("step %s: %s is a 64-bit number, so %s sends one %s of %d at %s %d (from %s) and asserts it is exactly %d: "+
			"the fixtures' totals fit in 32 bits, and a backend that computes or stores the sum in 32 bits wraps there",
			st.ID, join(t.carrier, t.field), id, strings.TrimSuffix(t.list, "s"), qty, t.price, price, prod.ID, price*qty)
	}
}
