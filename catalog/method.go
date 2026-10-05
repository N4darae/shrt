package catalog

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

var ErrNotFound = errors.New("not found")

const (
	StreamKindClient = "client-streaming"
	StreamKindServer = "server-streaming"
	StreamKindBidi   = "bidirectional-streaming"
)

type Method struct {
	FullName string
	Service  string
	Name     string
	File     string
	Doc      string

	ClientStreaming bool
	ServerStreaming bool

	desc protoreflect.MethodDescriptor
}

func newMethod(svc protoreflect.ServiceDescriptor, md protoreflect.MethodDescriptor) *Method {
	return &Method{
		FullName:        string(svc.FullName()) + "/" + string(md.Name()),
		Service:         string(svc.FullName()),
		Name:            string(md.Name()),
		File:            svc.ParentFile().Path(),
		Doc:             leadingComment(md),
		ClientStreaming: md.IsStreamingClient(),
		ServerStreaming: md.IsStreamingServer(),
		desc:            md,
	}
}

func (m *Method) Deprecated() bool {
	if opts, ok := m.desc.Options().(*descriptorpb.MethodOptions); ok && opts.GetDeprecated() {
		return true
	}
	svc, ok := m.desc.Parent().(protoreflect.ServiceDescriptor)
	if !ok {
		return false
	}
	opts, ok := svc.Options().(*descriptorpb.ServiceOptions)
	return ok && opts.GetDeprecated()
}

func (m *Method) Procedure() string { return "/" + m.FullName }

func (m *Method) Input() protoreflect.MessageDescriptor  { return m.desc.Input() }
func (m *Method) Output() protoreflect.MessageDescriptor { return m.desc.Output() }

func (m *Method) Descriptor() protoreflect.MethodDescriptor { return m.desc }

func (m *Method) Streaming() bool { return m.ClientStreaming || m.ServerStreaming }

func (m *Method) StreamKind() string {
	switch {
	case m.ClientStreaming && m.ServerStreaming:
		return StreamKindBidi
	case m.ClientStreaming:
		return StreamKindClient
	case m.ServerStreaming:
		return StreamKindServer
	default:
		return ""
	}
}

func (m *Method) StreamRefusal() string {
	if !m.ClientStreaming {
		return ""
	}
	return fmt.Sprintf("%s is a %s rpc; a chain calls unary and server-streaming rpcs only, so reach its state with the rpcs that write it",
		m.FullName, m.StreamKind())
}

const StreamMessages = "messages"

func (m *Method) Response() *Schema {
	s := DescribeMessage(m.Output())
	if !m.ServerStreaming {
		return s
	}
	return &Schema{Message: s.Message, Doc: s.Doc, Fields: []*Field{{
		Name: StreamMessages, JSONName: StreamMessages, Number: 1, Kind: "message", Repeated: true, Message: s.Message, Fields: s.Fields,
	}}}
}

func (m *Method) ResponseSample() map[string]any {
	sample := Scaffold(m.Output())
	if !m.ServerStreaming {
		return sample
	}
	return map[string]any{StreamMessages: []any{sample}}
}

func leadingComment(d protoreflect.Descriptor) string {
	loc := d.ParentFile().SourceLocations().ByDescriptor(d)
	parts := []string{cleanComment(loc.LeadingComments), cleanComment(loc.TrailingComments)}
	return strings.Join(slices.DeleteFunc(parts, func(p string) bool { return p == "" }), " — ")
}

func cleanComment(raw string) string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, " ")
}
