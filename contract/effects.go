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
	increaseClause = regexp.MustCompile(`(?i)\b(?:increases?|increments?|raises?|adds?)\s+([^.;]+?)\s+by\s+(?:the\s+)?([A-Za-z_][A-Za-z0-9_]*)`)
	reserveClause  = regexp.MustCompile(`(?i)\b(?:reserves?|takes?|deducts?|decrements?|consumes?|removes?|decreases?|allocates?|subtracts?)\s+([^.;]+?)\s+(?:for|from|on|of|across)\s+(?:every|each|all)\s+(?:of\s+(?:its|the)\s+|the\s+|its\s+)?([A-Za-z_]+)`)
	perItemClause  = regexp.MustCompile(`(?i)\bone\s+([A-Za-z][A-Za-z0-9]*)\s+per\s+(?:line|item|entry|row)\b`)
	untouchedWords = regexp.MustCompile(`(?i)\b(?:does not|doesn't|do not|never)\s+(?:touch|change|move|affect|alter|modify|reserve)\w*\s+([^.;,]+)`)
	startsAtZero   = regexp.MustCompile(`(?i)\b(?:zero|no)\s+([a-z]+)`)
	sumWord        = regexp.MustCompile(`(?i)\bsum\b|\btotal of\b`)
	priceWord      = regexp.MustCompile(`(?i)\bpric(?:e|ed|es|ing)\b`)
	plainWord      = regexp.MustCompile(`[A-Za-z]+`)
)

type stockRule struct {
	rpc       string
	sentence  string
	entityRPC string
	idPath    string
	idField   string
	qtyField  string
	moved     string
	words     []string
}

type batchRule struct {
	rpc     string
	list    string
	results string
	stock   *stockRule
}

type reserveRule struct {
	rpc         string
	sentence    string
	orderField  string
	orderRPC    string
	orderIDPath string
	list        string
	itemID      string
	itemQty     string
	stock       *stockRule
}

type totalRule struct {
	rpc      string
	sentence string
	carrier  string
	field    string
	list     string
	itemID   string
	itemQty  string
	entity   string
	price    string
}

type effectRules struct {
	increase map[string]*stockRule
	batch    map[string]*batchRule
	reserve  map[string]*reserveRule
	total    map[string]*totalRule
	byEntity map[string]*stockRule
	orders   map[string]bool
}

var effectStopWords = map[string]bool{"the": true, "its": true, "their": true, "and": true, "for": true, "with": true, "from": true, "into": true, "onto": true}

func contentWords(text string, drop []string) []string {
	skip := map[string]bool{}
	for _, d := range drop {
		skip[strings.ToLower(d)] = true
	}
	out := []string{}
	for _, w := range plainWord.FindAllString(strings.ReplaceAll(text, "'s", ""), -1) {
		w = strings.ToLower(w)
		if len(w) < 3 || effectStopWords[w] || skip[w] || containsString(out, w) {
			continue
		}
		out = append(out, w)
	}
	return out
}

func sharesWord(a, b []string) bool {
	for _, w := range a {
		if containsString(b, w) {
			return true
		}
	}
	return false
}

func (p *Plan) effectRules(lib *Library) *effectRules {
	r := &effectRules{increase: map[string]*stockRule{}, batch: map[string]*batchRule{}, reserve: map[string]*reserveRule{},
		total: map[string]*totalRule{}, byEntity: map[string]*stockRule{}, orders: map[string]bool{}}
	rpcs := lib.RPCs()
	sort.Strings(rpcs)
	for _, rpc := range rpcs {
		if s := p.increaseRule(lib, rpc); s != nil {
			r.increase[rpc] = s
			if r.byEntity[s.entityRPC] == nil {
				r.byEntity[s.entityRPC] = s
			}
		}
	}
	for _, rpc := range rpcs {
		if b := p.batchRuleFor(lib, rpc, r); b != nil {
			r.batch[rpc] = b
		}
		if v := p.reserveRuleFor(lib, rpc, r); v != nil {
			r.reserve[rpc] = v
			r.orders[v.orderRPC] = true
		}
	}
	for _, rpc := range rpcs {
		if t := p.totalRuleFor(lib, rpc, r); t != nil {
			r.total[rpc] = t
			r.orders[rpc] = true
		}
		if c, ok := lib.Get(rpc); ok && !chain.IsReadOnlyCall(rpc) {
			if _, _, _, entity := p.lineItems(rpc, c, ""); r.byEntity[entity] != nil {
				r.orders[rpc] = true
			}
		}
	}
	return r
}

func topFrom(c *RPCContract, cat *catalog.Catalog) map[string]Ref {
	out := map[string]Ref{}
	for _, name := range sortedFieldNames(c.Fields) {
		f := c.Fields[name]
		if f == nil || f.From == "" {
			continue
		}
		if ref, err := ParseRef(f.From); err == nil {
			ref.RPC = canonicalCall(cat, ref.RPC)
			out[name] = ref
		}
	}
	return out
}

func (p *Plan) increaseRule(lib *Library, rpc string) *stockRule {
	c, ok := lib.Get(rpc)
	if !ok || chain.IsReadOnlyCall(rpc) {
		return nil
	}
	m, err := p.cat.Lookup(rpc)
	if err != nil || m.Streaming() {
		return nil
	}
	match := increaseClause.FindStringSubmatch(c.Summary)
	if match == nil {
		return nil
	}
	s := &stockRule{rpc: rpc, sentence: strings.TrimSpace(match[0])}
	in := catalog.DescribeMessage(m.Input()).Fields
	names := map[string]bool{}
	for _, f := range in {
		names[f.Name] = true
		if !f.Repeated && chain.IsNumericKind(f.Kind) && namecase.Fold(f.Name) == namecase.Fold(match[2]) {
			s.qtyField = f.Name
		}
	}
	for name, ref := range topFrom(c, p.cat) {
		if strings.Contains(name, ".") || chain.IsReadOnlyCall(ref.RPC) {
			continue
		}
		s.idField, s.entityRPC, s.idPath = name, ref.RPC, ref.Path
	}
	moved := []string{}
	for _, f := range catalog.DescribeMessage(m.Output()).Fields {
		if f.Repeated || !chain.IsNumericKind(f.Kind) || names[f.Name] || IsVerdictFieldName(f.Name) || idLike(f.Name) {
			continue
		}
		moved = append(moved, f.Name)
	}
	if s.qtyField == "" || s.idField == "" || len(moved) != 1 {
		return nil
	}
	s.moved = moved[0]
	s.words = contentWords(match[1], namecase.Words(chain.SplitPath(s.idPath)[0]))
	return s
}

func (p *Plan) batchRuleFor(lib *Library, rpc string, r *effectRules) *batchRule {
	c, ok := lib.Get(rpc)
	if !ok || chain.IsReadOnlyCall(rpc) {
		return nil
	}
	m, err := p.cat.Lookup(rpc)
	if err != nil || m.Streaming() {
		return nil
	}
	for _, name := range sortedFieldNames(c.Fields) {
		f := c.Fields[name]
		if f == nil || strings.Contains(name, ".") {
			continue
		}
		match := perItemClause.FindStringSubmatch(f.Note)
		if match == nil {
			continue
		}
		var stock *stockRule
		for full, s := range r.increase {
			if strings.HasSuffix(full, "/"+match[1]) {
				stock = s
			}
		}
		if stock == nil {
			continue
		}
		b := &batchRule{rpc: rpc, stock: stock}
		for _, in := range catalog.DescribeMessage(m.Input()).Fields {
			if in.Name != name || !in.Repeated || in.Kind != "message" {
				continue
			}
			has := map[string]bool{}
			for _, sub := range in.Fields {
				has[sub.Name] = true
			}
			if has[stock.idField] && has[stock.qtyField] {
				b.list = in.Name
			}
		}
		for _, out := range catalog.DescribeMessage(m.Output()).Fields {
			if !out.Repeated || out.Kind != "message" {
				continue
			}
			for _, sub := range out.Fields {
				if sub.Name == stock.moved {
					b.results = out.Name
				}
			}
		}
		if b.list != "" && b.results != "" {
			return b
		}
	}
	return nil
}

func (p *Plan) lineItems(rpc string, c *RPCContract, word string) (list, itemID, itemQty, entity string) {
	m, err := p.cat.Lookup(rpc)
	if err != nil {
		return "", "", "", ""
	}
	from := topFrom(c, p.cat)
	for _, in := range catalog.DescribeMessage(m.Input()).Fields {
		if !in.Repeated || in.Kind != "message" {
			continue
		}
		folded := namecase.Fold(in.Name)
		if word != "" && folded != namecase.Fold(word) && folded != namecase.Fold(word)+"s" && folded != namecase.Fold(word)+"es" {
			continue
		}
		id, qty, ent := "", "", ""
		for _, sub := range in.Fields {
			if ref, ok := from[in.Name+"."+sub.Name]; ok && !chain.IsReadOnlyCall(ref.RPC) {
				id, ent = sub.Name, ref.RPC
			}
			if !sub.Repeated && chain.IsNumericKind(sub.Kind) && isQuantityName(sub.Name) {
				qty = sub.Name
			}
		}
		if id != "" && qty != "" {
			return in.Name, id, qty, ent
		}
	}
	return "", "", "", ""
}

func (p *Plan) reserveRuleFor(lib *Library, rpc string, r *effectRules) *reserveRule {
	c, ok := lib.Get(rpc)
	if !ok || chain.IsReadOnlyCall(rpc) {
		return nil
	}
	match := reserveClause.FindStringSubmatch(c.Summary)
	if match == nil {
		return nil
	}
	for name, ref := range topFrom(c, p.cat) {
		if strings.Contains(name, ".") || chain.IsReadOnlyCall(ref.RPC) {
			continue
		}
		oc, ok := lib.Get(ref.RPC)
		if !ok {
			continue
		}
		list, itemID, itemQty, entity := p.lineItems(ref.RPC, oc, match[2])
		stock := r.byEntity[entity]
		if list == "" || stock == nil || !sharesWord(contentWords(match[1], nil), stock.words) {
			continue
		}
		return &reserveRule{rpc: rpc, sentence: strings.TrimSpace(match[0]), orderField: name, orderRPC: ref.RPC, orderIDPath: ref.Path, list: list, itemID: itemID, itemQty: itemQty, stock: stock}
	}
	return nil
}

func (p *Plan) totalRuleFor(lib *Library, rpc string, r *effectRules) *totalRule {
	c, ok := lib.Get(rpc)
	if !ok || chain.IsReadOnlyCall(rpc) {
		return nil
	}
	m, err := p.cat.Lookup(rpc)
	if err != nil {
		return nil
	}
	texts := []string{c.Summary}
	for _, k := range sortedRuleKeys(c.Exports) {
		texts = append(texts, c.Exports[k])
	}
	priced := false
	for _, t := range texts {
		priced = priced || priceWord.MatchString(t)
	}
	if !priced {
		return nil
	}
	list, itemID, itemQty, entity := p.lineItems(rpc, c, "")
	if list == "" {
		return nil
	}
	em, err := p.cat.Lookup(entity)
	if err != nil {
		return nil
	}
	price := ""
	for _, f := range catalog.DescribeMessage(em.Input()).Fields {
		if !f.Repeated && chain.IsNumericKind(f.Kind) && strings.Contains(strings.ToLower(f.Name), "price") {
			price = f.Name
		}
	}
	if price == "" {
		return nil
	}
	for _, out := range catalog.DescribeMessage(m.Output()).Fields {
		if out.Kind != "message" || out.Repeated || out.Name == chain.EnvelopeField() {
			continue
		}
		for _, sf := range out.Fields {
			if sf.Repeated || !chain.IsNumericKind(sf.Kind) {
				continue
			}
			re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(sf.Name) + `\b`)
			for _, t := range texts {
				for _, clause := range clauseBreaks.Split(t, -1) {
					if re.MatchString(clause) && sumWord.MatchString(clause) {
						return &totalRule{rpc: rpc, sentence: strings.TrimSpace(clause), carrier: out.Name, field: sf.Name, list: list,
							itemID: itemID, itemQty: itemQty, entity: entity, price: price}
					}
				}
			}
		}
	}
	return nil
}

type modelLine struct {
	entity string
	qty    int64
	known  bool
}

type modelOrder struct {
	lines    []modelLine
	total    int64
	hasTotal bool
	held     string
	reserved bool
}

type effectModel struct {
	level   map[string]int64
	known   map[string]bool
	stockOf map[string]*stockRule
	orders  map[string]*modelOrder
	alias   map[string]string
	dirty   map[string]bool
	apply   bool
	pending *chain.Step
	waiting []string
	unread  map[string][]string
}

const (
	outcomeRefused = iota
	outcomeSuccess
	outcomeUnknown
)

func effectOutcome(st *chain.Step) int {
	if st.AllowFail {
		return outcomeUnknown
	}
	for _, e := range st.Expect {
		if e.Path == "transport.code" {
			return outcomeRefused
		}
		if chain.IsEnvelopePath(e.Path) && e.NotEqual != nil {
			return outcomeRefused
		}
		if e.Path == chain.EnvelopePath() && e.Equals != nil && fmt.Sprint(e.Equals) != chain.EnvelopeOK() {
			return outcomeRefused
		}
	}
	return outcomeSuccess
}

func itemRefused(st *chain.Step, i int) bool {
	path := chain.ItemEnvelope()
	if path == "" || !strings.Contains(path, "[]") {
		return false
	}
	at := strings.Replace(path, "[]", "."+strconv.Itoa(i), 1)
	for _, e := range st.Expect {
		if e.Path != at {
			continue
		}
		if e.NotEqual != nil || (e.Equals != nil && fmt.Sprint(e.Equals) != chain.EnvelopeOK()) {
			return true
		}
	}
	return false
}

func assertNumber(st *chain.Step, path string, v int64) bool {
	for i, e := range st.Expect {
		if e.Path != path {
			continue
		}
		if e.Equals != nil {
			return false
		}
		if e.Gte != nil || e.Gt != nil || e.Lte != nil || e.Lt != nil || e.NotEmpty || e.Exists != nil {
			st.Expect[i] = chain.Expectation{Path: path, Equals: v}
			return true
		}
	}
	st.Expect = append(st.Expect, chain.Expectation{Path: path, Equals: v})
	return true
}

func stepRefIn(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	src, ok := refSource(s)
	if !ok {
		return ""
	}
	return src
}

func carrierHolding(m *catalog.Method, field string) string {
	for _, out := range catalog.DescribeMessage(m.Output()).Fields {
		if out.Kind != "message" || out.Repeated || out.Name == chain.EnvelopeField() {
			continue
		}
		for _, sf := range out.Fields {
			if sf.Name == field && !sf.Repeated {
				return out.Name
			}
		}
	}
	return ""
}

func (p *Plan) assertEffects(lib *Library) {
	r := p.effectRules(lib)
	if len(r.increase) == 0 && len(r.total) == 0 {
		return
	}
	if _, _, unread := p.effectPass(lib, r, false); len(unread) > 0 {
		p.readAfterMoves(lib, unread)
	}
	asserted, silent, _ := p.effectPass(lib, r, true)
	p.noteEffects(r, asserted, silent)
}

func (p *Plan) readAfterMoves(lib *Library, unread map[string][]string) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		at := st.ID
		reserved := map[string]bool{}
		for _, e := range unread[st.ID] {
			prod := p.stepByID(e)
			s := p.effectRules(lib).byEntity[canonicalCall(p.cat, prod.Call)]
			if s == nil {
				continue
			}
			en, ok := p.readerFor(lib, prod, s.idPath)
			if !ok {
				continue
			}
			base := defaultID(en.reader.Name)
			if pm, err := p.cat.Lookup(prod.Call); err == nil {
				if suffix := strings.TrimPrefix(prod.ID, defaultID(pm.Name)); isIndexSuffix(suffix) {
					base += suffix
				}
			}
			body := catalog.ScaffoldWith(en.reader.Input(), catalog.ScaffoldOptions{})
			setBodyPath(body, en.field, "${"+prod.ID+"."+en.idPath+"}")
			read := &chain.Step{
				ID:          p.freeProbeID(base+"_after_"+st.ID, reserved),
				Description: fmt.Sprintf("the %s after %s: %s is what the contract says %s leaves it at.", en.carrier, st.ID, s.moved, shortRPC(st.Call)),
				Call:        en.reader.FullName,
				Auth:        en.contract.Auth,
				Body:        body,
				Expect:      SuccessExpectation(en.reader),
			}
			leaf := leafName(en.idPath)
			for _, sf := range carrierFields(en.reader, en.carrier) {
				if sf.Name == leaf {
					read.Expect = append(read.Expect, chain.Expectation{Path: en.carrier + "." + leaf, Equals: "${" + prod.ID + "." + en.idPath + "}"})
				}
			}
			p.insertAfter(at, read)
			at = read.ID
		}
	}
}

func (p *Plan) effectPass(lib *Library, r *effectRules, apply bool) (map[string][]string, map[string]bool, map[string][]string) {
	md := &effectModel{level: map[string]int64{}, known: map[string]bool{}, stockOf: map[string]*stockRule{}, orders: map[string]*modelOrder{},
		alias: map[string]string{}, dirty: map[string]bool{}, apply: apply, unread: map[string][]string{}}
	asserted := map[string][]string{}
	silent := map[string]bool{}
	mark := func(kind, id string) {
		if !containsString(asserted[kind], id) {
			asserted[kind] = append(asserted[kind], id)
		}
	}
	for _, st := range p.Chain.Steps {
		rpc := canonicalCall(p.cat, st.Call)
		if chain.IsReadOnlyCall(rpc) {
			if effectOutcome(st) == outcomeSuccess {
				p.assertReadEffects(lib, st, md, r, mark)
			}
			continue
		}
		md.flush()
		md.dirty = map[string]bool{}
		out := effectOutcome(st)
		if s := r.byEntity[rpc]; s != nil && out == outcomeSuccess {
			md.stockOf[st.ID] = s
			md.level[st.ID], md.known[st.ID] = 0, p.startsEmpty(lib, rpc, s)
		}
		if out == outcomeRefused {
			continue
		}
		handled := false
		if s := r.increase[rpc]; s != nil {
			handled = true
			if e := stepRefIn(st.Body[s.idField]); md.stockOf[e] != nil {
				q, lit := numericValue(st.Body[s.qtyField])
				p.moveStock(md, e, q, lit && out == outcomeSuccess)
				if md.known[e] && md.set(st, s.moved, md.level[e]) {
					mark("increase", st.ID)
				}
			}
		}
		if b := r.batch[rpc]; b != nil {
			handled = true
			items, _ := st.Body[b.list].([]any)
			for i, raw := range items {
				item, _ := raw.(map[string]any)
				e := stepRefIn(item[b.stock.idField])
				if md.stockOf[e] == nil || itemRefused(st, i) {
					continue
				}
				q, lit := numericValue(item[b.stock.qtyField])
				p.moveStock(md, e, q, lit && out == outcomeSuccess)
				if md.known[e] && md.set(st, fmt.Sprintf("%s.%d.%s", b.results, i, b.stock.moved), md.level[e]) {
					mark("batch", st.ID)
				}
			}
		}
		if r.orders[rpc] {
			handled = p.recordOrder(lib, st, rpc, out, md, r, mark) || handled
		}
		if v := r.reserve[rpc]; v != nil {
			handled = true
			o := md.order(stepRefIn(st.Body[v.orderField]))
			if o != nil && !o.reserved {
				for _, l := range o.lines {
					p.moveStock(md, l.entity, -l.qty, l.known && out == outcomeSuccess)
				}
				o.reserved = out == outcomeSuccess
				o.held = p.heldState(lib, st)
				md.watch(st, o)
				md.unsettle(o)
			} else if o == nil {
				p.forgetReferenced(md, st)
			}
		}
		if handled {
			continue
		}
		if p.restoreOrForget(lib, st, rpc, out, md, r) {
			continue
		}
		if touched := p.stockTouched(md, st); len(touched) > 0 {
			if c, ok := lib.Get(rpc); ok && p.saysUntouched(c, touched, md) {
				continue
			}
			for _, e := range touched {
				md.known[e] = false
			}
			silent[shortRPC(rpc)] = true
		}
	}
	md.flush()
	return asserted, silent, md.unread
}

func (md *effectModel) watch(st *chain.Step, o *modelOrder) {
	md.pending, md.waiting = st, nil
	for _, l := range o.lines {
		if md.dirty[l.entity] && !containsString(md.waiting, l.entity) {
			md.waiting = append(md.waiting, l.entity)
		}
	}
}

func (md *effectModel) flush() {
	if md.pending != nil && len(md.waiting) > 0 {
		md.unread[md.pending.ID] = md.waiting
	}
	md.pending, md.waiting = nil, nil
}

func (md *effectModel) set(st *chain.Step, path string, v int64) bool {
	return md.apply && assertNumber(st, path, v)
}

func (md *effectModel) order(id string) *modelOrder {
	for i := 0; i < 8 && md.alias[id] != ""; i++ {
		id = md.alias[id]
	}
	return md.orders[id]
}

func (p *Plan) moveStock(md *effectModel, e string, by int64, ok bool) {
	if md.stockOf[e] == nil {
		return
	}
	if !ok {
		md.known[e], md.dirty[e] = false, false
		return
	}
	md.level[e] += by
	md.dirty[e] = md.known[e]
}

func (md *effectModel) unsettle(o *modelOrder) {
	for _, l := range o.lines {
		md.known[l.entity] = false
	}
}

func (p *Plan) startsEmpty(lib *Library, rpc string, s *stockRule) bool {
	c, ok := lib.Get(rpc)
	if !ok {
		return false
	}
	for _, m := range startsAtZero.FindAllStringSubmatch(c.Summary, -1) {
		if containsString(s.words, strings.ToLower(m[1])) {
			return true
		}
	}
	return false
}

func (p *Plan) heldState(lib *Library, st *chain.Step) string {
	c, ok := lib.Get(canonicalCall(p.cat, st.Call))
	if !ok {
		return ""
	}
	for _, e := range p.entityStates(lib, st, c) {
		values := e.state.EnumValues[1:]
		short := enumShort(e.state.EnumValues)
		if v := stateIn([]string{c.Exports[e.carrier], c.Summary}, values, short); v != "" {
			return short[v]
		}
	}
	return ""
}

func (p *Plan) recordOrder(lib *Library, st *chain.Step, rpc string, out int, md *effectModel, r *effectRules, mark func(string, string)) bool {
	c, ok := lib.Get(rpc)
	if !ok {
		return false
	}
	m, err := p.cat.Lookup(rpc)
	if err != nil {
		return false
	}
	for _, f := range catalog.DescribeMessage(m.Input()).Fields {
		if !isIdempotencyField(f) {
			continue
		}
		key, ok := namecase.LookupKey(st.Body, f.Name)
		if !ok {
			continue
		}
		text, _ := st.Body[key].(string)
		if src, isRef := refSource(text); isRef && out == outcomeSuccess {
			if strings.Contains(text, ".request.") {
				src = strings.TrimPrefix(src, "steps.")
			}
			md.alias[st.ID] = src
			if t := r.total[rpc]; t != nil {
				if o := md.order(src); o != nil && o.hasTotal && md.set(st, t.carrier+"."+t.field, o.total) {
					mark("total", st.ID)
				}
			}
			return false
		}
	}
	if out != outcomeSuccess {
		return false
	}
	list, itemID, itemQty, entity := p.lineItems(rpc, c, "")
	if list == "" {
		return false
	}
	o := &modelOrder{}
	items, _ := st.Body[list].([]any)
	total, priced := int64(0), true
	t := r.total[rpc]
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		e := stepRefIn(item[itemID])
		q, lit := numericValue(item[itemQty])
		o.lines = append(o.lines, modelLine{entity: e, qty: q, known: lit})
		prod := p.stepByID(e)
		if t == nil || prod == nil || canonicalCall(p.cat, prod.Call) != entity || !lit {
			priced = false
			continue
		}
		price, ok := numericValue(prod.Body[t.price])
		if !ok {
			priced = false
			continue
		}
		total += q * price
	}
	md.orders[st.ID] = o
	if t != nil && priced && len(items) > 0 {
		o.total, o.hasTotal = total, true
		if md.set(st, t.carrier+"."+t.field, total) {
			mark("total", st.ID)
		}
	}
	return false
}

func (p *Plan) forgetReferenced(md *effectModel, st *chain.Step) {
	for _, e := range p.stockTouched(md, st) {
		md.known[e] = false
	}
}

func (p *Plan) stockTouched(md *effectModel, st *chain.Step) []string {
	out := []string{}
	for _, id := range referencedSteps(st.Body) {
		if md.stockOf[id] != nil && !containsString(out, id) {
			out = append(out, id)
		}
		if o := md.order(id); o != nil {
			for _, l := range o.lines {
				if md.stockOf[l.entity] != nil && !containsString(out, l.entity) {
					out = append(out, l.entity)
				}
			}
		}
	}
	return out
}

func (p *Plan) saysUntouched(c *RPCContract, touched []string, md *effectModel) bool {
	for _, m := range untouchedWords.FindAllStringSubmatch(c.Summary, -1) {
		for _, e := range touched {
			if sharesWord(contentWords(m[1], nil), md.stockOf[e].words) {
				return true
			}
		}
	}
	return false
}

func (p *Plan) restoreOrForget(lib *Library, st *chain.Step, rpc string, out int, md *effectModel, r *effectRules) bool {
	c, ok := lib.Get(rpc)
	if !ok {
		return false
	}
	var o *modelOrder
	for _, id := range referencedSteps(st.Body) {
		if found := md.order(id); found != nil {
			o = found
		}
	}
	if o == nil {
		return false
	}
	texts := []string{c.Summary}
	for _, k := range sortedRuleKeys(c.Exports) {
		texts = append(texts, c.Exports[k])
	}
	if !o.reserved {
		return restoreWord.MatchString(strings.Join(texts, " "))
	}
	texts = append(texts, lib.DescriptionOf(lib.Domain(rpc)))
	if o.held == "" || !restoresFrom(texts, o.held) {
		return false
	}
	for _, l := range o.lines {
		p.moveStock(md, l.entity, l.qty, l.known && out == outcomeSuccess)
	}
	o.reserved = out != outcomeSuccess
	md.watch(st, o)
	md.unsettle(o)
	return true
}

func (p *Plan) assertReadEffects(lib *Library, st *chain.Step, md *effectModel, r *effectRules, mark func(string, string)) {
	c, ok := lib.Get(canonicalCall(p.cat, st.Call))
	if !ok {
		return
	}
	m, err := p.cat.Lookup(st.Call)
	if err != nil {
		return
	}
	for name, ref := range topFrom(c, p.cat) {
		if strings.Contains(name, ".") {
			continue
		}
		key, ok := namecase.LookupKey(st.Body, name)
		if !ok {
			continue
		}
		id := stepRefIn(st.Body[key])
		md.waiting = removeString(md.waiting, id)
		if s := md.stockOf[id]; s != nil && ref.RPC == s.entityRPC && md.dirty[id] {
			if carrier := carrierHolding(m, s.moved); carrier != "" && md.set(st, carrier+"."+s.moved, md.level[id]) {
				mark("read", st.ID)
			}
		}
		if o := md.order(id); o != nil && o.hasTotal {
			if t := r.total[ref.RPC]; t != nil {
				if carrier := carrierHolding(m, t.field); carrier != "" && md.set(st, carrier+"."+t.field, o.total) {
					mark("total", st.ID)
				}
			}
		}
	}
}

func (p *Plan) noteEffects(r *effectRules, asserted map[string][]string, silent map[string]bool) {
	said := []string{}
	called := map[string]bool{}
	for _, st := range p.Chain.Steps {
		called[canonicalCall(p.cat, st.Call)] = true
	}
	r = r.onlyCalled(called)
	for _, rpc := range sortedRuleKeys(r.increase) {
		s := r.increase[rpc]
		said = append(said, fmt.Sprintf("%s after %s is the level before plus %s (%q)", s.moved, shortRPC(rpc), s.qtyField, s.sentence))
	}
	for _, rpc := range sortedRuleKeys(r.batch) {
		b := r.batch[rpc]
		said = append(said, fmt.Sprintf("each %s.N.%s after %s is its line applied as %s", b.results, b.stock.moved, shortRPC(rpc), shortRPC(b.stock.rpc)))
	}
	for _, rpc := range sortedRuleKeys(r.reserve) {
		v := r.reserve[rpc]
		said = append(said, fmt.Sprintf("%s after %s is the level before minus the %s of every %s naming that %s, a %s on two lines counted twice (%q)",
			v.stock.moved, shortRPC(rpc), v.itemQty, strings.TrimSuffix(v.list, "s"), strings.TrimPrefix(v.itemID, "id_"), strings.TrimPrefix(v.itemID, "id_"), v.sentence))
	}
	for _, rpc := range sortedRuleKeys(r.total) {
		t := r.total[rpc]
		said = append(said, fmt.Sprintf("%s.%s after %s and on every read of it is the sum of %s × %s over %s (%q)", t.carrier, t.field, shortRPC(rpc), t.itemQty, t.price, t.list, t.sentence))
	}
	ids := []string{}
	for _, kind := range []string{"increase", "batch", "total", "read"} {
		for _, id := range asserted[kind] {
			if !containsString(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) > 0 {
		shown := ids
		more := ""
		if len(shown) > 8 {
			shown, more = ids[:8], fmt.Sprintf(", … %d more", len(ids)-8)
		}
		p.note("steps %s%s assert numbers the plan works out from the literal values it sends, as the contracts state: %s. "+
			"A read asserts the level only right after the write that moved it, so a defect in one write fails the reads of that write alone",
			strings.Join(shown, ", "), more, strings.Join(said, "; "))
	}
	if len(silent) > 0 {
		names := []string{}
		for n := range silent {
			names = append(names, n)
		}
		sort.Strings(names)
		p.note("%s %s what the plan tracks and %s neither what %s does to it nor that it leaves it alone, so no level is "+
			"asserted after %s: state it in the summary (\"Reserve stock for every line\", \"does not touch stock\")",
			strings.Join(names, ", "), pluralVerb(len(names), "touches", "touch"), pluralVerb(len(names), "its contract says", "their contracts say"),
			pluralVerb(len(names), "it", "each"), pluralVerb(len(names), "it", "them"))
	}
}

func sortedRuleKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (p *Plan) probeSameEntityTwice(lib *Library, isTarget func(*chain.Step) bool) {
	r := p.effectRules(lib)
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		v := r.reserve[canonicalCall(p.cat, st.Call)]
		if v == nil || !isTarget(st) || effectOutcome(st) != outcomeSuccess {
			continue
		}
		key, ok := namecase.LookupKey(st.Body, v.orderField)
		if !ok {
			continue
		}
		order := p.stepByID(stepRefIn(st.Body[key]))
		if order == nil || canonicalCall(p.cat, order.Call) != v.orderRPC {
			continue
		}
		items, _ := order.Body[v.list].([]any)
		if len(items) == 0 {
			continue
		}
		first, ok := items[0].(map[string]any)
		if !ok {
			continue
		}
		entity := p.stepByID(stepRefIn(first[v.itemID]))
		q, lit := numericValue(first[v.itemQty])
		if entity == nil || !lit {
			continue
		}
		noun := strings.TrimPrefix(v.itemID, "id_")
		id := p.freeStepID(st.ID + "_same_" + noun + "_twice")
		fixture := copyStep(order, p.freeStepID(order.ID+"_for_"+id))
		fixture.Export = nil
		p.freshen(lib, fixture)
		renameStepRefs(fixture, order.ID, fixture.ID)
		one, other := cloneBody(first).(map[string]any), cloneBody(first).(map[string]any)
		a, b := q-1, int64(1)
		if a < 1 {
			a = 1
		}
		one[v.itemQty], other[v.itemQty] = strconv.FormatInt(a, 10), strconv.FormatInt(b, 10)
		fixture.Body[v.list] = []any{one, other}
		fixture.Expect = withoutItemCounts(fixture.Expect, v.list)
		fixture.Description = fmt.Sprintf("as %s, with %s on both %s (%d and %d), so %s must take %d of it.", order.ID, entity.ID, v.list, a, b, id, a+b)
		p.assertEcho(fixture)
		act := copyStep(st, id)
		act.Export = nil
		p.freshen(lib, act)
		act.Body[key] = "${" + fixture.ID + "." + v.orderIDPath + "}"
		renameStepRefs(act, st.ID, act.ID)
		act.Expect = retargetExpect(act.Expect, order.ID, fixture.ID)
		act.Description = fmt.Sprintf("%s on an order naming one %s on two %s: the stock read after it is down by both quantities.", st.ID, noun, v.list)
		steps := []*chain.Step{fixture, act}
		if e, ok := p.readerFor(lib, entity, v.stock.idPath); ok {
			body := catalog.ScaffoldWith(e.reader.Input(), catalog.ScaffoldOptions{})
			setBodyPath(body, e.field, "${"+entity.ID+"."+e.idPath+"}")
			steps = append(steps, &chain.Step{
				ID:          p.freeStepID(defaultID(e.reader.Name) + "_after_" + id),
				Description: fmt.Sprintf("the %s after %s: %s lower by %d, both lines counted.", e.carrier, id, v.stock.moved, a+b),
				Call:        e.reader.FullName,
				Auth:        e.contract.Auth,
				Body:        body,
				Expect:      SuccessExpectation(e.reader),
			})
		}
		p.Chain.Steps = append(p.Chain.Steps, steps...)
		p.note("step %s: %s names %s on both of its %s (%d and %d), so %s must take %d of it: a backend that reserves per product "+
			"rather than per line, counts one line only, or takes an extra unit per line fails the read after it", st.ID, fixture.ID, entity.ID, v.list, a, b, id, a+b)
		return
	}
}

func withoutItemCounts(expect []chain.Expectation, list string) []chain.Expectation {
	out := []chain.Expectation{}
	for _, e := range expect {
		segs := chain.SplitPath(e.Path)
		if len(segs) >= 2 && segs[len(segs)-2] == list && isIndexSegment(segs[len(segs)-1]) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (r *effectRules) onlyCalled(called map[string]bool) *effectRules {
	out := &effectRules{increase: map[string]*stockRule{}, batch: map[string]*batchRule{}, reserve: map[string]*reserveRule{}, total: map[string]*totalRule{}}
	for k, v := range r.increase {
		if called[k] {
			out.increase[k] = v
		}
	}
	for k, v := range r.batch {
		if called[k] {
			out.batch[k] = v
		}
	}
	for k, v := range r.reserve {
		if called[k] {
			out.reserve[k] = v
		}
	}
	for k, v := range r.total {
		if called[k] {
			out.total[k] = v
		}
	}
	return out
}

func removeString(list []string, drop string) []string {
	out := list[:0:0]
	for _, s := range list {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}
