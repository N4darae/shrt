package catalog

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
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
	if !m.Streaming() {
		return ""
	}
	return fmt.Sprintf("%s is a %s rpc and shrt is unary-only: a step is one POST of JSON to %s "+
		"answered by one response body, which cannot carry a stream. This rpc is out of scope for a chain — "+
		"reproduce the state it observes with the unary rpcs that write it",
		m.FullName, m.StreamKind(), m.Procedure())
}

func leadingComment(d protoreflect.Descriptor) string {
	loc := d.ParentFile().SourceLocations().ByDescriptor(d)
	parts := []string{cleanComment(loc.LeadingComments), cleanComment(loc.TrailingComments)}
	out := make([]string, 0, 2)
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " — ")
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
