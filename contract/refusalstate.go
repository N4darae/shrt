package contract

import (
	"fmt"
	"slices"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/pathmask"
)

const overdrawValue = "100000"

var (
	shortReason  = lazyRegexp(`^(?:Insufficient|NotEnough|OutOf|Exceeds|Exceeded|Over)[A-Z]`)
	shortWhen    = lazyRegexp(`(?i)\binsufficient\b|\bnot enough\b|\bmore than\b|\bexceeds?\b|\bout of stock\b|\bbeyond\b`)
	stepRefToken = lazyRegexp(`\$\{\s*(?:steps\.)?([A-Za-z_][A-Za-z0-9_]*)\.(?:response\.)?([A-Za-z0-9_.]+)\s*\}`)
)

func isQuantityName(name string) bool {
	return nameHasWord(name, "qty", "quantity", "count", "units", "amount")
}

func nameHasWord(name string, words ...string) bool {
	return slices.ContainsFunc(namecase.Words(name), func(w string) bool { return slices.Contains(words, strings.ToLower(w)) })
}

func quantityPaths(body map[string]any, fields []*catalog.Field) []string {
	var walk func(m map[string]any, fs []*catalog.Field, path string) []string
	walk = func(m map[string]any, fs []*catalog.Field, path string) []string {
		for _, f := range fs {
			key, ok := namecase.LookupKey(m, f.Name)
			if !ok || f.MapKey != "" {
				continue
			}
			at := pathmask.Join(path, f.Name)
			switch v := m[key].(type) {
			case []any:
				if len(f.Fields) == 0 || len(v) == 0 {
					continue
				}
				paths := []string{}
				indexes := []int{0}
				if len(v) > 1 {
					indexes = append(indexes, len(v)-1)
				}
				for _, i := range indexes {
					item, ok := v[i].(map[string]any)
					if !ok {
						continue
					}
					if got := walk(item, f.Fields, fmt.Sprintf("%s.%d", at, i)); len(got) > 0 && !slices.Contains(paths, got[0]) {
						paths = append(paths, got[0])
					}
				}
				if len(paths) > 0 {
					return paths
				}
			case map[string]any:
				if got := walk(v, f.Fields, at); len(got) > 0 {
					return got
				}
			default:
				if !f.Repeated && chain.IsNumericKind(f.Kind) && isQuantityName(f.Name) {
					if _, isNum := numericValue(v); isNum {
						return []string{at}
					}
				}
			}
		}
		return nil
	}
	return walk(body, fields, "")
}

func (p *Plan) probeInsufficiency(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) {
			continue
		}
		if m, f, ok := p.shortageFailure(lib, st); ok {
			p.addInsufficiencyProbe(lib, st, m, f)
		}
	}
}

func (p *Plan) addInsufficiencyProbe(lib *Library, st *chain.Step, m *catalog.Method, f Failure) {
	name := "over_quantity"
	if f.Reason != "" {
		name = defaultID(f.Reason)
	}
	expect, _ := refusalExpectations(m, f, false)
	source, paths := p.shortagePaths(st, m)
	if len(paths) == 0 {
		p.gap("step %s: the contract declares %s, but no quantity field (qty, quantity, count, amount) was found in its "+
			"body or in a step it reads, so no refused attempt was planned: write one that asks for more than there is, "+
			"and read the state it would have changed before and after it", st.ID, f.Label())
		return
	}
	ids, unknown := []string{}, []string{}
	for i, path := range paths {
		suffix := name
		if i > 0 {
			suffix = name + "_last_item"
		}
		refused := probeStep(st, p.freeStepID(st.ID+"_"+suffix))
		refused.Expect = append([]chain.Expectation{}, expect...)
		body := refused.Body
		var short *chain.Step
		if source != nil {
			short = probeStep(source, p.freeStepID(source.ID+"_for_"+suffix))
			body = short.Body
		}
		qty, derived := p.shortageQuantity(lib, body, path)
		setBodyPath(body, path, qty)
		set := fmt.Sprintf("%s asks for %s, one more than the stock this chain added", path, qty)
		if !derived {
			set = fmt.Sprintf("%s asks for %s, more than any fixture holds", path, qty)
		}
		where := set
		if short == nil {
			if !derived {
				unknown = append(unknown, refused.ID)
			}
			p.freshen(lib, refused)
		} else {
			short.Description = fmt.Sprintf("as %s, but %s, for %s.", source.ID, set, refused.ID)
			if !derived {
				unknown = append(unknown, short.ID)
			}
			p.freshen(lib, short)
			renameStepRefs(short, source.ID, short.ID)
			renameStepRefs(refused, source.ID, short.ID)
			p.Chain.Steps = append(p.Chain.Steps, short)
			where = fmt.Sprintf("%s (%s)", short.ID, set)
		}
		refused.Description = fmt.Sprintf("refused with %s (%s), and nothing it would have changed moves.", f.Label(), strings.TrimSpace(f.When))
		p.Chain.Steps = append(p.Chain.Steps, p.guardUnchanged(lib, []*chain.Step{refused}, refused.ID)...)
		ids = append(ids, fmt.Sprintf("%s via %s", refused.ID, where))
	}
	p.note("step %s: %s expect %s; the reads around each assert that every entity it touches is unchanged, so a "+
		"backend that refuses but still applies part of the write fails. The shortage sits on the first item and, in "+
		"a second probe, on the last, since a backend checking only the first item confirms the second",
		st.ID, strings.Join(ids, " and "), f.Label())
	p.noteShortageQuantity(st, unknown)
}

func (p *Plan) shortagePaths(st *chain.Step, m *catalog.Method) (*chain.Step, []string) {
	if got := quantityPaths(st.Body, catalog.DescribeMessage(m.Input()).Fields); len(got) > 0 {
		return nil, got
	}
	for _, src := range referencedSteps(st.Body) {
		prod := p.stepByID(src)
		if prod == nil || chain.IsReadOnlyCall(prod.Call) {
			continue
		}
		pm, err := p.cat.Lookup(prod.Call)
		if err != nil {
			continue
		}
		if got := quantityPaths(prod.Body, catalog.DescribeMessage(pm.Input()).Fields); len(got) > 0 {
			return prod, got
		}
	}
	return nil, nil
}

func (p *Plan) shortageFailure(lib *Library, st *chain.Step) (*catalog.Method, Failure, bool) {
	_, m, ok := p.contractOf(lib, st.Call)
	if !ok || chain.IsReadOnlyCall(st.Call) {
		return nil, Failure{}, false
	}
	for _, f := range lib.AllFailures(st.Call) {
		if shortReason().MatchString(f.Reason) || shortWhen().MatchString(f.When) {
			return m, f, true
		}
	}
	return nil, Failure{}, false
}

func (p *Plan) uniqueFields(lib *Library, st *chain.Step) map[string]bool {
	unique := map[string]bool{}
	if c, ok := lib.Get(st.Call); ok {
		for _, f := range lib.AllFailures(st.Call) {
			if noun, isUnique := uniquenessNoun(f); isUnique {
				if field := p.uniqueField(st, c, f, noun); field != "" {
					unique[stripIndexes(field)] = true
				}
			}
		}
	}
	return unique
}

func (p *Plan) freshen(lib *Library, st *chain.Step) {
	m, err := p.cat.Lookup(st.Call)
	if err != nil {
		return
	}
	fields := catalog.DescribeMessage(m.Input()).Fields
	marker := strings.TrimPrefix(st.ID, defaultID(m.Name)+"_")
	for path := range p.uniqueFields(lib, st) {
		v, _ := bodyValue(st.Body, path)
		kind := ""
		if fd, ok := catalog.FieldAt(fields, chain.SplitPath(path)); ok && fd != nil {
			kind = fd.Kind
		}
		if next, ok := otherValue(v, kind, marker); ok {
			setBodyPath(st.Body, path, next)
		}
	}
	for _, f := range fields {
		if isIdempotencyField(f) {
			if key, ok := namecase.LookupKey(st.Body, f.Name); ok {
				st.Body[key] = "${uuid}"
			}
		}
	}
}

func isIdempotencyField(f *catalog.Field) bool {
	folded := namecase.Fold(f.Name)
	return f.Kind == "string" && !f.Repeated && slices.ContainsFunc([]string{"idempotency", "idempotent", "dedup", "requestid", "clientrequest", "clienttoken"}, func(w string) bool { return strings.Contains(folded, w) })
}

func referencedSteps(v any) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, ref := range allStepRefs(v) {
		if !seen[ref[0]] {
			seen[ref[0]] = true
			out = append(out, ref[0])
		}
	}
	return out
}

func renameStepRefs(st *chain.Step, from, to string) { retarget(st, map[string]string{from: to}) }

type entityRead struct {
	producer *chain.Step
	idPath   string
	reader   *catalog.Method
	contract *RPCContract
	field    string
	carrier  string
	scalars  []string
}

func (e entityRead) asIn(id string) []chain.Expectation {
	var out []chain.Expectation
	for _, name := range e.scalars {
		path := e.carrier + "." + name
		out = append(out, chain.Expectation{Path: path, Equals: "${" + id + "." + path + "}"})
	}
	return out
}

func (p *Plan) entitiesOf(lib *Library, st *chain.Step) []entityRead {
	out := []entityRead{}
	seen := map[string]bool{}
	var visit func(body map[string]any, depth int)
	visit = func(body map[string]any, depth int) {
		for _, m := range allStepRefs(body) {
			prod := p.stepByID(m[0])
			if prod == nil || chain.IsReadOnlyCall(prod.Call) || seen[m[0]] {
				continue
			}
			seen[m[0]] = true
			if e, ok := p.readerMatching(lib, prod, m[1], true); ok {
				out = append(out, e)
			}
			if depth < 1 {
				visit(prod.Body, depth+1)
			}
		}
	}
	visit(st.Body, 0)
	return out
}

func allStepRefs(v any) [][2]string {
	out := [][2]string{}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			for _, m := range stepRefToken().FindAllStringSubmatch(t, -1) {
				if m[1] == "vars" || m[1] == "env" || m[1] == "exports" || strings.HasPrefix(m[2], "request.") {
					continue
				}
				out = append(out, [2]string{m[1], m[2]})
			}
		case map[string]any:
			for _, k := range sortedKeys(t) {
				walk(t[k])
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	walk(v)
	return out
}

func (e entityRead) readStep(id, description, ref string) *chain.Step {
	body := catalog.ScaffoldWith(e.reader.Input(), catalog.ScaffoldOptions{})
	setBodyPath(body, e.field, ref)
	return &chain.Step{ID: id, Description: description, Call: e.reader.FullName, Auth: e.contract.Auth, Body: body, Expect: SuccessExpectation(e.reader)}
}

func (e entityRead) echoingRead(id, description string) *chain.Step {
	ref := "${" + e.producer.ID + "." + e.idPath + "}"
	read := e.readStep(id, description, ref)
	leaf := chain.PathLeaf(e.idPath)
	if fieldByName(carrierFields(e.reader, e.carrier), leaf) != nil {
		read.Expect = append(read.Expect, chain.Expectation{Path: e.carrier + "." + leaf, Equals: ref})
	}
	return read
}

func (p *Plan) readBase(e entityRead) string {
	base := defaultID(e.reader.Name)
	if pm, err := p.cat.Lookup(e.producer.Call); err == nil {
		if suffix := strings.TrimPrefix(e.producer.ID, defaultID(pm.Name)); isIndexSuffix(suffix) {
			base += suffix
		}
	}
	return base
}

func (p *Plan) readerMatching(lib *Library, prod *chain.Step, idPath string, needScalars bool) (entityRead, bool) {
	pm, err := p.cat.Lookup(prod.Call)
	if err != nil {
		return entityRead{}, false
	}
	carrierMsg := ""
	head := chain.SplitPath(idPath)[0]
	if f := fieldByName(catalog.DescribeMessage(pm.Output()).Fields, head); f != nil && f.Kind == "message" && !f.Repeated {
		carrierMsg = f.Message
	}
	rpcs := lib.RPCs()
	for _, rpc := range rpcs {
		if !chain.IsReadOnlyCall(rpc) {
			continue
		}
		c, _ := lib.Get(rpc)
		rm, err := p.cat.Lookup(rpc)
		if err != nil || rm.Streaming() {
			continue
		}
		for _, name := range sortedKeys(c.Fields) {
			ref, err := ParseRef(c.Fields[name].From)
			if err != nil || canonicalCall(p.cat, ref.RPC) != pm.FullName || ref.Path != idPath {
				continue
			}
			for _, out := range catalog.DescribeMessage(rm.Output()).Fields {
				if out.Kind != "message" || out.Repeated || out.MapKey != "" || (carrierMsg != "" && out.Message != carrierMsg) {
					continue
				}
				scalars := []string{}
				for _, sf := range out.Fields {
					if !sf.Repeated && sf.MapKey == "" && (chain.IsNumericKind(sf.Kind) || len(sf.EnumValues) > 0) {
						scalars = append(scalars, sf.Name)
					}
				}
				if len(scalars) == 0 && needScalars {
					continue
				}
				return entityRead{producer: prod, idPath: idPath, reader: rm, contract: c, field: name, carrier: out.Name, scalars: scalars}, true
			}
		}
	}
	return entityRead{}, false
}

func (p *Plan) guardUnchanged(lib *Library, refused []*chain.Step, label string) []*chain.Step {
	entities := []entityRead{}
	seen := map[string]bool{}
	for _, r := range refused {
		for _, e := range p.entitiesOf(lib, r) {
			if !seen[e.producer.ID] {
				seen[e.producer.ID] = true
				entities = append(entities, e)
			}
		}
	}
	if len(entities) == 0 && p.allRefusedCreates(refused) {
		p.noteRefusedCreate(refused[0])
		return refused
	}
	if len(entities) == 0 {
		if readers := p.textOnlyReaders(lib, refused[0], ""); len(readers) > 0 {
			p.note("step %s: %s reads what it touches but answers only text, so no read before and after the refusal "+
				"is added; the expected failure is its check", refused[0].ID, strings.Join(readers, ", "))
			return refused
		}
		p.gap("step %s: no read rpc in the contracts takes the id of anything it touches, so nothing proves the "+
			"refusal changed nothing: read the state it would have written after it", refused[0].ID)
		return refused
	}
	before, after := []*chain.Step{}, []*chain.Step{}
	reserved := map[string]bool{}
	for _, e := range entities {
		base := p.readBase(e)
		beforeID := p.freeProbeID(base+"_before_"+label, reserved)
		afterID := p.freeProbeID(base+"_after_"+label, reserved)
		read := e.echoingRead(beforeID, fmt.Sprintf("the %s as it stands before %s.", e.carrier, label))
		check := copyStep(read, afterID)
		check.Description = fmt.Sprintf("the %s after %s is unchanged: %s read as in %s.", e.carrier, label, strings.Join(e.scalars, ", "), beforeID)
		check.Expect = append(check.Expect, e.asIn(beforeID)...)
		before = append(before, read)
		after = append(after, check)
	}
	return append(append(before, refused...), after...)
}

func isIndexSuffix(s string) bool {
	return s == "" || (strings.HasPrefix(s, "_") && chain.IsDigits(s[1:]))
}

func (p *Plan) freeProbeID(base string, reserved map[string]bool) string {
	id := base
	for i := 2; ; i++ {
		if _, exists := p.Chain.Step(id); !exists && !p.reserved[id] && !reserved[id] {
			reserved[id] = true
			return id
		}
		id = fmt.Sprintf("%s_%d", base, i)
	}
}

func carrierFields(m *catalog.Method, carrier string) []*catalog.Field {
	if f := fieldByName(catalog.DescribeMessage(m.Output()).Fields, carrier); f != nil {
		return f.Fields
	}
	return nil
}
