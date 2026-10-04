package contract

import (
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
	"gopkg.in/yaml.v3"
)

const TodoMarker = "TODO"

type Producer struct {
	RPC  string
	Path string
}

func (p Producer) Ref() string { return p.RPC + RefSeparator + p.Path }

func ProducersOf(field string, methods []*catalog.Method, exclude string) []Producer {
	leaf := field[strings.LastIndex(field, ".")+1:]
	out := []Producer{}
	for _, m := range methods {
		if m.FullName == exclude || chain.IsReadOnlyCall(m.Name) || m.Streaming() {
			continue
		}
		if carriesLeaf(catalog.DescribeMessage(m.Input()).Fields, leaf) {
			continue
		}
		for _, path := range responsePathsTo(catalog.DescribeMessage(m.Output()).Fields, leaf, "") {
			out = append(out, Producer{RPC: m.FullName, Path: path})
		}
	}
	out = preferOwnID(out, leaf)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RPC != out[j].RPC {
			return out[i].RPC < out[j].RPC
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func preferOwnID(candidates []Producer, leaf string) []Producer {
	subject := idSubject(leaf)
	if subject == "" || len(candidates) < 2 {
		return candidates
	}
	own := []Producer{}
	for _, p := range candidates {
		segs := strings.Split(p.Path, ".")
		if len(segs) == 1 || strings.Join(namecase.Words(segs[len(segs)-2]), "_") == subject {
			own = append(own, p)
		}
	}
	if len(own) == 0 {
		return candidates
	}
	return own
}

func idSubject(leaf string) string {
	words := []string{}
	for _, w := range namecase.Words(leaf) {
		if !isIDWord(w) {
			words = append(words, w)
		}
	}
	return strings.Join(words, "_")
}

func responsePathsTo(fields []*catalog.Field, leaf, prefix string) []string {
	out := []string{}
	for _, f := range fields {
		if prefix == "" && f.Name == chain.EnvelopeField() {
			continue
		}
		path := join(prefix, f.Name)
		if f.Repeated || f.MapKey != "" {
			continue
		}
		if f.Kind == "message" || f.Kind == "group" {
			out = append(out, responsePathsTo(f.Fields, leaf, path)...)
			continue
		}
		if f.Name == leaf {
			out = append(out, path)
		}
	}
	return out
}

func carriesLeaf(fields []*catalog.Field, leaf string) bool {
	for _, f := range fields {
		if f.Name == leaf || carriesLeaf(f.Fields, leaf) {
			return true
		}
	}
	return false
}

func ScaffoldOverlay(domain string, methods []*catalog.Method, existing *Library, all []*catalog.Method) *yaml.Node {
	doc := &yaml.Node{Kind: yaml.MappingNode}
	put(doc, "apiVersion", scalar(OverlayAPIVersion))
	put(doc, "domain", scalar(domain))

	description := TodoMarker + ": what this domain is for, in two lines"
	if existing != nil {
		if prior := strings.TrimSpace(existing.DescriptionOf(domain)); prior != "" {
			description = prior
		}
	}
	put(doc, "description", scalar(description))

	if existing != nil {
		if prior := domainFailures(existing, domain); prior != nil {
			put(doc, "failures", prior)
		}
	}

	rpcs := &yaml.Node{Kind: yaml.MappingNode}
	for _, m := range methods {
		var prior *RPCContract
		if existing != nil {
			if c, ok := existing.Get(m.FullName); ok {
				prior = c
			}
		}
		put(rpcs, m.FullName, scaffoldRPC(m, prior, all))
	}
	put(doc, "rpcs", rpcs)
	return doc
}

func domainFailures(lib *Library, domain string) *yaml.Node {
	for _, o := range lib.Overlays {
		if o.Domain != domain || len(o.Failures) == 0 {
			continue
		}
		node := &yaml.Node{}
		if err := node.Encode(o.Failures); err == nil {
			return node
		}
	}
	return nil
}

func scaffoldRPC(m *catalog.Method, prior *RPCContract, all []*catalog.Method) *yaml.Node {
	if prior != nil {
		node := &yaml.Node{}
		if err := node.Encode(prior); err == nil {
			carryRequiredTodo(node, prior)
			if todo := effectsTodo(m, all); todo != "" && len(prior.Effects) == 0 && prior.IsUnfilled("effects") {
				put(node, "effects", scalar(todo))
			}
			addNewFields(node, prior, m, all)
			return node
		}
	}
	readOnly := chain.IsReadOnlyCall(m.Name)
	summary, hint, refusal, todo := m.Doc, readOnlyHint, "", ""
	if !readOnly {
		hint, refusal, todo = fieldHint, m.StreamRefusal(), effectsTodo(m, all)
	}
	if summary == "" && readOnly {
		summary = fmt.Sprintf("%s: what %s returns, and which request fields the handler actually requires",
			TodoMarker, m.Name)
	} else if summary == "" {
		summary = TodoMarker + ": what this rpc does and when to call it"
	}
	node := &yaml.Node{Kind: yaml.MappingNode}
	put(node, "summary", scalar(summary))
	if refusal != "" {
		put(node, "note", scalar(refusal))
	}
	put(node, "required", requiredTodo())
	if fields := scaffoldFields(m, all, hint); len(fields.Content) > 0 {
		put(node, "fields", fields)
	}
	if todo != "" {
		put(node, "effects", scalar(todo))
	}
	if exports := exportsNode(m); readOnly || len(exports.Content) > 0 {
		put(node, "exports", exports)
	}
	put(node, "status", scalar(StatusDraft))
	return node
}

func readOnlyHint(f *catalog.Field) string {
	switch {
	case f.Name == "pagination":
		return "page_size 0 falls back to the server default"
	case len(f.EnumValues) > 0:
		return TodoMarker + ": filter or required? does the handler treat " + f.EnumValues[0] +
			" as no filter, or reject it — and if it filters, where does the value come from"
	default:
		return TodoMarker + ": filter or required? and if it filters, where does the value come from"
	}
}

const RequiredTodoText = TodoMarker + ": which fields the server rejects without, or " +
	RequiredNone + " if it rejects nothing"

func requiredTodo() *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	n.Content = append(n.Content, scalar(RequiredTodoText))
	return n
}

func addNewFields(node *yaml.Node, prior *RPCContract, m *catalog.Method, all []*catalog.Method) {
	known := map[string]bool{}
	for key := range prior.Fields {
		known[headSegment(key)] = true
	}
	for _, key := range prior.Required {
		if !IsRequiredLiteral(key) {
			known[headSegment(key)] = true
		}
	}
	for _, alias := range prior.Aliases {
		if alias == nil {
			continue
		}
		for key := range alias.Fields {
			known[headSegment(key)] = true
		}
	}
	hint := fieldHint
	if chain.IsReadOnlyCall(m.Name) {
		hint = readOnlyHint
	}
	added := &yaml.Node{Kind: yaml.MappingNode}
	for _, f := range catalog.DescribeMessage(m.Input()).Fields {
		if known[f.Name] {
			nested := &yaml.Node{Kind: yaml.MappingNode}
			scaffoldNested(nested, f, f.Name, m, all, 1, false, hint)
			for i := 0; i+1 < len(nested.Content); i += 2 {
				if _, have := prior.Fields[nested.Content[i].Value]; !have && !priorAliasField(prior, nested.Content[i].Value) {
					added.Content = append(added.Content, nested.Content[i], nested.Content[i+1])
				}
			}
			continue
		}
		put(added, f.Name, scaffoldField(f, m, all, hint))
		scaffoldNested(added, f, f.Name, m, all, 1, false, hint)
	}
	if len(added.Content) == 0 {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "fields" && node.Content[i+1].Kind == yaml.MappingNode {
			node.Content[i+1].Content = append(node.Content[i+1].Content, added.Content...)
			return
		}
	}
	at := len(node.Content)
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "required" {
			at = i + 2
		}
	}
	rest := append([]*yaml.Node{scalar("fields"), added}, node.Content[at:]...)
	node.Content = append(node.Content[:at:at], rest...)
}

func priorAliasField(prior *RPCContract, key string) bool {
	for _, alias := range prior.Aliases {
		if alias == nil {
			continue
		}
		if _, ok := alias.Fields[key]; ok {
			return true
		}
	}
	return false
}

func carryRequiredTodo(node *yaml.Node, prior *RPCContract) {
	if !prior.IsUnfilled("required") || len(prior.Required) > 0 {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "required" {
			node.Content[i+1] = requiredTodo()
			return
		}
	}
}

func scaffoldFields(m *catalog.Method, all []*catalog.Method, hint func(*catalog.Field) string) *yaml.Node {
	fields := &yaml.Node{Kind: yaml.MappingNode}
	for _, f := range catalog.DescribeMessage(m.Input()).Fields {
		put(fields, f.Name, scaffoldField(f, m, all, hint))
		scaffoldNested(fields, f, f.Name, m, all, 1, false, hint)
	}
	return fields
}

const nestedIDDepth = 3

func scaffoldNested(fields *yaml.Node, parent *catalog.Field, path string, m *catalog.Method, all []*catalog.Method, depth int, inItem bool, hint func(*catalog.Field) string) {
	if depth > nestedIDDepth || parent.MapKey != "" || (parent.Kind != "message" && parent.Kind != "group") {
		return
	}
	inItem = inItem || parent.Repeated
	for _, f := range parent.Fields {
		child := path + "." + f.Name
		leaf := f.Kind != "message" && f.Kind != "group"
		if IsEntityIDField(f.Name) && leaf && !f.Repeated {
			if len(ProducersOf(f.Name, all, m.FullName)) > 0 {
				put(fields, child, scaffoldField(f, m, all, fieldHint))
			} else if inItem {
				put(fields, child, scaffoldField(f, m, all, hint))
			}
			continue
		}
		if inItem && (leaf || f.MapKey != "" || f.JSONForm != "") {
			put(fields, child, scaffoldField(f, m, all, hint))
			continue
		}
		scaffoldNested(fields, f, child, m, all, depth+1, inItem, hint)
	}
}

func inferredFroms(m *catalog.Method, all []*catalog.Method) map[string]string {
	out := map[string]string{}
	fields := scaffoldFields(m, all, fieldHint)
	for i := 0; i+1 < len(fields.Content); i += 2 {
		if from := mappingValue(fields.Content[i+1], "from"); from != nil {
			out[fields.Content[i].Value] = from.Value
		}
	}
	return out
}

func (p *Plan) wireInferredIDs(step *chain.Step, m *catalog.Method, fields map[string]*FieldContract) {
	if p.noun == "" {
		return
	}
	earlier := map[string]bool{}
	for _, st := range p.Chain.Steps {
		earlier[st.Call] = true
	}
	froms := inferredFroms(m, p.cat.Methods())
	for _, path := range sortedKeys(froms) {
		if f := fields[path]; f != nil && (f.Value != "" || f.From != "" || f.SameAs != "") {
			continue
		}
		ref, err := ParseRef(froms[path])
		if err != nil || !earlier[ref.Node()] {
			continue
		}
		setBodyPath(step.Body, path, "${"+p.stepOf[ref.Node()]+"."+ref.Path+"}")
	}
}

func withDoc(text string, f *catalog.Field) string {
	if f == nil || strings.TrimSpace(f.Doc) == "" || !IsTodo(text) {
		return text
	}
	return text + " (proto: " + strings.TrimSpace(f.Doc) + ")"
}

func scaffoldField(f *catalog.Field, m *catalog.Method, all []*catalog.Method, hint func(*catalog.Field) string) *yaml.Node {
	entry := &yaml.Node{Kind: yaml.MappingNode}
	if strings.Contains(f.Name, "idempotency") {
		put(entry, "value", scalar("${uuid}"))
		put(entry, "note", scalar("fresh per run; a replay with the same key is the idempotency path"))
		return entry
	}
	if IsEntityIDField(f.Name) {
		producers := ProducersOf(f.Name, all, m.FullName)
		switch len(producers) {
		case 1:
			put(entry, "from", scalar(producers[0].Ref()))
			put(entry, "checked_by", scalar(TodoMarker+": fk, app_lookup or none"))
			return entry
		case 0:
		default:
			refs := make([]string, 0, len(producers))
			for _, p := range producers {
				refs = append(refs, p.Ref())
			}
			put(entry, "note", scalar(TodoMarker+": pick a from — candidates are "+strings.Join(refs, ", ")))
			return entry
		}
	}
	put(entry, "note", scalar(withDoc(hint(f), f)))
	return entry
}

func exportsNode(m *catalog.Method) *yaml.Node {
	out := catalog.DescribeMessage(m.Output())
	exports := &yaml.Node{Kind: yaml.MappingNode}
	for _, f := range out.Fields {
		if f.Name == chain.EnvelopeField() {
			continue
		}
		if f.Kind == "message" || f.Kind == "group" {
			if !f.Repeated {
				continue
			}
		}
		put(exports, f.Name, scalar(withDoc(TodoMarker+": why a later step would need this, or move it to terminal: if nothing consumes it", f)))
	}
	return exports
}

func fieldHint(f *catalog.Field) string {
	parts := []string{}
	if len(f.EnumValues) > 0 {
		parts = append(parts, "one of "+strings.Join(f.EnumValues, ", "))
	}
	if f.Repeated {
		parts = append(parts, "repeated")
	}
	if len(parts) == 0 {
		return TodoMarker + ": where this value comes from, or what you checked if you could not tell; keep the entry"
	}
	return TodoMarker + ": " + strings.Join(parts, "; ")
}

func Domains(methods []*catalog.Method) map[string][]*catalog.Method {
	out := map[string][]*catalog.Method{}
	for _, m := range methods {
		out[DomainOf(m)] = append(out[DomainOf(m)], m)
	}
	return out
}

var reverseDNSRoot = map[string]bool{
	"com": true, "org": true, "net": true, "io": true, "dev": true,
	"co": true, "gov": true, "edu": true, "app": true, "cloud": true,
}

func DomainOf(m *catalog.Method) string {
	parts := strings.Split(m.Service, ".")
	if len(parts) == 0 || parts[0] == "" {
		return "default"
	}
	pkg := parts[:len(parts)-1]
	for len(pkg) > 0 && isVersionSegment(pkg[len(pkg)-1]) {
		pkg = pkg[:len(pkg)-1]
	}
	if len(pkg) >= 3 && reverseDNSRoot[pkg[0]] {
		pkg = pkg[1:]
	}
	switch {
	case len(pkg) >= 2:
		return pkg[1]
	case len(pkg) == 1:
		return pkg[0]
	default:
		return parts[0]
	}
}

func PackageRootOf(m *catalog.Method) string {
	parts := strings.Split(m.Service, ".")
	pkg := parts[:max(len(parts)-1, 0)]
	for len(pkg) > 0 && isVersionSegment(pkg[len(pkg)-1]) {
		pkg = pkg[:len(pkg)-1]
	}
	if len(pkg) >= 2 {
		return strings.Join(pkg[:2], ".")
	}
	if len(pkg) == 1 {
		return pkg[0]
	}
	return m.Service
}

func isVersionSegment(s string) bool {
	if len(s) < 2 || s[0] != 'v' || s[1] < '0' || s[1] > '9' {
		return false
	}
	rest := s[1:]
	for len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
		rest = rest[1:]
	}
	for _, suffix := range []string{"alpha", "beta", "test", "p"} {
		if strings.HasPrefix(rest, suffix) {
			rest = rest[len(suffix):]
			break
		}
	}
	for len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
		rest = rest[1:]
	}
	return rest == ""
}

func DomainNames(methods []*catalog.Method) []string {
	seen := Domains(methods)
	return sortedKeys(seen)
}

func scalar(v string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	if strings.Contains(v, "\n") {
		n.Style = yaml.LiteralStyle
	} else if strings.Contains(v, LegacyRefSeparator) || strings.Contains(v, ":") {
		n.Style = yaml.SingleQuotedStyle
	}
	return n
}

func put(mapping *yaml.Node, key string, value *yaml.Node) {
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	mapping.Content = append(mapping.Content, k, value)
}

func RenderOverlay(node *yaml.Node) ([]byte, error) {
	raw, err := yaml.Marshal(node)
	if err != nil {
		return nil, fmt.Errorf("render overlay: %w", err)
	}
	return raw, nil
}

func ScanTodos(domain string, raw []byte) []Issue {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	if len(doc.Content) == 0 {
		return nil
	}
	issues := []Issue{}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Value != "rpcs" {
			collectTodos(domain, "", key.Value, value, &issues)
			continue
		}
		for j := 0; j+1 < len(value.Content); j += 2 {
			collectTodos(domain, value.Content[j].Value, "", value.Content[j+1], &issues)
		}
	}
	return issues
}

func collectTodos(domain, rpc, path string, node *yaml.Node, issues *[]Issue) {
	report := func(field, text string) {
		*issues = append(*issues, Issue{
			Domain: domain, RPC: rpc, Field: field, Severity: SeverityWarn,
			Message: "unfilled " + TodoMarker + ": " + cleanTodo(text),
		})
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if text := todoText(node.Value, node.LineComment); text != "" {
			report(path, text)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			child := join(path, key.Value)
			if text := todoText("", key.LineComment); text != "" {
				report(child, text)
			}
			collectTodos(domain, rpc, child, value, issues)
		}
	case yaml.SequenceNode:
		if text := todoText("", node.LineComment); text != "" {
			report(path, text)
		}
		for i, item := range node.Content {
			if item.Kind == yaml.ScalarNode {
				if text := todoText(item.Value, item.LineComment); text != "" {
					report(path, text)
					continue
				}
			}
			collectTodos(domain, rpc, fmt.Sprintf("%s.%d", path, i), item, issues)
		}
	}
}

func todoText(value, comment string) string {
	if IsTodo(value) {
		return value
	}
	if IsTodo(comment) {
		return comment
	}
	return ""
}

func IsTodo(text string) bool {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "#"))
	if !strings.HasPrefix(trimmed, TodoMarker) {
		return false
	}
	rest := strings.TrimSpace(trimmed[len(TodoMarker):])
	return rest == "" || strings.HasPrefix(rest, ":")
}

func cleanTodo(raw string) string {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "#")
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, TodoMarker)
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), ":"))
	return FirstSentence(text)
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
