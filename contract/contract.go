package contract

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"gopkg.in/yaml.v3"
)

type Contract struct {
	RPC       string          `json:"rpc" yaml:"rpc"`
	Procedure string          `json:"procedure" yaml:"procedure"`
	ProtoFile string          `json:"proto_file" yaml:"proto_file"`
	Doc       string          `json:"doc,omitempty" yaml:"doc,omitempty"`
	Streaming string          `json:"streaming,omitempty" yaml:"streaming,omitempty"`
	Request   *catalog.Schema `json:"request" yaml:"request"`
	Response  *catalog.Schema `json:"response" yaml:"response"`
	Example   map[string]any  `json:"example_body" yaml:"example_body"`
	StepYAML  string          `json:"step_yaml" yaml:"step_yaml"`
	Exports   []ExportHint    `json:"export_hints,omitempty" yaml:"export_hints,omitempty"`
}

type ExportHint struct {
	Path string `json:"path" yaml:"path"`
	Kind string `json:"kind" yaml:"kind"`
	Doc  string `json:"doc,omitempty" yaml:"doc,omitempty"`
}

func For(m *catalog.Method) *Contract { return ForPreferring(m, nil) }

func ForCurated(m *catalog.Method, lib *Library, cat *catalog.Catalog) *Contract {
	c, _ := ForCuratedWithNotes(m, lib, cat)
	return c
}

func ForCuratedWithNotes(m *catalog.Method, lib *Library, cat *catalog.Catalog) (*Contract, []string) {
	c := ForPreferring(m, ArmedOneofMembersOf(lib, m.FullName))
	if lib == nil || cat == nil {
		return c, nil
	}
	nodes, notes, err := ScaffoldSteps([]string{m.FullName}, []string{defaultID(m.Name)}, lib, cat)
	if err != nil || len(nodes) == 0 {
		return c, nil
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Content: nodes[:1]}
	if raw, err := yaml.Marshal(seq); err == nil {
		c.StepYAML = string(raw)
	}
	return c, notes
}

func ForPreferring(m *catalog.Method, prefer []string) *Contract {
	c := &Contract{
		RPC:       m.FullName,
		Procedure: m.Procedure(),
		ProtoFile: m.File,
		Doc:       m.Doc,
		Streaming: m.StreamKind(),
		Request:   catalog.DescribeMessage(m.Input()),
		Response:  catalog.DescribeMessage(m.Output()),
		Example:   catalog.ScaffoldWith(m.Input(), catalog.ScaffoldOptions{Prefer: prefer}),
	}
	c.Exports = exportHints(c.Response.Fields, "")
	c.StepYAML = stepYAML(m, prefer)
	return c
}

func exportHints(fields []*catalog.Field, prefix string) []ExportHint {
	out := []ExportHint{}
	for _, f := range fields {
		path := f.Name
		if prefix != "" {
			path = prefix + "." + f.Name
		}
		if len(f.Fields) > 0 {
			next := path
			if f.Repeated {
				next = path + ".0"
			}
			out = append(out, exportHints(f.Fields, next)...)
			continue
		}
		if f.Kind == "message" || f.Kind == "group" {
			continue
		}
		kind := f.Kind
		if f.Repeated {
			kind = "repeated " + kind
		}
		out = append(out, ExportHint{Path: path, Kind: kind, Doc: f.Doc})
	}
	return out
}

func StepNodePreferring(m *catalog.Method, id string, prefer []string) *yaml.Node {
	if id == "" {
		id = defaultID(m.Name)
	}
	step := &chain.Step{
		ID:     id,
		Call:   m.FullName,
		Expect: SuccessExpectation(m),
	}
	node := &yaml.Node{}
	if err := node.Encode(step); err != nil {
		return nil
	}
	schema := catalog.DescribeMessage(m.Input())
	inject(node, bodyNode(schema.Fields, catalog.ScaffoldWith(m.Input(), catalog.ScaffoldOptions{Prefer: prefer})))
	return node
}

func stepYAML(m *catalog.Method, prefer []string) string {
	node := StepNodePreferring(m, "", prefer)
	if node == nil {
		return ""
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{node}}
	raw, err := yaml.Marshal(seq)
	if err != nil {
		return ""
	}
	return string(raw)
}

func inject(mapping *yaml.Node, body *yaml.Node) {
	if mapping == nil || body == nil {
		return
	}
	at := len(mapping.Content)
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "call" {
			at = i + 2
			break
		}
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "body"}
	tail := append([]*yaml.Node{}, mapping.Content[at:]...)
	mapping.Content = append(mapping.Content[:at], append([]*yaml.Node{key, body}, tail...)...)
}

func bodyNode(fields []*catalog.Field, example map[string]any) *yaml.Node {
	if len(fields) == 0 {
		return nil
	}
	out := &yaml.Node{Kind: yaml.MappingNode}
	for _, f := range fields {
		v, ok := example[f.Name]
		if !ok {
			continue
		}
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: f.Name}
		val := &yaml.Node{}
		if nested, isMap := v.(map[string]any); isMap && len(f.Fields) > 0 {
			val = bodyNode(f.Fields, nested)
			if val == nil {
				val = &yaml.Node{Kind: yaml.MappingNode}
			}
		} else if err := val.Encode(v); err != nil {
			continue
		}
		out.Content = append(out.Content, key, val)
	}
	return out
}

func defaultID(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + 32)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (c *Contract) StepID() string { return defaultID(lastSegment(c.RPC)) }

func lastSegment(fqn string) string {
	if i := strings.LastIndex(fqn, "/"); i >= 0 {
		return fqn[i+1:]
	}
	return fqn
}

func (c *Contract) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "RPC        %s\n", c.RPC)
	fmt.Fprintf(&b, "PROCEDURE  %s\n", c.Procedure)
	fmt.Fprintf(&b, "PROTO      %s\n", c.ProtoFile)
	if c.Doc != "" {
		fmt.Fprintf(&b, "DOC        %s\n", c.Doc)
	}
	if c.Streaming != "" {
		how := "OUT OF SCOPE: 'shrt chain lint' errors on a step that calls it"
		if c.Streaming == catalog.StreamKindServer {
			how = "a step records its first message as messages.0"
		}
		fmt.Fprintf(&b, "STREAMING  %s — %s\n", c.Streaming, how)
	}
	fmt.Fprintf(&b, "\nREQUEST %s", c.Request.Text())
	fmt.Fprintf(&b, "\nRESPONSE %s", c.Response.Text())
	fmt.Fprintf(&b, "\nCHAIN STEP\n%s", c.StepYAML)
	if len(c.Exports) > 0 {
		b.WriteString("\nEXPORTABLE PATHS\n")
		for _, e := range c.Exports {
			fmt.Fprintf(&b, "  %-40s %s\n", e.Path, e.Kind)
		}
	}
	return b.String()
}
