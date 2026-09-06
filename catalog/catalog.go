package catalog

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type Catalog struct {
	files   *protoregistry.Files
	types   *protoregistry.Types
	methods map[string]*Method
	order   []string
}

func Load(path string) (*Catalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read descriptor %q: %w", path, err)
	}
	return Parse(raw)
}

func Parse(raw []byte) (*Catalog, error) {
	fds := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(raw, fds); err != nil {
		return nil, fmt.Errorf("decode descriptor set: %w", err)
	}
	files, err := protodesc.NewFiles(fds)
	if err != nil {
		return nil, fmt.Errorf("build file registry: %w", err)
	}
	c := &Catalog{
		files:   files,
		types:   &protoregistry.Types{},
		methods: map[string]*Method{},
	}
	c.index()
	return c, nil
}

func (c *Catalog) index() {
	c.files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		registerMessages(c.types, fd.Messages())
		registerEnums(c.types, fd.Enums())
		services := fd.Services()
		for i := range services.Len() {
			svc := services.Get(i)
			methods := svc.Methods()
			for j := range methods.Len() {
				m := newMethod(svc, methods.Get(j))
				c.methods[m.FullName] = m
				c.order = append(c.order, m.FullName)
			}
		}
		return true
	})
	sort.Strings(c.order)
}

func registerMessages(types *protoregistry.Types, msgs protoreflect.MessageDescriptors) {
	for i := range msgs.Len() {
		md := msgs.Get(i)
		_ = types.RegisterMessage(dynamicpb.NewMessageType(md))
		registerMessages(types, md.Messages())
		registerEnums(types, md.Enums())
	}
}

func registerEnums(types *protoregistry.Types, enums protoreflect.EnumDescriptors) {
	for i := range enums.Len() {
		_ = types.RegisterEnum(dynamicpb.NewEnumType(enums.Get(i)))
	}
}

func (c *Catalog) Types() *protoregistry.Types { return c.types }

func (c *Catalog) Methods() []*Method {
	out := make([]*Method, 0, len(c.order))
	for _, k := range c.order {
		out = append(out, c.methods[k])
	}
	return out
}

func (c *Catalog) Services() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, k := range c.order {
		s := c.methods[k].Service
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (c *Catalog) Lookup(ref string) (*Method, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "/")
	if m, ok := c.methods[ref]; ok {
		return m, nil
	}
	matches := []*Method{}
	for _, k := range c.order {
		if matchesRef(c.methods[k], ref) {
			matches = append(matches, c.methods[k])
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, fmt.Errorf("unknown rpc %q: %w", ref, ErrNotFound)
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.FullName)
		}
		return nil, fmt.Errorf("ambiguous rpc %q, candidates: %s", ref, strings.Join(names, ", "))
	}
}

func matchesRef(m *Method, ref string) bool {
	if strings.EqualFold(m.Name, ref) {
		return true
	}
	short := shortService(m.Service) + "/" + m.Name
	return strings.EqualFold(short, ref)
}

func shortService(full string) string {
	if i := strings.LastIndex(full, "."); i >= 0 {
		return full[i+1:]
	}
	return full
}
