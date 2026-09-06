package contract

import (
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"gopkg.in/yaml.v3"
)

const TodoMarker = "TODO"

type Producer struct {
	RPC  string
	Path string
}

func (p Producer) Ref() string { return p.RPC + RefSeparator + p.Path }

func ProducersOf(field string, methods []*catalog.Method, exclude string) []Producer {
	out := []Producer{}
	for _, m := range methods {
		if m.FullName == exclude || isReadOnly(m.Name) {
			continue
		}
		for _, f := range catalog.DescribeMessage(m.Output()).Fields {
			if f.Name == field && f.Kind != "message" && f.Kind != "group" && !f.Repeated {
				out = append(out, Producer{RPC: m.FullName, Path: f.Name})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RPC < out[j].RPC })
	return out
}

func isReadOnly(name string) bool {
	return chain.IsReadOnlyCall(name)
}

func ScaffoldOverlay(domain string, methods []*catalog.Method, existing *Library, all []*catalog.Method) *yaml.Node {
	doc := &yaml.Node{Kind: yaml.MappingNode}
	put(doc, "apiVersion", scalar(OverlayAPIVersion), "")
	put(doc, "domain", scalar(domain), "")

	description := TodoMarker + ": what this domain is for, in two lines"
	if existing != nil {
		if prior := strings.TrimSpace(existing.DescriptionOf(domain)); prior != "" {
			description = prior
		}
	}
	put(doc, "description", scalar(description), "")

	if existing != nil {
		if prior := domainFailures(existing, domain); prior != nil {
			put(doc, "failures", prior, "")
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
		put(rpcs, m.FullName, scaffoldRPC(m, prior, all), m.Doc)
	}
	put(doc, "rpcs", rpcs, "")
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
			reattachFieldDocs(node, m)
			return node
		}
	}
	if isReadOnly(m.Name) {
		return scaffoldReadOnly(m, all)
	}
	return scaffoldWrite(m, all)
}

func scaffoldReadOnly(m *catalog.Method, all []*catalog.Method) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}
	summary := m.Doc
	if summary == "" {
		summary = fmt.Sprintf("%s: what %s returns, and which request fields the handler actually requires",
			TodoMarker, m.Name)
	}
	put(node, "summary", scalar(summary), "")
	put(node, "required", seq(TodoMarker+": which fields the server rejects without, or "+
		RequiredNone+" if it rejects nothing"), "")

	schema := catalog.DescribeMessage(m.Input())
	fields := &yaml.Node{Kind: yaml.MappingNode}
	for _, f := range schema.Fields {
		put(fields, f.Name, scaffoldField(f, m, all, readOnlyHint), f.Doc)
	}
	if len(fields.Content) > 0 {
		put(node, "fields", fields, "")
	}
	put(node, "exports", exportsNode(m), "")
	put(node, "status", scalar(StatusDraft), "")
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

func scaffoldWrite(m *catalog.Method, all []*catalog.Method) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}
	summary := m.Doc
	if summary == "" {
		summary = TodoMarker + ": what this rpc does and when to call it"
	}
	put(node, "summary", scalar(summary), "")
	put(node, "required", seq(TodoMarker+": which fields the server rejects without, or "+
		RequiredNone+" if it rejects nothing"), "")

	schema := catalog.DescribeMessage(m.Input())
	fields := &yaml.Node{Kind: yaml.MappingNode}
	for _, f := range schema.Fields {
		put(fields, f.Name, scaffoldField(f, m, all, fieldHint), f.Doc)
	}
	if len(fields.Content) > 0 {
		put(node, "fields", fields, "")
	}
	if exports := exportsNode(m); len(exports.Content) > 0 {
		put(node, "exports", exports, "")
	}
	put(node, "status", scalar(StatusDraft), "")
	return node
}

func scaffoldField(f *catalog.Field, m *catalog.Method, all []*catalog.Method, hint func(*catalog.Field) string) *yaml.Node {
	entry := &yaml.Node{Kind: yaml.MappingNode}
	if strings.Contains(f.Name, "idempotency") {
		put(entry, "value", scalar("${uuid}"), "")
		put(entry, "note", scalar("fresh per run; a replay with the same key is the idempotency path"), "")
		return entry
	}
	if IsEntityIDField(f.Name) {
		producers := ProducersOf(f.Name, all, m.FullName)
		switch len(producers) {
		case 1:
			put(entry, "from", scalar(producers[0].Ref()), "")
			put(entry, "checked_by", scalar(TodoMarker+": fk, app_lookup or none"), "")
			return entry
		case 0:
		default:
			refs := make([]string, 0, len(producers))
			for _, p := range producers {
				refs = append(refs, p.Ref())
			}
			put(entry, "note", scalar(TodoMarker+": pick a from — candidates are "+strings.Join(refs, ", ")), "")
			return entry
		}
	}
	put(entry, "note", scalar(hint(f)), "")
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
		put(exports, f.Name, scalar(TodoMarker+": why a later step would need this, or delete the line"), f.Doc)
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
		return TodoMarker + ": where this value comes from, or delete the entry"
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
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
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

func seq(comment string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	n.LineComment = comment
	return n
}

func put(mapping *yaml.Node, key string, value *yaml.Node, comment string) {
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	if comment != "" {
		k.HeadComment = comment
	}
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
	return firstSentence(text)
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func reattachFieldDocs(node *yaml.Node, m *catalog.Method) {
	reattachDocs(mappingValue(node, "fields"), catalog.DescribeMessage(m.Input()).Fields)
	reattachDocs(mappingValue(node, "exports"), catalog.DescribeMessage(m.Output()).Fields)
}

func reattachDocs(mapping *yaml.Node, fields []*catalog.Field) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	docs := map[string]string{}
	for _, f := range fields {
		if f.Doc != "" {
			docs[f.Name] = f.Doc
		}
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		if key.HeadComment != "" || key.LineComment != "" {
			continue
		}
		if doc, ok := docs[key.Value]; ok {
			key.HeadComment = doc
		}
	}
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
