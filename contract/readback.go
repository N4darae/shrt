package contract

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var normalisedClaim = regexp.MustCompile(`(?i)\b(?:lower|upper)[- ]?cased?\b|\bin (?:lower|upper)[- ]?case\b|\bnormali[sz]\w*|\bcase[- ]?fold\w*|\bcanonicali[sz]\w*|\bfolded\b`)

func singleCarrier(m *catalog.Method) *catalog.Field {
	var out *catalog.Field
	for _, fd := range catalog.DescribeMessage(m.Output()).Fields {
		if fd.Kind != "message" || fd.Repeated || fd.MapKey != "" || fd.Name == chain.EnvelopeField() || IsVerdictFieldName(fd.Name) {
			continue
		}
		if out != nil {
			return nil
		}
		out = fd
	}
	return out
}

func (p *Plan) normalised(lib *Library, rpc, field string) bool {
	c, ok := lib.Get(canonicalCall(p.cat, rpc))
	if !ok {
		return false
	}
	note := ""
	if fc := c.Fields[field]; fc != nil {
		note = fc.Note
	}
	if yes, no := claims(note, normalisedClaim, nil); yes && !no {
		return true
	}
	if yes, no := claims(note, trimClaimed, trimDenied); yes && !no {
		return true
	}
	for _, f := range lib.AllFailures(canonicalCall(p.cat, rpc)) {
		if _, unique := uniquenessNoun(f); !unique {
			continue
		}
		if f.Field != field && !mentionsField(f.When, field) {
			continue
		}
		text := strings.Join([]string{f.When, f.Message, c.Summary, note}, " ")
		if ignoresCase(f, text) || trimsSpace(f, text) {
			return true
		}
	}
	return false
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
	for _, e := range st.Expect {
		if e.Path != idPath {
			continue
		}
		if text, ok := e.Equals.(string); ok {
			if src, isRef := refSource(text); isRef && src != st.ID {
				return true
			}
		}
	}
	return false
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
		idPath := p.createdIDPath(prod, pm)
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
		var text strings.Builder
		bodyText(map[string]any(s.Body), &text)
		if !strings.Contains(text.String(), ref) {
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
			if p.normalised(lib, src.Call, sf.Name) {
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
	read := e.readStep(p.freeStepID(defaultID(e.reader.Name)+"_after_"+prod.ID),
		fmt.Sprintf("the %s %s stored, read back: every field it sent, as sent.", e.carrier, prod.ID), "${"+prod.ID+"."+idPath+"}")
	p.assertEcho(read)
	return read
}

func (p *Plan) caseFields(lib *Library, st *chain.Step, m *catalog.Method) []string {
	out := []string{}
	for _, f := range catalog.DescribeMessage(m.Input()).Fields {
		if f.Kind != "string" || f.Repeated || f.MapKey != "" || len(f.EnumValues) > 0 || IsEntityIDField(f.Name) || idLike(f.Name) || isIdempotencyField(f) {
			continue
		}
		key, ok := namecase.LookupKey(st.Body, f.Name)
		if !ok {
			continue
		}
		v, isText := st.Body[key].(string)
		if !isText || strings.TrimSpace(v) == "" || wholeReference(v) || refersToStep(v) || swapLiteralCase(v) == v {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (p *Plan) mixedCaseProbe(lib *Library, st *chain.Step) []*chain.Step {
	m, err := p.cat.Lookup(st.Call)
	if err != nil {
		return nil
	}
	idPath := p.createdIDPath(st, m)
	carrier := singleCarrier(m)
	if idPath == "" || carrier == nil {
		return nil
	}
	if _, ok := p.readerMatching(lib, st, idPath, false); !ok {
		return nil
	}
	fields := p.caseFields(lib, st, m)
	if len(fields) == 0 {
		return nil
	}
	probe := p.probeCopy(lib, st, "mixed_case")
	renameStepRefs(probe, st.ID, probe.ID)
	probe.Expect = SuccessExpectation(m)
	for _, key := range fields {
		v, _ := probe.Body[key].(string)
		probe.Body[key] = swapLiteralCase(v)
		if fieldByName(carrier.Fields, key) != nil && !p.normalised(lib, st.Call, key) {
			probe.Expect = append(probe.Expect, chain.Expectation{Path: carrier.Name + "." + key, Equals: "${steps." + probe.ID + ".request." + key + "}"})
		}
	}
	probe.Description = fmt.Sprintf("as %s, but %s with the letters' case swapped: accepted, and echoed and stored with "+
		"that case, unless the contract says the backend normalises it.", st.ID, strings.Join(fields, ", "))
	out := []*chain.Step{probe}
	if read := p.readBackStep(lib, probe, idPath); read != nil {
		out = append(out, read)
	}
	return out
}

var normalisedWord = regexp.MustCompile(`(?i)\bnormali[sz]\w*|\bcanonicali[sz]\w*`)

func (p *Plan) trimmed(lib *Library, rpc, field string) bool {
	c, ok := lib.Get(canonicalCall(p.cat, rpc))
	if !ok {
		return false
	}
	note := ""
	if fc := c.Fields[field]; fc != nil {
		note = fc.Note
	}
	for _, re := range []*regexp.Regexp{trimClaimed, normalisedWord} {
		denied := trimDenied
		if re != trimClaimed {
			denied = nil
		}
		if yes, no := claims(note, re, denied); yes && !no {
			return true
		}
	}
	for _, f := range lib.AllFailures(canonicalCall(p.cat, rpc)) {
		if _, unique := uniquenessNoun(f); !unique || (f.Field != field && !mentionsField(f.When, field)) {
			continue
		}
		if trimsSpace(f, strings.Join([]string{f.When, f.Message, c.Summary, note}, " ")) {
			return true
		}
	}
	return false
}

func (p *Plan) keptUntrimmed(lib *Library, rpc, field string) bool {
	c, ok := lib.Get(canonicalCall(p.cat, rpc))
	if !ok {
		return false
	}
	if fc := c.Fields[field]; fc != nil {
		if _, no := claims(fc.Note, trimClaimed, trimDenied); no {
			return true
		}
	}
	for _, f := range lib.AllFailures(canonicalCall(p.cat, rpc)) {
		if _, unique := uniquenessNoun(f); !unique || (f.Field != field && !mentionsField(f.When, field)) {
			continue
		}
		if f.Unique != nil && f.Unique.Trim != nil && !*f.Unique.Trim {
			return true
		}
	}
	return false
}

func (p *Plan) paddedFields(lib *Library, st *chain.Step, m *catalog.Method) (padded, skipped []string) {
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
		if !isFreeText(f.Name) && !p.keptUntrimmed(lib, st.Call, key) {
			continue
		}
		if p.trimmed(lib, st.Call, key) {
			skipped = append(skipped, key)
			continue
		}
		padded = append(padded, key)
	}
	sort.Strings(padded)
	sort.Strings(skipped)
	return padded, skipped
}

func (p *Plan) paddedTextProbe(lib *Library, st *chain.Step) []*chain.Step {
	m, err := p.cat.Lookup(st.Call)
	if err != nil {
		return nil
	}
	idPath := p.createdIDPath(st, m)
	carrier := singleCarrier(m)
	if idPath == "" || carrier == nil {
		return nil
	}
	if _, ok := p.readerMatching(lib, st, idPath, false); !ok {
		return nil
	}
	fields, skipped := p.paddedFields(lib, st, m)
	if len(skipped) > 0 {
		p.note("step %s: %s %s not padded with spaces, since the contract says the backend trims or normalises %s", st.ID,
			strings.Join(skipped, ", "), pluralVerb(len(skipped), "is", "are"), pluralVerb(len(skipped), "it", "them"))
	}
	if len(fields) == 0 {
		return nil
	}
	probe := p.probeCopy(lib, st, "padded_text")
	renameStepRefs(probe, st.ID, probe.ID)
	probe.Expect = SuccessExpectation(m)
	for _, key := range fields {
		v, _ := probe.Body[key].(string)
		probe.Body[key] = "  " + v + "  "
		if fieldByName(carrier.Fields, key) != nil {
			probe.Expect = append(probe.Expect, chain.Expectation{Path: carrier.Name + "." + key, Equals: "${steps." + probe.ID + ".request." + key + "}"})
		}
	}
	probe.Description = fmt.Sprintf("as %s, but %s with two spaces before and after: accepted, and echoed and stored "+
		"with the spaces, since the contract does not say the backend trims %s.", st.ID, strings.Join(fields, ", "), pluralVerb(len(fields), "it", "them"))
	out := []*chain.Step{probe}
	if read := p.readBackStep(lib, probe, idPath); read != nil {
		out = append(out, read)
	}
	return out
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
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) {
			continue
		}
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
		if _, known := lib.Get(canonicalCall(p.cat, st.Call)); !known {
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		if idPath := p.createdIDPath(st, m); idPath != "" && isTarget(st) && !p.readsCreated(st, idPath) {
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
