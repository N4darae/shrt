package contract

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
	"gopkg.in/yaml.v3"
)

const (
	EffectNone    = "none"
	EffectZero    = "zero"
	EffectPerItem = "per_item"
)

type Effects map[string]*Effect

type Effect struct {
	Increase string `yaml:"increase,omitempty" json:"increase,omitempty"`
	Decrease string `yaml:"decrease,omitempty" json:"decrease,omitempty"`
	Of       string `yaml:"of,omitempty" json:"of,omitempty"`
	Restore  string `yaml:"restore,omitempty" json:"restore,omitempty"`
	Sum      string `yaml:"sum,omitempty" json:"sum,omitempty"`
	Times    string `yaml:"times,omitempty" json:"times,omitempty"`
	Is       string `yaml:"-" json:"is,omitempty"`
}

var (
	effectWords = []string{EffectNone, EffectZero, EffectPerItem}
	effectKeys  = []string{"increase", "decrease", "of", "restore", "sum", "times"}
)

const effectShape = "none, zero, per_item, or a map such as {increase: <request field>}"

func (e *Effects) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || IsTodo(n.Value)) {
		*e = nil
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: effects maps a field name to its effect (%s)", n.Line, effectShape)
	}
	out := Effects{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if v.Kind == yaml.ScalarNode && IsTodo(v.Value) {
			continue
		}
		eff := &Effect{}
		if err := eff.decode(v); err != nil {
			return fmt.Errorf("line %d: effects.%s: %v", v.Line, k.Value, err)
		}
		out[k.Value] = eff
	}
	*e = out
	return nil
}

func (e *Effect) decode(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		if slices.Contains(effectWords, n.Value) {
			e.Is = n.Value
			return nil
		}
		return fmt.Errorf("%q is not an effect: write %s%s", n.Value, effectShape, suggest(n.Value, effectWords))
	case yaml.MappingNode:
	default:
		return fmt.Errorf("an effect is %s", effectShape)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i].Value, n.Content[i+1]
		if !slices.Contains(effectKeys, key) {
			return fmt.Errorf("unknown key %q%s", key, suggest(key, effectKeys))
		}
		if val.Kind != yaml.ScalarNode || strings.TrimSpace(val.Value) == "" {
			return fmt.Errorf("%s takes one name, e.g. %s: qty", key, key)
		}
		*[]*string{&e.Increase, &e.Decrease, &e.Of, &e.Restore, &e.Sum, &e.Times}[slices.Index(effectKeys, key)] = strings.TrimSpace(val.Value)
	}
	stated := len(slices.DeleteFunc([]string{e.Increase, e.Decrease, e.Restore, e.Sum}, func(v string) bool { return v == "" }))
	switch {
	case stated != 1:
		return fmt.Errorf("an effect states exactly one of increase, decrease, restore or sum")
	case (e.Sum == "") != (e.Times == ""):
		return fmt.Errorf("sum and times go together: {sum: <list>.<qty>, times: <price field>}")
	case e.Of != "" && e.Increase == "" && e.Decrease == "":
		return fmt.Errorf("of goes with increase or decrease")
	}
	return nil
}

func (e Effect) MarshalYAML() (any, error) {
	if e.Is != "" {
		return e.Is, nil
	}
	type plain Effect
	return plain(e), nil
}

func (e *Effect) String() string {
	if e.Is != "" {
		return e.Is
	}
	parts := []string{}
	for _, kv := range [][2]string{{"increase", e.Increase}, {"decrease", e.Decrease}, {"of", e.Of}, {"restore", e.Restore}, {"sum", e.Sum}, {"times", e.Times}} {
		if kv[1] != "" {
			parts = append(parts, kv[0]+": "+kv[1])
		}
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func (es Effects) is(field, word string) bool {
	e := es[field]
	return e != nil && e.Is == word
}

func (es Effects) any(pred func(*Effect) bool) bool {
	for _, e := range es {
		if e != nil && pred(e) {
			return true
		}
	}
	return false
}

func (es Effects) increases() bool {
	return es.any(func(e *Effect) bool { return e.Increase != "" && e.Of == "" })
}

func (es Effects) perItem() bool {
	return es.any(func(e *Effect) bool { return e.Is == EffectPerItem })
}

func (es Effects) restores(state string) bool {
	return es.any(func(e *Effect) bool { return e.Restore != "" && SameState(e.Restore, state) })
}

func (es Effects) restoresAny() bool {
	return es.any(func(e *Effect) bool { return e.Restore != "" })
}

func SameState(a, b string) bool {
	a, b = strings.ToUpper(a), strings.ToUpper(b)
	return a == b || strings.HasSuffix(a, "_"+b) || strings.HasSuffix(b, "_"+a)
}

func quoteEffect(field string, e *Effect) string {
	return fmt.Sprintf("effects: {%s: %s}", field, e)
}

func suggest(name string, candidates []string) string {
	near := namecase.Closest(name, candidates, 1)
	for _, c := range candidates {
		if len(near) == 0 && (strings.HasPrefix(c, name+"_") || strings.HasSuffix(c, "_"+name)) {
			near = []string{c}
		}
	}
	if len(near) == 0 {
		return ""
	}
	return fmt.Sprintf(" (did you mean %q?)", near[0])
}

type effectSpec struct {
	field   string
	form    string
	sign    int64
	list    string
	qty     string
	idField string
	entity  string
	idPath  string
	of      string
	ofRPC   string
	ofPath  string
	price   string
	text    string
}

func numericAt(m *catalog.Method, field string) string {
	if f := fieldByName(catalog.DescribeMessage(m.Output()).Fields, field); f != nil && !f.Repeated && chain.IsNumericKind(f.Kind) {
		return f.Name
	}
	return carriedPath(m, field, true)
}

func numericNames(fields []*catalog.Field) []string {
	out := []string{}
	for _, f := range fields {
		if !f.Repeated && chain.IsNumericKind(f.Kind) {
			out = append(out, f.Name)
		}
		out = append(out, numericNames(f.Fields)...)
	}
	return out
}

func repeatedItems(fields []*catalog.Field) []string {
	out := []string{}
	for _, f := range fields {
		if f.Repeated && f.Kind == "message" && f.MapKey == "" {
			out = append(out, f.Name)
		}
	}
	return out
}

func writeRef(c *RPCContract, name string, cat *catalog.Catalog) (Ref, bool) {
	f := c.Fields[name]
	if f == nil || f.From == "" {
		return Ref{}, false
	}
	ref, err := ParseRef(f.From)
	if err != nil || chain.IsReadOnlyCall(ref.RPC) {
		return Ref{}, false
	}
	ref.RPC = canonicalCall(cat, ref.RPC)
	return ref, true
}

func linePath(in []*catalog.Field, path string) (list, qty string, problem string) {
	segs := chain.SplitPath(path)
	if len(segs) != 2 {
		return "", "", fmt.Sprintf("%q is not <repeated field>.<number>", path)
	}
	lf := fieldByName(in, segs[0])
	if lf == nil || !lf.Repeated || lf.Kind != "message" {
		return "", "", fmt.Sprintf("%q is not a repeated field of the request%s", segs[0], suggest(segs[0], repeatedItems(in)))
	}
	qf := fieldByName(lf.Fields, segs[1])
	if qf == nil || qf.Repeated || !chain.IsNumericKind(qf.Kind) {
		return "", "", fmt.Sprintf("%q is not a number on each %s%s", segs[1], segs[0], suggest(segs[1], numericNames(lf.Fields)))
	}
	return lf.Name, qf.Name, ""
}

func lineEntity(c *RPCContract, in []*catalog.Field, list string, accept func(*catalog.Method) bool, cat *catalog.Catalog) (id, entity, idPath string) {
	lf := fieldByName(in, list)
	if lf == nil {
		return "", "", ""
	}
	for _, sub := range lf.Fields {
		ref, ok := writeRef(c, list+"."+sub.Name, cat)
		if !ok {
			continue
		}
		if em, err := cat.Lookup(ref.RPC); err == nil && accept(em) {
			return sub.Name, ref.RPC, ref.Path
		}
	}
	return "", "", ""
}

func resolveEffects(rpc string, c *RPCContract, lib *Library, cat *catalog.Catalog) ([]effectSpec, []string) {
	if c == nil || len(c.Effects) == 0 || cat == nil {
		return nil, nil
	}
	m, err := cat.Lookup(rpc)
	if err != nil {
		return nil, nil
	}
	in := catalog.DescribeMessage(m.Input()).Fields
	specs, problems := []effectSpec{}, []string{}
	keys := sortedKeys(c.Effects)
	carries := func(field string) func(*catalog.Method) bool {
		return func(em *catalog.Method) bool { return numericAt(em, field) != "" }
	}
	for _, k := range keys {
		e := c.Effects[k]
		if e == nil {
			continue
		}
		s := effectSpec{field: k, text: quoteEffect(k, e), sign: 1}
		fail := func(format string, args ...any) {
			problems = append(problems, fmt.Sprintf("effects.%s: ", k)+fmt.Sprintf(format, args...))
		}
		switch {
		case e.Is == EffectPerItem:
			if !slices.Contains(repeatedItems(in), k) {
				fail("per_item names a repeated field of the request%s", suggest(k, repeatedItems(in)))
				continue
			}
			s.form = EffectPerItem
		case e.Is == EffectZero:
			if numericAt(m, k) == "" {
				fail("zero: %q is not a number this rpc answers with%s", k, suggest(k, answeredNumbers(m)))
				continue
			}
			s.form = EffectZero
		case e.Is == EffectNone || e.Restore != "":
			if !slices.Contains(catalogNumbers(cat), k) {
				fail("%q is a number no rpc answers with%s", k, suggest(k, catalogNumbers(cat)))
				continue
			}
			s.form = EffectNone
			if e.Restore != "" {
				if states := statesNear(m, c, cat); len(states) > 0 && !slices.ContainsFunc(states, func(s string) bool { return strings.EqualFold(s, e.Restore) }) {
					fail("restore: %q is not a state this rpc or the records it names answer with%s", e.Restore, suggest(e.Restore, states))
					continue
				}
				s.form = "restore"
			}
		case e.Sum != "":
			if numericAt(m, k) == "" {
				fail("sum: %q is not a number this rpc answers with%s", k, suggest(k, answeredNumbers(m)))
				continue
			}
			list, qty, problem := linePath(in, e.Sum)
			if problem != "" {
				fail("sum: %s", problem)
				continue
			}
			priced := func(em *catalog.Method) bool {
				f := fieldByName(catalog.DescribeMessage(em.Input()).Fields, e.Times)
				return f != nil && !f.Repeated && chain.IsNumericKind(f.Kind)
			}
			id, entity, idPath := lineEntity(c, in, list, priced, cat)
			if id == "" {
				sources, numbers := lineSources(c, in, list, cat)
				fail("times: %q is not a number in the request of %s%s", e.Times, sources, suggest(e.Times, numbers))
				continue
			}
			s.form, s.list, s.qty, s.idField, s.entity, s.idPath, s.price = "total", list, qty, id, entity, idPath, e.Times
		default:
			path := e.Increase
			if e.Decrease != "" {
				path, s.sign = e.Decrease, -1
			}
			if e.Of != "" || len(chain.SplitPath(path)) == 2 {
				lc, lin, form, where, owner := c, in, "batch", "", ""
				if e.Of != "" {
					ref, ok := writeRef(c, e.Of, cat)
					if !ok || strings.Contains(e.Of, ".") {
						fail("of: %q is not a request field wired with from: to another write%s", e.Of, suggest(e.Of, wiredWrites(c, cat)))
						continue
					}
					om, err := cat.Lookup(ref.RPC)
					oc, ok := lib.Get(ref.RPC)
					if err != nil || !ok {
						fail("of: %s has no contract to read its lines from", shortRPC(ref.RPC))
						continue
					}
					lc, lin, form = oc, catalog.DescribeMessage(om.Input()).Fields, "reserve"
					where, owner = fmt.Sprintf("%s of %s: ", path, shortRPC(ref.RPC)), shortRPC(ref.RPC)+"."
					s.of, s.ofRPC, s.ofPath = e.Of, ref.RPC, ref.Path
				}
				list, qty, problem := linePath(lin, path)
				if problem != "" {
					fail("%s%s", where, problem)
					continue
				}
				id, entity, idPath := lineEntity(lc, lin, list, carries(k), cat)
				if id == "" {
					fail("no field of %s%s is wired with from: to a record that answers %s", owner, list, k)
					continue
				}
				s.form, s.list, s.qty, s.idField, s.entity, s.idPath = form, list, qty, id, entity, idPath
				break
			}
			f := fieldByName(in, path)
			if f == nil || f.Repeated || !chain.IsNumericKind(f.Kind) {
				fail("%q is not a number of the request%s", path, suggest(path, numericNames(in)))
				continue
			}
			for _, name := range wiredWrites(c, cat) {
				ref, _ := writeRef(c, name, cat)
				if em, err := cat.Lookup(ref.RPC); err == nil && carries(k)(em) {
					s.idField, s.entity, s.idPath = name, ref.RPC, ref.Path
					break
				}
			}
			if s.idField == "" {
				fail("no request field is wired with from: to a record that answers %s", k)
				continue
			}
			s.form, s.qty = "increase", f.Name
		}
		specs = append(specs, s)
	}
	return specs, problems
}

func wiredWrites(c *RPCContract, cat *catalog.Catalog) []string {
	out := []string{}
	for _, name := range sortedKeys(c.Fields) {
		if _, ok := writeRef(c, name, cat); ok && !strings.Contains(name, ".") {
			out = append(out, name)
		}
	}
	return out
}

func lineSources(c *RPCContract, in []*catalog.Field, list string, cat *catalog.Catalog) (string, []string) {
	names, numbers := []string{}, []string{}
	if lf := fieldByName(in, list); lf != nil {
		for _, sub := range lf.Fields {
			ref, ok := writeRef(c, list+"."+sub.Name, cat)
			if !ok {
				continue
			}
			names = append(names, fmt.Sprintf("%s (which %s.%s is wired from:)", shortRPC(ref.RPC), list, sub.Name))
			if em, err := cat.Lookup(ref.RPC); err == nil {
				numbers = append(numbers, numericNames(catalog.DescribeMessage(em.Input()).Fields)...)
			}
		}
	}
	if len(names) == 0 {
		return "a record any field of " + list + " is wired from:", numbers
	}
	return strings.Join(names, " or "), numbers
}

func catalogNumbers(cat *catalog.Catalog) []string {
	out := []string{}
	for _, m := range cat.Methods() {
		out = append(out, numericNames(catalog.DescribeMessage(m.Output()).Fields)...)
	}
	return out
}

func enumValues(fields []*catalog.Field) []string {
	out := []string{}
	for _, f := range fields {
		if len(f.EnumValues) > 1 {
			for full, short := range enumShort(f.EnumValues) {
				out = append(out, full, short)
			}
		}
		out = append(out, enumValues(f.Fields)...)
	}
	sort.Strings(out)
	return out
}

func statesNear(m *catalog.Method, c *RPCContract, cat *catalog.Catalog) []string {
	out := enumValues(catalog.DescribeMessage(m.Output()).Fields)
	for _, name := range wiredWrites(c, cat) {
		ref, _ := writeRef(c, name, cat)
		if em, err := cat.Lookup(ref.RPC); err == nil {
			out = append(out, enumValues(catalog.DescribeMessage(em.Output()).Fields)...)
		}
	}
	return out
}

func EffectProblems(lib *Library, cat *catalog.Catalog) []Issue {
	issues := []Issue{}
	if lib == nil || cat == nil {
		return issues
	}
	for _, o := range lib.Overlays {
		for _, rpc := range sortedKeys(o.RPCs) {
			m, err := cat.Lookup(rpc)
			if err != nil {
				continue
			}
			_, problems := resolveEffects(m.FullName, o.RPCs[rpc], lib, cat)
			for _, pr := range problems {
				field, msg, _ := strings.Cut(pr, ": ")
				issues = append(issues, Issue{Domain: o.Domain, RPC: rpc, Field: field, Severity: SeverityError, Message: msg})
			}
		}
	}
	return issues
}

func effectsTodo(m *catalog.Method, all []*catalog.Method) string {
	idOf := func(fields []*catalog.Field) (string, []Producer) {
		for _, f := range fields {
			if IsEntityIDField(f.Name) && !f.Repeated {
				if producers := ProducersOf(f.Name, all, m.FullName); len(producers) > 0 {
					return f.Name, producers
				}
			}
		}
		return "", nil
	}
	number := func(fields []*catalog.Field) string {
		for _, f := range fields {
			if !f.Repeated && chain.IsNumericKind(f.Kind) && !idLike(f.Name) {
				return f.Name
			}
		}
		return ""
	}
	in := catalog.DescribeMessage(m.Input()).Fields
	path, producers := "", []Producer(nil)
	if id, prods := idOf(in); id != "" && number(in) != "" {
		path, producers = number(in), prods
	}
	for _, f := range in {
		if path == "" && f.Repeated && f.Kind == "message" {
			if id, prods := idOf(f.Fields); id != "" && number(f.Fields) != "" {
				path, producers = f.Name+"."+number(f.Fields), prods
			}
		}
	}
	if path == "" {
		return ""
	}
	moved := "<number>"
	for _, pm := range all {
		if pm.FullName != producers[0].RPC {
			continue
		}
		asked := numericNames(catalog.DescribeMessage(pm.Input()).Fields)
		kept := []string{}
		for _, name := range answeredNumbers(pm) {
			if !slices.Contains(asked, name) && !idLike(name) && !IsVerdictFieldName(name) && !slices.Contains(kept, name) {
				kept = append(kept, name)
			}
		}
		carried := numericNames(append(catalog.DescribeMessage(m.Input()).Fields, catalog.DescribeMessage(m.Output()).Fields...))
		if len(kept) == 1 && slices.Contains(carried, kept[0]) {
			moved = kept[0]
		}
	}
	return fmt.Sprintf("%s: what this write does to %s: none | {increase: %s} | {decrease: %s} | ...; see GRAMMAR effects",
		TodoMarker, moved, path, path)
}

func answeredNumbers(m *catalog.Method) []string {
	out := []string{}
	for _, f := range catalog.DescribeMessage(m.Output()).Fields {
		if f.Kind == "message" && !f.Repeated && f.Name != chain.EnvelopeField() {
			for _, sf := range f.Fields {
				if !sf.Repeated && chain.IsNumericKind(sf.Kind) {
					out = append(out, sf.Name)
				}
			}
		} else if !f.Repeated && chain.IsNumericKind(f.Kind) {
			out = append(out, f.Name)
		}
	}
	return out
}
