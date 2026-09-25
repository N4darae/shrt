package contract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

const largeValue = 12345

var (
	positiveOnly = regexp.MustCompile(`(?i)zero or negative|not positive|non-positive|must be positive|is positive|greater than zero|greater than 0\b|less than 1\b|below 1\b|at least 1\b|> ?0\b|<= ?0\b`)
	atLeastN     = regexp.MustCompile(`(?i)(?:at least|minimum(?: is| of)?|no less than)\s+(\d+)`)
	belowN       = regexp.MustCompile(`(?i)(?:less than|below|under|fewer than)\s+(\d+)`)
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
	if largeValue > mid {
		return largeValue
	}
	return mid*10 + 5
}

func varyItemNumbers(producers []*chain.Step, fields []*catalog.Field) {
	for _, f := range fields {
		if !f.Repeated || f.Kind != "message" || f.MapKey != "" {
			continue
		}
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
					if isQuantityName(sub.Name) {
						item[k] = strconv.FormatInt(smallerQuantity(n, rank), 10)
						continue
					}
					item[k] = strconv.FormatInt(spreadValue(n, rank, false), 10)
				}
			}
		}
	}
}

func parseMinimum(text string) (int64, bool) {
	if positiveOnly.MatchString(text) {
		return 1, true
	}
	for _, re := range []*regexp.Regexp{atLeastN, belowN} {
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

func statedMinimum(lib *Library, rpc string, c *RPCContract, name string) (int64, *Failure, bool) {
	for _, f := range lib.AllFailures(rpc) {
		if f.Field != name && !mentionsField(f.When, name) {
			continue
		}
		if n, ok := parseMinimum(f.When); ok {
			failure := f
			return n, &failure, true
		}
	}
	if fc := c.Fields[name]; fc != nil {
		if n, ok := parseMinimum(fc.Note); ok {
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
		c, ok := lib.Get(st.Call)
		if !ok {
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		said := []string{}
		quantities, unbounded := []string{}, []string{}
		rpc := canonicalCall(p.cat, st.Call)
		if b := rules.batch[rpc]; b != nil {
			if probe := p.largeBatchQuantities(lib, st, b); probe != "" {
				said = append(said, probe)
			}
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
			min, failure, stated := statedMinimum(lib, st.Call, c, f.Name)
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
					strings.Join(quantities, ", "), pluralIs(len(quantities)))
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
	for _, e := range st.Expect {
		if e.Path == path {
			return true
		}
	}
	return false
}

func smallerQuantity(base int64, rank int) int64 {
	if base-int64(rank) >= 1 {
		return base - int64(rank)
	}
	return base + int64(rank)
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

const (
	widePrice = int64(1500000000)
	wideLimit = int64(1) << 31
)

var lengthUnit = regexp.MustCompile(`(?i)^\s*(?:characters|chars|char|letters|runes|code points|bytes|items|lines|entries)\b`)

var aboveN = regexp.MustCompile(`(?i)(?:at most|no more than|up to|maximum(?: is| of)?|not exceed|exceeds?|more than|greater than|above|over)\s+(\d+)`)

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
		for _, at := range aboveN.FindAllStringSubmatchIndex(t, -1) {
			n, err := strconv.ParseInt(t[at[2]:at[3]], 10, 64)
			if err != nil || n <= 0 || lengthUnit.MatchString(t[at[1]:]) {
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
		for _, sf := range carrierFields(m, t.carrier) {
			if sf.Name == t.field {
				kind = sf.Kind
			}
		}
		if !is64BitKind(kind) {
			continue
		}
		key, ok := namecase.LookupKey(st.Body, t.list)
		if !ok {
			continue
		}
		items, _ := st.Body[key].([]any)
		if len(items) == 0 {
			continue
		}
		first, ok := items[0].(map[string]any)
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
			p.note("step %s: %s.%s is a 64-bit number, but the bounds its contract states on %s and %s keep %s × %s at or below 2^31, "+
				"so no probe checks the sum past 32 bits", st.ID, t.carrier, t.field, t.price, t.itemQty, t.price, t.itemQty)
			continue
		}
		id := p.freeStepID(st.ID + "_wide_total")
		prod := copyStep(entity, p.freeStepID(entity.ID+"_for_"+id))
		prod.Export = nil
		p.freshen(lib, prod)
		prod.Body[priceKey] = strconv.FormatInt(price, 10)
		renameStepRefs(prod, entity.ID, prod.ID)
		prod.Description = fmt.Sprintf("as %s, but priced at %d, for %s.", entity.ID, price, id)
		probe := copyStep(st, id)
		probe.Export = nil
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
		probe.Description = fmt.Sprintf("one %s of %d at %d: %s.%s is %d, past 2^31 and 2^32, so a sum kept in 32 bits wraps and fails.",
			strings.TrimSuffix(t.list, "s"), qty, price, t.carrier, t.field, price*qty)
		p.Chain.Steps = append(p.Chain.Steps, prod, probe)
		p.note("step %s: %s.%s is a 64-bit number, so %s sends one %s of %d at %s %d (from %s) and asserts it is exactly %d: "+
			"the fixtures' totals fit in 32 bits, and a backend that computes or stores the sum in 32 bits wraps there",
			st.ID, t.carrier, t.field, id, strings.TrimSuffix(t.list, "s"), qty, t.price, price, prod.ID, price*qty)
	}
}
