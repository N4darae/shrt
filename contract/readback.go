package contract

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var normalisedClaim = lazyRegexp(`(?i)\b(?:lower|upper)[- ]?cased?\b|\bin (?:lower|upper)[- ]?case\b|\bnormali[sz]\w*|\bcase[- ]?fold\w*|\bcanonicali[sz]\w*|\bfolded\b`)

func carriersOf(m *catalog.Method) []*catalog.Field {
	return slices.DeleteFunc(catalog.DescribeMessage(m.Output()).Fields, func(fd *catalog.Field) bool {
		return fd.Kind != "message" || fd.Repeated || IsVerdictFieldName(fd.Name)
	})
}

func singleCarrier(m *catalog.Method) *catalog.Field {
	if carriers := carriersOf(m); len(carriers) == 1 {
		return carriers[0]
	}
	return nil
}

func (p *Plan) uniqueOnField(lib *Library, rpc, field string) (c *RPCContract, note string, unique []Failure, ok bool) {
	rpc = canonicalCall(p.cat, rpc)
	if c, ok = lib.Get(rpc); !ok {
		return nil, "", nil, false
	}
	if fc := c.Fields[field]; fc != nil {
		note = fc.Note
	}
	for _, f := range lib.AllFailures(rpc) {
		if _, isUnique := uniquenessNoun(f); isUnique && (f.Field == field || mentionsField(f.When, field)) {
			unique = append(unique, f)
		}
	}
	return c, note, unique, true
}

func (p *Plan) normalised(lib *Library, rpc, field string, word *regexp.Regexp, caseToo bool) bool {
	c, note, unique, ok := p.uniqueOnField(lib, rpc, field)
	if !ok {
		return false
	}
	if yes, no := claims(note, word, nil); yes && !no {
		return true
	}
	if yes, no := claims(note, trimClaimed(), trimDenied()); yes && !no {
		return true
	}
	return slices.ContainsFunc(unique, func(f Failure) bool {
		text := strings.Join([]string{f.When, f.Message, c.Summary, note}, " ")
		return (caseToo && ignoresCase(f, text)) || trimsSpace(f, text)
	})
}

func meaningfulValue(v any) bool {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t) != "" && !isNumericZero(t)
	case bool:
		return t
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	}
	return false
}

func (p *Plan) replaysAnother(st *chain.Step, idPath string) bool {
	return slices.ContainsFunc(st.Expect, func(e chain.Expectation) bool {
		text, ok := e.Equals.(string)
		if !ok || e.Path != idPath {
			return false
		}
		src, isRef := refSource(text)
		return isRef && src != st.ID
	})
}

func (p *Plan) readProducer(r *chain.Step, carrier *catalog.Field) (*chain.Step, string) {
	for _, sf := range carrier.Fields {
		if !IsEntityIDField(sf.Name) || sf.Kind != "string" || sf.Repeated {
			continue
		}
		key, ok := namecase.LookupKey(r.Body, sf.Name)
		if !ok {
			continue
		}
		text, _ := r.Body[key].(string)
		src, isRef := refSource(text)
		if !isRef {
			continue
		}
		prod := p.stepByID(src)
		if prod == nil || chain.IsReadOnlyCall(prod.Call) || isRefusalStep(prod) || prod.AllowFail {
			continue
		}
		pm, err := p.cat.Lookup(prod.Call)
		if err != nil {
			continue
		}
		idPath := p.createdIDPath(pm)
		if idPath == "" || text != "${"+prod.ID+"."+idPath+"}" || p.replaysAnother(prod, idPath) {
			continue
		}
		return prod, text
	}
	return nil, ""
}

func (p *Plan) latestWriter(prod, r *chain.Step, ref, key string) *chain.Step {
	out := prod
	started := false
	for _, s := range p.Chain.Steps {
		if s == r {
			break
		}
		if s == prod {
			started = true
			continue
		}
		if !started || chain.IsReadOnlyCall(s.Call) || isRefusalStep(s) || s.AllowFail || s.SkipAuth {
			continue
		}
		if !strings.Contains(bodyText(map[string]any(s.Body)), ref) {
			continue
		}
		if _, sends := namecase.LookupKey(s.Body, key); sends {
			out = s
		}
	}
	return out
}

func (p *Plan) assertReadBack(lib *Library) []string {
	asserted := []string{}
	for _, r := range p.Chain.Steps {
		if !chain.IsReadOnlyCall(r.Call) || isRefusalStep(r) || r.AllowFail || r.SkipAuth || r.Auth == "invalid" {
			continue
		}
		m, err := p.cat.Lookup(r.Call)
		if err != nil {
			continue
		}
		carrier := singleCarrier(m)
		if carrier == nil {
			continue
		}
		prod, ref := p.readProducer(r, carrier)
		if prod == nil {
			continue
		}
		base := carrier.Name
		if m.ServerStreaming {
			base = catalog.StreamMessages + ".0." + base
		}
		added := false
		for _, sf := range carrier.Fields {
			if sf.Repeated && sf.Kind == "message" && sf.MapKey == "" && len(sf.Fields) > 0 {
				added = p.assertItemsReadBack(r, prod, ref, base, sf) || added
				continue
			}
			if sf.Kind == "message" || sf.Repeated || sf.MapKey != "" || IsEntityIDField(sf.Name) {
				continue
			}
			path := base + "." + sf.Name
			if hasExpectOn(r, path) {
				continue
			}
			src := p.latestWriter(prod, r, ref, sf.Name)
			key, ok := namecase.LookupKey(src.Body, sf.Name)
			if !ok || !meaningfulValue(src.Body[key]) {
				continue
			}
			want := "${steps." + src.ID + ".request." + key + "}"
			if p.normalised(lib, src.Call, sf.Name, normalisedClaim(), true) {
				sm, err := p.cat.Lookup(src.Call)
				if err != nil {
					continue
				}
				echo := singleCarrier(sm)
				if echo == nil || fieldByName(echo.Fields, sf.Name) == nil {
					continue
				}
				want = "${" + src.ID + "." + echo.Name + "." + sf.Name + "}"
			}
			r.Expect = append(r.Expect, chain.Expectation{Path: path, Equals: want})
			added = true
		}
		if added {
			asserted = append(asserted, r.ID)
		}
	}
	return asserted
}

func (p *Plan) assertItemsReadBack(r, prod *chain.Step, ref, carrier string, list *catalog.Field) bool {
	src := p.latestWriter(prod, r, ref, list.Name)
	key, ok := namecase.LookupKey(src.Body, list.Name)
	if !ok {
		return false
	}
	items, _ := src.Body[key].([]any)
	if len(items) == 0 {
		return false
	}
	base := carrier + "." + list.Name
	for _, e := range r.Expect {
		if e.Path == base || strings.HasPrefix(e.Path, base+".") {
			return false
		}
	}
	added := false
	for i, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return added
		}
		for _, sub := range list.Fields {
			if sub.Kind == "message" || sub.Repeated || sub.MapKey != "" {
				continue
			}
			k, ok := namecase.LookupKey(item, sub.Name)
			if !ok || !meaningfulValue(item[k]) {
				continue
			}
			r.Expect = append(r.Expect, chain.Expectation{
				Path:   fmt.Sprintf("%s.%d.%s", base, i, sub.Name),
				Equals: fmt.Sprintf("${steps.%s.request.%s.%d.%s}", src.ID, key, i, k),
			})
			added = true
		}
	}
	if added {
		r.Expect = append(r.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", base, len(items)), Exists: boolPtr(false)})
	}
	return added
}

func (p *Plan) readsCreated(prod *chain.Step, idPath string) bool {
	ref := "${" + prod.ID + "." + idPath + "}"
	for _, s := range p.Chain.Steps {
		if !chain.IsReadOnlyCall(s.Call) || isRefusalStep(s) {
			continue
		}
		for _, v := range s.Body {
			if text, ok := v.(string); ok && text == ref {
				return true
			}
		}
	}
	return false
}

func (p *Plan) readBackStep(lib *Library, prod *chain.Step, idPath string) *chain.Step {
	e, ok := p.readerMatching(lib, prod, idPath, false)
	if !ok {
		return nil
	}
	read := e.readStep(p.freeStepID(chain.SnakeCase(e.reader.Name)+"_after_"+prod.ID),
		fmt.Sprintf("the %s %s stored, read back: every field it sent, as sent.", e.carrier, prod.ID), "${"+prod.ID+"."+idPath+"}")
	p.assertEcho(read)
	return read
}

func plainText(st *chain.Step, m *catalog.Method, each func(name, key, v string)) {
	for _, f := range catalog.DescribeMessage(m.Input()).Fields {
		if f.Kind != "string" || f.Repeated || f.MapKey != "" || len(f.EnumValues) > 0 || IsEntityIDField(f.Name) || idLike(f.Name) || isIdempotencyField(f) {
			continue
		}
		key, ok := namecase.LookupKey(st.Body, f.Name)
		if !ok {
			continue
		}
		v, isText := st.Body[key].(string)
		if !isText || strings.TrimSpace(v) == "" || wholeReference(v) || refersToStep(v) {
			continue
		}
		each(f.Name, key, v)
	}
}

func (p *Plan) mixedCaseProbe(lib *Library, st *chain.Step) []*chain.Step {
	pick := func(m *catalog.Method) []string {
		out := []string{}
		plainText(st, m, func(_, key, v string) {
			if swapLiteralCase(v) != v {
				out = append(out, key)
			}
		})
		sort.Strings(out)
		return out
	}
	echoed := func(key string) bool { return !p.normalised(lib, st.Call, key, normalisedClaim(), true) }
	return p.retypedTextProbe(lib, st, "mixed_case", pick, swapLiteralCase, echoed, func(fields []string) string {
		return fmt.Sprintf("as %s, but %s with the letters' case swapped: accepted, and echoed and stored with "+
			"that case, unless the contract says the backend normalises it.", st.ID, strings.Join(fields, ", "))
	})
}

func (p *Plan) retypedTextProbe(lib *Library, st *chain.Step, tag string, pick func(*catalog.Method) []string, twist func(string) string, echoed func(string) bool, describe func([]string) string) []*chain.Step {
	m, err := p.cat.Lookup(st.Call)
	if err != nil {
		return nil
	}
	idPath := p.createdIDPath(m)
	carrier := singleCarrier(m)
	if idPath == "" || carrier == nil {
		return nil
	}
	if _, ok := p.readerMatching(lib, st, idPath, false); !ok {
		return nil
	}
	fields := pick(m)
	if len(fields) == 0 {
		return nil
	}
	probe := p.probeCopy(lib, st, tag)
	renameStepRefs(probe, st.ID, probe.ID)
	probe.Expect = SuccessExpectation(m)
	for _, key := range fields {
		v, _ := probe.Body[key].(string)
		probe.Body[key] = twist(v)
		if fieldByName(carrier.Fields, key) != nil && echoed(key) {
			probe.Expect = append(probe.Expect, chain.Expectation{Path: carrier.Name + "." + key, Equals: "${steps." + probe.ID + ".request." + key + "}"})
		}
	}
	probe.Description = describe(fields)
	out := []*chain.Step{probe}
	if read := p.readBackStep(lib, probe, idPath); read != nil {
		out = append(out, read)
	}
	return out
}

var normalisedWord = lazyRegexp(`(?i)\bnormali[sz]\w*|\bcanonicali[sz]\w*`)

func (p *Plan) keptUntrimmed(lib *Library, rpc, field string) bool {
	_, note, unique, ok := p.uniqueOnField(lib, rpc, field)
	if !ok {
		return false
	}
	if _, no := claims(note, trimClaimed(), trimDenied()); no {
		return true
	}
	return slices.ContainsFunc(unique, func(f Failure) bool { return f.Unique != nil && f.Unique.Trim != nil && !*f.Unique.Trim })
}

func (p *Plan) paddedTextProbe(lib *Library, st *chain.Step) []*chain.Step {
	pick := func(m *catalog.Method) []string {
		var padded, skipped []string
		plainText(st, m, func(name, key, _ string) {
			if !isFreeText(name) && !p.keptUntrimmed(lib, st.Call, key) {
				return
			}
			if p.normalised(lib, st.Call, key, normalisedWord(), false) {
				skipped = append(skipped, key)
			} else {
				padded = append(padded, key)
			}
		})
		sort.Strings(padded)
		sort.Strings(skipped)
		if len(skipped) > 0 {
			p.note("step %s: %s %s not padded with spaces, since the contract says the backend trims or normalises %s", st.ID,
				strings.Join(skipped, ", "), pluralVerb(len(skipped), "is", "are"), pluralVerb(len(skipped), "it", "them"))
		}
		return padded
	}
	pad := func(v string) string { return "  " + v + "  " }
	echoed := func(string) bool { return true }
	return p.retypedTextProbe(lib, st, "padded_text", pick, pad, echoed, func(fields []string) string {
		return fmt.Sprintf("as %s, but %s with two spaces before and after: accepted, and echoed and stored "+
			"with the spaces, since the contract does not say the backend trims %s.", st.ID, strings.Join(fields, ", "), pluralVerb(len(fields), "it", "them"))
	})
}

func (p *Plan) probeReadBack(lib *Library, isTarget func(*chain.Step) bool) {
	subjects := []*chain.Step{}
	seen := map[string]bool{}
	add := func(st *chain.Step) {
		if st != nil && !seen[st.ID] && !chain.IsReadOnlyCall(st.Call) && !isRefusalStep(st) && !p.isLogin(st.Call) {
			seen[st.ID] = true
			subjects = append(subjects, st)
		}
	}
	for st := range p.targets(isTarget) {
		if !chain.IsReadOnlyCall(st.Call) {
			add(st)
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		if carrier := singleCarrier(m); carrier != nil {
			prod, _ := p.readProducer(st, carrier)
			add(prod)
		}
	}
	added, probes, padded := []string{}, []string{}, []string{}
	for _, st := range subjects {
		_, m, ok := p.contractOf(lib, canonicalCall(p.cat, st.Call))
		if !ok {
			continue
		}
		if idPath := p.createdIDPath(m); idPath != "" && isTarget(st) && !p.readsCreated(st, idPath) {
			if read := p.readBackStep(lib, st, idPath); read != nil {
				p.insertAfter(st.ID, read)
				added = append(added, read.ID)
			}
		}
		if steps := p.mixedCaseProbe(lib, st); len(steps) > 0 {
			p.Chain.Steps = append(p.Chain.Steps, steps...)
			probes = append(probes, steps[0].ID)
		}
		if isTarget(st) {
			if steps := p.paddedTextProbe(lib, st); len(steps) > 0 {
				p.Chain.Steps = append(p.Chain.Steps, steps...)
				padded = append(padded, steps[0].ID)
			}
		}
	}
	if len(padded) > 0 {
		p.note("%s %s free text (and text the contract says is not trimmed) with spaces before and after and %s it back, "+
			"so a backend that trims what it stores while echoing the request fails the read; say trimmed in the field's "+
			"note (or unique: {trim: true}) when the backend is meant to trim", strings.Join(padded, ", "),
			pluralVerb(len(padded), "sends", "send"), pluralVerb(len(padded), "reads", "read"))
	}
	if len(added) > 0 {
		p.note("%s %s the created record back right after it was written", strings.Join(added, ", "), pluralVerb(len(added), "reads", "read"))
	}
	if len(probes) > 0 {
		p.note("%s %s the text fields with their letters' case swapped and %s them back, so a backend that lowercases "+
			"or uppercases what it stores or returns fails", strings.Join(probes, ", "), pluralVerb(len(probes), "sends", "send"),
			pluralVerb(len(probes), "reads", "read"))
	}
}

func (p *Plan) noteReadBack(lib *Library) {
	ids := p.assertReadBack(lib)
	if len(ids) == 0 {
		return
	}
	p.note("%s %s each field the write before %s sent equals what it sent, so a backend that stores or returns a "+
		"field changed fails; a field the contract says the backend normalises (unique ignoring case or trimmed, or a note "+
		"saying it is stored lowercased or normalised) equals what the write's own response echoed instead",
		strings.Join(clipList(ids, 6), ", "), pluralVerb(len(ids), "asserts", "assert"), pluralVerb(len(ids), "it", "them"))
}
