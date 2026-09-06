package contract

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"gopkg.in/yaml.v3"
)

type Plan struct {
	Target string       `json:"target" yaml:"target"`
	Order  []string     `json:"order" yaml:"order"`
	Chain  *chain.Chain `json:"-" yaml:"-"`
	Notes  []string     `json:"notes,omitempty" yaml:"notes,omitempty"`

	stepOf  map[string]string
	cat     *catalog.Catalog
	pending []pendingChecks
}

type pendingChecks struct {
	step     *chain.Step
	contract *RPCContract
	schema   *catalog.Schema
}

func BuildPlan(target string, lib *Library, cat *catalog.Catalog, name string) (*Plan, error) {
	m, err := cat.Lookup(target)
	if err != nil {
		return nil, err
	}
	order, err := resolveOrder(m.FullName, lib, cat)
	if err != nil {
		return nil, err
	}

	p := &Plan{Target: m.FullName, Order: order, stepOf: map[string]string{}, cat: cat}
	c := &chain.Chain{
		APIVersion:  chain.APIVersion,
		Name:        name,
		Description: fmt.Sprintf("reach %s, composed from the contract dependency graph", m.Name),
	}
	p.Chain = c
	for _, node := range order {
		rpc, alias := SplitNode(node)
		method, err := cat.Lookup(rpc)
		if err != nil {
			return nil, err
		}
		base := defaultID(method.Name)
		if alias != "" {
			base += "_" + alias
		}
		id := uniqueStepID(c, base)
		p.stepOf[node] = id
		c.Steps = append(c.Steps, p.buildStep(id, alias, method, lib))
	}
	p.noteRequirements()
	if err := c.Normalize(); err != nil {
		return nil, err
	}
	return p, nil
}

func ArmedOneofMembersOf(lib *Library, rpc string) []string {
	if lib == nil {
		return nil
	}
	c, ok := lib.Get(rpc)
	if !ok {
		return nil
	}
	return ArmedOneofMembers(c, "")
}

func ArmedOneofMembers(c *RPCContract, alias string) []string {
	if c == nil {
		return nil
	}
	fields := c.FieldsFor(alias)
	out := []string{}
	for _, name := range sortedFieldNames(fields) {
		f := fields[name]
		if f.OneOf != "" && (f.Value != "" || f.From != "" || f.SameAs != "") {
			out = append(out, name)
		}
	}
	return out
}

func ScaffoldSteps(refs, ids []string, lib *Library, cat *catalog.Catalog) ([]*yaml.Node, []string, error) {
	p := &Plan{stepOf: map[string]string{}, cat: cat}
	methods := make([]*catalog.Method, len(refs))
	for i, ref := range refs {
		m, err := cat.Lookup(ref)
		if err != nil {
			return nil, nil, err
		}
		methods[i] = m
		p.stepOf[m.FullName] = ids[i]
	}
	nodes := make([]*yaml.Node, 0, len(refs))
	for i, m := range methods {
		step := p.buildStep(ids[i], "", m, lib)
		body := step.Body
		bare := *step
		bare.Body = nil
		node := &yaml.Node{}
		if err := node.Encode(&bare); err != nil {
			return nil, nil, err
		}
		inject(node, bodyNode(catalog.DescribeMessage(m.Input()).Fields, body))
		nodes = append(nodes, node)
	}
	return nodes, p.Notes, nil
}

func ScaffoldStep(m *catalog.Method, id string, lib *Library, cat *catalog.Catalog) *yaml.Node {
	nodes, _, err := ScaffoldSteps([]string{m.FullName}, []string{id}, lib, cat)
	if err != nil || len(nodes) == 0 {
		return nil
	}
	return nodes[0]
}

func (p *Plan) buildStep(id, alias string, m *catalog.Method, lib *Library) *chain.Step {
	c, ok := lib.Get(m.FullName)
	step := &chain.Step{
		ID:     id,
		Call:   m.FullName,
		Body:   catalog.ScaffoldWith(m.Input(), catalog.ScaffoldOptions{Prefer: ArmedOneofMembers(c, alias)}),
		Expect: SuccessExpectation(m),
	}
	if !ok {
		p.note("step %s: %s has no contract, its body is a bare scaffold", id, m.FullName)
		return step
	}
	if c.Summary != "" {
		step.Description = firstSentence(c.Summary)
	}
	step.Auth = c.Auth
	if alias != "" {
		if _, declared := c.Aliases[alias]; !declared {
			p.note("step %s: alias %q is not declared on %s, so this step is identical to its sibling", id, alias, m.FullName)
		}
	}

	fields := c.FieldsFor(alias)
	schema := catalog.DescribeMessage(m.Input())
	for _, name := range sortedFieldNames(fields) {
		f := fields[name]
		switch {
		case f.Value != "":
			setBodyPath(step.Body, name, typedValue(f.Value, schema, name))
		case f.From != "":
			ref, err := ParseRef(f.From)
			if err != nil {
				continue
			}
			src, ok := p.stepOf[ref.Node()]
			if !ok {
				p.note("step %s: %s wants %s but that rpc is not in the plan", id, name, ref.Node())
				continue
			}
			setBodyPath(step.Body, name, "${"+src+"."+ref.Path+"}")
		case f.SameAs != "":
			p.bindSameAs(step, id, name, f.SameAs)
		}
	}
	if len(step.Expect) == 0 {
		p.note("step %s: %s has no scalar or repeated response field, so nothing could be scaffolded to "+
			"assert. Write one — a step with no expect: passes whatever the server answers, and "+
			"'chain lint -strict' rejects it", id, m.FullName)
	}
	p.pending = append(p.pending, pendingChecks{step: step, contract: c, schema: schema})
	if len(c.Exports) > 0 {
		step.Export = map[string]string{}
		for _, path := range sortedKeys(c.Exports) {
			step.Export[exportName(id, path)] = path
		}
	}
	return step
}

func (p *Plan) note(format string, args ...any) {
	p.Notes = append(p.Notes, fmt.Sprintf(format, args...))
}

func setBodyPath(body map[string]any, path string, value any) {
	segs := chain.SplitPath(path)
	if len(segs) == 0 {
		return
	}
	var cur any = body
	for i, seg := range segs {
		last := i == len(segs)-1
		if list, ok := cur.([]any); ok {
			idx, err := strconv.Atoi(seg)
			if err == nil {
				if idx < 0 || idx >= len(list) {
					return
				}
				if last {
					list[idx] = value
					return
				}
				cur = list[idx]
				continue
			}
			if len(list) == 0 {
				return
			}
			cur = list[0]
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return
		}
		if last {
			m[seg] = value
			return
		}
		cur = childContainer(m, seg)
	}
}

func childContainer(m map[string]any, seg string) any {
	switch next := m[seg].(type) {
	case map[string]any:
		return next
	case []any:
		return next
	}
	next := map[string]any{}
	m[seg] = next
	return next
}

func resolveOrder(target string, lib *Library, cat *catalog.Catalog) ([]string, error) {
	order := []string{}
	state := map[string]int{}

	var visit func(node string, trail []string) error
	visit = func(node string, trail []string) error {
		rpc, alias := SplitNode(node)
		if m, err := cat.Lookup(rpc); err == nil {
			rpc = m.FullName
		}
		canonical := rpc
		if alias != "" {
			canonical = rpc + "@" + alias
		}
		switch state[canonical] {
		case 1:
			return fmt.Errorf("dependency cycle: %s", strings.Join(append(trail, canonical), " -> "))
		case 2:
			return nil
		}
		state[canonical] = 1
		if c, ok := lib.Get(rpc); ok {
			for _, dep := range c.DependenciesFor(alias) {
				if err := visit(dep, append(trail, canonical)); err != nil {
					return err
				}
			}
		}
		for _, requires := range lib.RequiredBy(rpc) {
			if err := visit(requires, append(trail, canonical)); err != nil {
				return err
			}
		}
		state[canonical] = 2
		order = append(order, canonical)
		return nil
	}
	if err := visit(target, nil); err != nil {
		return nil, err
	}
	return order, nil
}

func (p *Plan) YAML() ([]byte, error) {
	doc := &yaml.Node{}
	if err := doc.Encode(&chain.Chain{
		APIVersion:  p.Chain.APIVersion,
		Name:        p.Chain.Name,
		Description: p.Chain.Description,
		Vars:        p.Chain.Vars,
	}); err != nil {
		return nil, err
	}
	steps := &yaml.Node{Kind: yaml.SequenceNode}
	for _, step := range p.Chain.Steps {
		node, err := p.stepNode(step)
		if err != nil {
			return nil, err
		}
		steps.Content = append(steps.Content, node)
	}
	setMappingKey(doc, "steps", steps)
	return yaml.Marshal(doc)
}

func (p *Plan) stepNode(step *chain.Step) (*yaml.Node, error) {
	body := step.Body
	shallow := *step
	shallow.Body = nil
	node := &yaml.Node{}
	if err := node.Encode(&shallow); err != nil {
		return nil, err
	}
	m, err := p.cat.Lookup(step.Call)
	if err != nil {
		return nil, err
	}
	inject(node, bodyNode(catalog.DescribeMessage(m.Input()).Fields, body))
	return node, nil
}

func setMappingKey(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func uniqueStepID(c *chain.Chain, base string) string {
	id := base
	for i := 2; ; i++ {
		if _, exists := c.Step(id); !exists {
			return id
		}
		id = fmt.Sprintf("%s_%d", base, i)
	}
}

func exportName(stepID, path string) string {
	return stepID + "_" + strings.ReplaceAll(path, ".", "_")
}

func firstSentence(s string) string {
	flat := strings.Join(strings.Fields(s), " ")
	if i := strings.Index(flat, ". "); i >= 0 {
		return flat[:i+1]
	}
	return flat
}

func (p *Plan) bindSameAs(step *chain.Step, id, field, raw string) {
	ref, err := ParseRef(raw)
	if err != nil {
		return
	}
	producerID, ok := p.stepOf[ref.Node()]
	if !ok {
		p.note("step %s: %s must equal what %s sends, but that rpc is not in the plan", id, field, ref.Node())
		return
	}
	producer := p.stepByID(producerID)
	if producer == nil {
		return
	}
	varName := producerID + "_" + strings.ReplaceAll(ref.Path, ".", "_")
	token := "${vars." + varName + "}"

	seed, _ := bodyValue(producer.Body, ref.Path)
	if text, ok := seed.(string); ok && chain.IsStableRef(text) {
		setBodyPath(step.Body, field, text)
		return
	}
	if p.Chain.Vars == nil {
		p.Chain.Vars = map[string]any{}
	}
	if _, declared := p.Chain.Vars[varName]; !declared {
		if IsPlaceholder(seed, ScaffoldedBody) || hasTemplate(seed) {
			p.Chain.Vars[varName] = ""
			p.note("step %s: %s and %s %s must send the same value — both read ${vars.%s}, which is "+
				"empty. Two ways to fill it, and they are not interchangeable: a literal in vars: if the "+
				"value may repeat, or 'shrt run <chain> -var %s=...' if it must be fresh EVERY run, "+
				"because a uniqueness-constrained field takes the literal once and is refused the second "+
				"time. ${uuid} inside vars: is rejected by lint and always will be — no step has run when "+
				"vars: is read, so only generators could ever resolve there and a rule about which "+
				"references work where is worse than one that always fails loudly",
				id, field, producerID, ref.Path, varName, varName)
		} else {
			p.Chain.Vars[varName] = seed
		}
	}
	setBodyPath(producer.Body, ref.Path, token)
	setBodyPath(step.Body, field, token)
}

func (p *Plan) stepByID(id string) *chain.Step {
	for i := range p.Chain.Steps {
		if p.Chain.Steps[i].ID == id {
			return p.Chain.Steps[i]
		}
	}
	return nil
}

func hasTemplate(v any) bool {
	s, ok := v.(string)
	return ok && strings.Contains(s, "${")
}

func (p *Plan) noteRequirements() {
	for _, pc := range p.pending {
		id := pc.step.ID
		if pc.contract.IsUnfilled("required") {
			p.note("step %s: required is an unfilled TODO, so this plan cannot say what the server rejects without — treat the body as unverified", id)
		}
		for _, name := range pc.contract.Required {
			if IsRequiredNone(name) {
				continue
			}
			if strings.TrimSpace(name) == RequiredUnknown {
				p.note("step %s: the contract says UNKNOWN — nobody has determined what this rpc rejects "+
					"without, so nothing here can tell you the body is complete", id)
				continue
			}
			if !HasUsableValue(pc.step.Body, name, ScaffoldedBody) {
				p.note("step %s: %s is required and has no usable value — fill it", id, name)
			}
		}
		if len(pc.contract.RequiresRole) > 0 && !pc.contract.DeclaresNoRole() {
			p.note("step %s: caller must hold role %s", id, strings.Join(pc.contract.RequiresRole, " or "))
		}
	}
}

func typedValue(raw string, schema *catalog.Schema, path string) any {
	if schema == nil || chain.HasReference(raw) {
		return raw
	}
	kind := fieldKindAt(schema.Fields, chain.SplitPath(path))
	switch kind {
	case "bool":
		switch raw {
		case "true":
			return true
		case "false":
			return false
		}
	}
	return raw
}

func fieldKindAt(fields []*catalog.Field, segs []string) string {
	if len(segs) == 0 {
		return ""
	}
	for _, f := range fields {
		if f.Name != segs[0] {
			continue
		}
		if len(segs) == 1 {
			return f.Kind
		}
		return fieldKindAt(f.Fields, segs[1:])
	}
	return ""
}
