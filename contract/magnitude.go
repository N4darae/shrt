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

func (p *Plan) probeBoundaries(lib *Library, isTarget func(*chain.Step) bool) {
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
			if quantity {
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
			}
			if !quantity {
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
