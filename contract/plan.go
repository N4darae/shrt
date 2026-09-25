package contract

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"gopkg.in/yaml.v3"
)

type Plan struct {
	Target  string       `json:"target" yaml:"target"`
	Targets []string     `json:"targets" yaml:"targets"`
	Order   []string     `json:"order" yaml:"order"`
	Chain   *chain.Chain `json:"-" yaml:"-"`
	Notes   []string     `json:"notes,omitempty" yaml:"notes,omitempty"`

	stepOf   map[string]string
	cat      *catalog.Catalog
	pending  []pendingChecks
	grown    []string
	reserved map[string]bool
	noun     string
	seconds  []producerSecond
	preps    map[string][]string
	opts     PlanOptions
}

type PlanOptions struct {
	Auth     bool
	Profiles []string
	Logins   []string
}

type pendingChecks struct {
	step     *chain.Step
	contract *RPCContract
	schema   *catalog.Schema
	fields   map[string]*FieldContract
}

func BuildPlan(target string, lib *Library, cat *catalog.Catalog, name string) (*Plan, error) {
	return BuildPlanFor([]string{target}, lib, cat, name)
}

func BuildPlanFor(targets []string, lib *Library, cat *catalog.Catalog, name string) (*Plan, error) {
	return BuildPlanWith(targets, lib, cat, name, PlanOptions{})
}

func BuildPlanWith(targets []string, lib *Library, cat *catalog.Catalog, name string, opts PlanOptions) (*Plan, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("nothing to plan: name at least one rpc")
	}
	nodes := []string{}
	labels := []string{}
	seen := map[string]bool{}
	repeats := map[string]int{}
	for _, raw := range targets {
		node, m, err := ResolveTarget(raw, lib, cat)
		if err != nil {
			return nil, err
		}
		if m.Streaming() {
			return nil, fmt.Errorf("refusing to plan %s: %s", m.FullName, m.StreamRefusal())
		}
		if seen[node] {
			repeats[node]++
			continue
		}
		seen[node] = true
		nodes = append(nodes, node)
		labels = append(labels, shortNode(node))
	}
	order, edges, listed, err := resolveOrder(nodes, lib, cat)
	if err != nil {
		return nil, err
	}
	for _, node := range order {
		rpc, _ := SplitNode(node)
		if dep, err := cat.Lookup(rpc); err == nil && dep.Streaming() {
			return nil, fmt.Errorf("refusing to plan %s: its contract graph pulls in %s, and %s — drop that "+
				"edge (needs/from/same_as/before) from the contract", strings.Join(nodes, ", "), dep.FullName, dep.StreamRefusal())
		}
	}

	p := &Plan{Target: strings.Join(nodes, ", "), Targets: nodes, Order: order, stepOf: map[string]string{}, cat: cat, opts: opts}
	c := &chain.Chain{
		APIVersion:  chain.APIVersion,
		Name:        name,
		Description: fmt.Sprintf("reach %s, composed from the contract dependency graph", strings.Join(labels, ", ")),
	}
	if !anyContract(order, lib) {
		c.Description = fmt.Sprintf("reach %s; no contract covers %s yet, so this is a bare scaffold with no "+
			"dependency graph behind it", strings.Join(labels, ", "), pluralVerb(len(labels), "it", "them"))
	}
	p.Chain = c
	isTarget := map[string]bool{}
	for _, node := range nodes {
		isTarget[node] = true
	}
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
		step := p.buildStep(id, alias, method, lib)
		p.splitSharedProducers(step, p.grown)
		c.Steps = append(c.Steps, step)
		if !isTarget[node] {
			p.prepareSecondProducers(step)
		}
	}
	p.noteListProducers(listed)
	targetSteps := map[string]bool{}
	for _, node := range nodes {
		targetSteps[p.stepOf[node]] = true
	}
	p.discriminateListOrder(lib, func(st *chain.Step) bool { return targetSteps[st.ID] })
	p.probeUniqueness(lib, func(st *chain.Step) bool { return targetSteps[st.ID] })
	p.probeListFilters(lib, func(st *chain.Step) bool { return targetSteps[st.ID] })
	p.probeInsufficiency(lib, func(st *chain.Step) bool { return targetSteps[st.ID] })
	p.probeBoundaries(lib, func(st *chain.Step) bool { return targetSteps[st.ID] })
	p.probeBatch(lib, func(st *chain.Step) bool { return targetSteps[st.ID] })
	p.probeIdempotency(lib, func(st *chain.Step) bool { return targetSteps[st.ID] })
	p.probeDenials(lib, func(st *chain.Step) bool { return targetSteps[st.ID] })
	p.probeRoleParity(lib, func(st *chain.Step) bool { return targetSteps[st.ID] })
	p.probeItemCounts(lib, func(st *chain.Step) bool { return targetSteps[st.ID] })
	p.echoNumbers()
	p.noteRepeatedTargets(nodes, repeats, lib)
	p.noteAliasSiblings(edges)
	p.noteRequirements()
	if err := c.Normalize(); err != nil {
		return nil, err
	}
	if missing, _ := chain.ExternalInputs(c); len(missing) > 0 {
		p.declareInterpolatedVars(missing)
	}
	if missing, _ := chain.ExternalInputs(c); len(missing) > 0 {
		p.note("the chain reads ${vars.%s}, which the contract's value: entries use and the chain does not declare: "+
			"pass -var %s=... on every run (a fresh value when it makes names unique), or declare it under vars:",
			strings.Join(missing, "}, ${vars."), strings.Join(missing, "=... -var "))
	}
	return p, nil
}

func ResolveTarget(raw string, lib *Library, cat *catalog.Catalog) (string, *catalog.Method, error) {
	rpc, alias := SplitNode(raw)
	m, err := cat.Lookup(rpc)
	if err != nil {
		return "", nil, err
	}
	if alias == "" {
		return m.FullName, m, nil
	}
	c, ok := lib.Get(m.FullName)
	if !ok {
		return "", nil, fmt.Errorf("%s has no contract, so it has no alias %q to plan", m.FullName, alias)
	}
	if _, declared := c.Aliases[alias]; !declared {
		names := sortedAliasNames(c.Aliases)
		have := "it declares none"
		if len(names) > 0 {
			have = "declared: " + strings.Join(names, ", ")
		}
		return "", nil, fmt.Errorf("%s has no alias %q (%s)", m.FullName, alias, have)
	}
	return m.FullName + "@" + alias, m, nil
}

func shortNode(node string) string {
	if i := strings.LastIndex(node, "/"); i >= 0 {
		return node[i+1:]
	}
	return node
}

func (p *Plan) noteAliasSiblings(edges map[string][]string) {
	byRPC := map[string][]string{}
	rpcs := []string{}
	for _, node := range p.Order {
		rpc, _ := SplitNode(node)
		if _, ok := byRPC[rpc]; !ok {
			rpcs = append(rpcs, rpc)
		}
		byRPC[rpc] = append(byRPC[rpc], node)
	}
	for _, rpc := range rpcs {
		nodes := byRPC[rpc]
		if len(nodes) < 2 || !containsString(nodes, rpc) || containsString(edges[rpc], "the plan target") {
			continue
		}
		if !p.plainLooksDuplicate(rpc, nodes, edges[rpc]) {
			continue
		}
		aliased := []string{}
		ids := []string{p.stepOf[rpc]}
		first := ""
		for _, node := range nodes {
			if node == rpc {
				continue
			}
			if first == "" {
				first = node
			}
			ids = append(ids, p.stepOf[node])
			aliased = append(aliased, fmt.Sprintf("%s (via %s)", shortNode(node), strings.Join(edges[node], "; ")))
		}
		p.note("steps %s all call %s: the plain %s came in via %s, while %s came in through other edges. "+
			"The plain step is built from the unaliased fields only, so it is likely a duplicate of an "+
			"aliased one — if it is, point that edge at the alias (needs: [%s], or from:/same_as: naming it) or drop it. before: "+
			"always names the plain rpc, so an ordering meant for an alias belongs in the later rpc's "+
			"needs: instead",
			strings.Join(ids, ", "), shortNode(rpc), shortNode(rpc), strings.Join(edges[rpc], "; "),
			strings.Join(aliased, ", "), shortNode(first))
	}
}

func (p *Plan) plainLooksDuplicate(rpc string, nodes, via []string) bool {
	onlyBefore := len(via) > 0
	for _, edge := range via {
		if !strings.Contains(edge, " before: ") {
			onlyBefore = false
		}
	}
	if onlyBefore {
		return true
	}
	plain := p.stepByID(p.stepOf[rpc])
	if plain == nil {
		return false
	}
	for _, node := range nodes {
		if node == rpc {
			continue
		}
		if s := p.stepByID(p.stepOf[node]); s != nil && s.Auth == plain.Auth && reflect.DeepEqual(s.Body, plain.Body) {
			return true
		}
	}
	return false
}

func (p *Plan) noteRepeatedTargets(nodes []string, repeats map[string]int, lib *Library) {
	for _, node := range nodes {
		n := repeats[node]
		if n == 0 {
			continue
		}
		rpc, _ := SplitNode(node)
		how := fmt.Sprintf("declare one under aliases: on %s's contract (aliases: {after: {note: ...}}) and name it "+
			"as %s@after", shortNode(rpc), shortNode(rpc))
		if c, ok := lib.Get(rpc); ok && len(c.Aliases) > 0 {
			names := sortedAliasNames(c.Aliases)
			how = fmt.Sprintf("name one of its aliases instead, such as %s@%s (declared: %s)",
				shortNode(rpc), names[0], strings.Join(names, ", "))
		}
		p.note("%s is named %d times as a target, and a plan calls each target once, so the repeat(s) were "+
			"merged into step %s. To call it again at another point in the chain, %s",
			shortNode(node), n+1, p.stepOf[node], how)
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
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
	p, err := scaffoldPlan("", "the step", refs, ids, lib, cat)
	if err != nil {
		return nil, nil, err
	}
	nodes := make([]*yaml.Node, 0, len(refs))
	for _, step := range p.Chain.Steps {
		m, err := cat.Lookup(step.Call)
		if err != nil {
			return nil, nil, err
		}
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
		p.grown = secondItems(step.Body, catalog.DescribeMessage(m.Input()).Fields)
		p.noteSecondItems(id, p.grown)
		return step
	}
	if c.Summary != "" && !IsTodo(c.Summary) {
		step.Description = firstSentence(c.Summary)
	}
	if a := c.Aliases[alias]; alias != "" && a != nil && strings.TrimSpace(a.Note) != "" && !IsTodo(a.Note) {
		note := strings.Join(strings.Fields(a.Note), " ")
		if step.Description == "" {
			step.Description = note
		} else {
			step.Description = strings.TrimSuffix(step.Description, ".") + " — " + note
		}
	}
	step.Auth = c.Auth
	if alias != "" {
		if _, declared := c.Aliases[alias]; !declared {
			p.note("step %s: alias %q is not declared on %s, so this step is identical to its sibling", id, alias, m.FullName)
		}
	}

	fields := c.FieldsFor(alias)
	schema := catalog.DescribeMessage(m.Input())
	names := byIndexDepth(sortedFieldNames(fields))
	for _, name := range names {
		growLists(step.Body, name)
	}
	for _, name := range names {
		f := fields[name]
		placed := true
		switch {
		case f.Value != "":
			placed = setBodyPath(step.Body, name, typedValue(f.Value, schema, name))
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
			placed = setBodyPath(step.Body, name, "${"+src+"."+ref.Path+"}")
		case f.SameAs != "":
			p.bindSameAs(step, id, name, f.SameAs)
		}
		if !placed {
			p.note("step %s: the contract sets %s, but that path does not exist in the %s body, so the "+
				"value was NOT placed — 'shrt contract lint' names what is wrong with the key", id, name, m.Name)
		}
	}
	p.grown = secondItems(step.Body, schema.Fields)
	p.noteSecondItems(id, p.grown)
	if len(step.Expect) == 0 {
		p.note("step %s: %s has no scalar or repeated response field, so nothing could be scaffolded to "+
			"assert. Write one — a step with no expect: passes whatever the server answers, and "+
			"'chain lint -strict' rejects it", id, m.FullName)
	}
	step.Expect = append(step.Expect, p.timestampExpectations(step, m, c, lib.DescriptionOf(lib.Domain(m.FullName)))...)
	p.pending = append(p.pending, pendingChecks{step: step, contract: c, schema: schema, fields: fields})
	if len(c.Exports) > 0 {
		step.Export = map[string]string{}
		for _, path := range sortedKeys(c.Exports) {
			step.Export[exportName(id, path)] = path
		}
	}
	return step
}

func (p *Plan) UnfilledCount() int {
	n := 0
	for _, note := range p.Notes {
		if strings.Contains(note, "has no usable value") {
			n++
		}
	}
	for _, pc := range p.pending {
		for _, name := range pc.contract.Required {
			if IsRequiredLiteral(name) {
				continue
			}
			if v, ok := bodyValue(pc.step.Body, name); ok {
				if _, empty := emptyDeclaredVar(p.Chain, v); empty {
					n++
				}
			}
		}
	}
	return n
}

func (p *Plan) note(format string, args ...any) {
	p.Notes = append(p.Notes, fmt.Sprintf(format, args...))
}

func setBodyPath(body map[string]any, path string, value any) bool {
	segs := chain.SplitPath(path)
	if len(segs) == 0 {
		return false
	}
	return setAt(body, segs, value)
}

func setAt(cur any, segs []string, value any) bool {
	seg := segs[0]
	last := len(segs) == 1
	if list, ok := cur.([]any); ok {
		if idx, err := strconv.Atoi(seg); err == nil {
			if idx < 0 || idx >= len(list) {
				return false
			}
			if last {
				list[idx] = value
				return true
			}
			return setAt(list[idx], segs[1:], value)
		}
		if len(list) == 0 {
			return false
		}
		placed := true
		for _, item := range list {
			placed = setAt(item, segs, value) && placed
		}
		return placed
	}
	m, ok := cur.(map[string]any)
	if !ok {
		return false
	}
	if last {
		m[seg] = value
		return true
	}
	return setAt(childContainer(m, seg), segs[1:], value)
}

const maxPlannedEntries = 100

func growLists(body map[string]any, path string) {
	growAt(body, chain.SplitPath(path))
}

func growAt(cur any, segs []string) any {
	if len(segs) == 0 {
		return cur
	}
	switch t := cur.(type) {
	case []any:
		idx, err := strconv.Atoi(segs[0])
		if err != nil {
			for i := range t {
				t[i] = growAt(t[i], segs)
			}
			return t
		}
		if len(t) == 0 || idx < 0 || idx >= maxPlannedEntries {
			return t
		}
		for len(t) <= idx {
			t = append(t, cloneBody(t[0]))
		}
		t[idx] = growAt(t[idx], segs[1:])
		return t
	case map[string]any:
		if next, ok := t[segs[0]]; ok {
			t[segs[0]] = growAt(next, segs[1:])
		}
		return t
	}
	return cur
}

func cloneBody(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			out[k] = cloneBody(item)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = cloneBody(item)
		}
		return out
	}
	return v
}

func indexDepth(path string) int {
	n := 0
	for _, seg := range chain.SplitPath(path) {
		if isIndexSegment(seg) {
			n++
		}
	}
	return n
}

func isIndexSegment(seg string) bool {
	if seg == "" {
		return false
	}
	for _, r := range seg {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func byIndexDepth(names []string) []string {
	out := append([]string{}, names...)
	sort.SliceStable(out, func(i, j int) bool { return indexDepth(out[i]) < indexDepth(out[j]) })
	return out
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

func resolveOrder(targets []string, lib *Library, cat *catalog.Catalog) ([]string, map[string][]string, []listProducer, error) {
	order := []string{}
	listed := []listProducer{}
	state := map[string]int{}
	edges := map[string][]string{}
	canon := func(node string) (string, string, string) {
		rpc, alias := SplitNode(node)
		if m, err := cat.Lookup(rpc); err == nil {
			rpc = m.FullName
		}
		if alias != "" {
			return rpc + "@" + alias, rpc, alias
		}
		return rpc, rpc, alias
	}

	var visit func(node, via string, trail []string) error
	visit = func(node, via string, trail []string) error {
		canonical, rpc, alias := canon(node)
		if via != "" && !containsString(edges[canonical], via) {
			edges[canonical] = append(edges[canonical], via)
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
				depCanonical, _, _ := canon(dep)
				label := shortNode(canonical) + " " + dependencyKind(c, alias, depCanonical, canon)
				if err := visit(dep, label, append(trail, canonical)); err != nil {
					return err
				}
			}
		}
		for _, requires := range lib.RequiredBy(rpc) {
			label := shortNode(requires) + " before: " + shortNode(rpc)
			if err := visit(requires, label, append(trail, canonical)); err != nil {
				return err
			}
		}
		if via == "the plan target" {
			m, _ := cat.Lookup(rpc)
			if msg := listedMessage(m); msg != "" && !nodesReturn(order, msg, cat) {
				lp := listProducer{list: canonical, msg: msg}
				if creator := creatorOf(msg, lib, cat); creator != "" && state[creator] == 0 {
					label := shortNode(canonical) + " lists " + shortMessage(msg) + " (inferred: no needs:)"
					if err := visit(creator, label, append(trail, canonical)); err != nil {
						return err
					}
					lp.creator = creator
				}
				listed = append(listed, lp)
			}
		}
		state[canonical] = 2
		order = append(order, canonical)
		return nil
	}
	for _, target := range targets {
		if err := visit(target, "the plan target", nil); err != nil {
			return nil, nil, nil, err
		}
	}
	return order, edges, listed, nil
}

func dependencyKind(c *RPCContract, alias, dep string, canon func(string) (string, string, string)) string {
	kinds := []string{}
	for _, n := range c.Needs {
		if got, _, _ := canon(n); got == dep {
			kinds = append(kinds, "needs:")
			break
		}
	}
	fields := c.FieldsFor(alias)
	for _, name := range sortedFieldNames(fields) {
		if ref, err := ParseRef(fields[name].From); err == nil {
			if got, _, _ := canon(ref.Node()); got == dep {
				kinds = append(kinds, name+" from:")
			}
		}
		if ref, err := ParseRef(fields[name].SameAs); err == nil {
			if got, _, _ := canon(ref.Node()); got == dep {
				kinds = append(kinds, name+" same_as:")
			}
		}
	}
	if len(kinds) == 0 {
		return "depends on it"
	}
	return strings.Join(kinds, ", ")
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
	read := readRoots(p.Chain)
	for _, step := range p.Chain.Steps {
		node, err := p.stepNode(step, read)
		if err != nil {
			return nil, err
		}
		steps.Content = append(steps.Content, node)
	}
	setMappingKey(doc, "steps", steps)
	return yaml.Marshal(doc)
}

func (p *Plan) stepNode(step *chain.Step, read map[string]bool) (*yaml.Node, error) {
	body := step.Body
	shallow := *step
	shallow.Body = nil
	shallow.Export = readExports(step.Export, read)
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
	if hasTemplate(seed) && !IsPlaceholder(seed, ScaffoldedBody) {
		setBodyPath(step.Body, field, "${steps."+producerID+".request."+ref.Path+"}")
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

var planVarRef = regexp.MustCompile(`\$\{vars\.([A-Za-z0-9_]+)\}`)

func (p *Plan) declareInterpolatedVars(missing []string) {
	interpolated, whole := map[string]bool{}, map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			for _, m := range planVarRef.FindAllStringSubmatchIndex(t, -1) {
				name := t[m[2]:m[3]]
				if m[0] == 0 && m[1] == len(t) {
					whole[name] = true
				} else {
					interpolated[name] = true
				}
			}
		case map[string]any:
			for _, item := range t {
				walk(item)
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	for _, st := range p.Chain.Steps {
		if st != nil {
			walk(st.Body)
		}
	}
	value := p.Chain.Name
	if value == "" {
		value = "planned"
	}
	declared := []string{}
	for _, name := range missing {
		if !interpolated[name] || whole[name] {
			continue
		}
		if p.Chain.Vars == nil {
			p.Chain.Vars = map[string]any{}
		}
		p.Chain.Vars[name] = value
		declared = append(declared, name)
	}
	if len(declared) > 0 {
		p.note("the chain reads ${vars.%s} inside the contract's value: entries, so it is declared under vars: as %q "+
			"and a first run needs no -var. A second run sends the same values: pass -var %s=<fresh> on every run "+
			"where they must be unique, as a sweep does",
			strings.Join(declared, "}, ${vars."), value, strings.Join(declared, "=<fresh> -var "))
	}
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
		if zeros := scaffoldZeros(pc.step.Body, pc.schema.Fields, pc.contract, pc.fields); len(zeros) > 0 {
			p.note("step %s: %s still %s the scaffold's numeric zero and %s not in required. If 0 is not the "+
				"test data you mean, set it — in the contract (value:) or in the chain; if 0 IS what you mean, say so "+
				"with value: \"0\", because a note: describes the field and does not silence this. This line is the only "+
				"warning you get: chain lint cannot tell the scaffold's 0 from a deliberate one",
				id, strings.Join(zeros, ", "), pluralVerb(len(zeros), "carries", "carry"), pluralIs(len(zeros)))
		}
		if facts := DeclaredFacts(pc.contract); len(facts) > 0 && AssertsOnlyVerdict(pc.step) {
			p.note("step %s %s", id, EnvelopeOnlyMessage(pc.step.Call, facts))
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
	kind := ""
	if f, ok := catalog.FieldAt(schema.Fields, chain.SplitPath(path)); ok && f != nil {
		kind = f.Kind
	}
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

func pluralVerb(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func scaffoldZeros(body map[string]any, schema []*catalog.Field, c *RPCContract, fields map[string]*FieldContract) []string {
	out := []string{}
	var walk func(v any, fs []*catalog.Field, path string)
	walk = func(v any, fs []*catalog.Field, path string) {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		for _, f := range fs {
			child, ok := m[f.Name]
			if !ok || IsPagingFieldName(f.Name) || f.MapKey != "" || f.JSONForm != "" {
				continue
			}
			at := join(path, f.Name)
			if list, isList := child.([]any); isList {
				for i, item := range list {
					itemPath := at
					if len(list) > 1 {
						itemPath = fmt.Sprintf("%s.%d", at, i)
					}
					if len(f.Fields) > 0 {
						walk(item, f.Fields, itemPath)
					} else if isNumericZero(item) && !contractSpeaksFor(c, fields, itemPath) {
						out = append(out, itemPath)
					}
				}
				continue
			}
			if len(f.Fields) > 0 {
				walk(child, f.Fields, at)
				continue
			}
			if isNumericZero(child) && !contractSpeaksFor(c, fields, at) {
				out = append(out, at)
			}
		}
	}
	walk(body, schema, "")
	return out
}

func isNumericZero(v any) bool {
	switch t := v.(type) {
	case string:
		return t == "0"
	case float64:
		return t == 0
	case int:
		return t == 0
	}
	return false
}

func contractSpeaksFor(c *RPCContract, fields map[string]*FieldContract, path string) bool {
	depth := len(chain.SplitPath(stripIndexes(path)))
	covers := func(key string) bool {
		return relatedKey(key, path) && len(chain.SplitPath(stripIndexes(key))) >= depth
	}
	for _, r := range c.Required {
		if !IsRequiredLiteral(r) && covers(r) {
			return true
		}
	}
	for name, f := range fields {
		if f != nil && covers(name) && hasValueSource(f) {
			return true
		}
	}
	return false
}

func stripIndexes(path string) string {
	kept := []string{}
	for _, seg := range chain.SplitPath(path) {
		if !isIndexSegment(seg) {
			kept = append(kept, seg)
		}
	}
	return strings.Join(kept, ".")
}

func anyContract(order []string, lib *Library) bool {
	for _, node := range order {
		rpc, _ := SplitNode(node)
		if _, ok := lib.Get(rpc); ok {
			return true
		}
	}
	return false
}
