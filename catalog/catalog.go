package catalog

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/namecase"
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
		if m := c.methods[k]; strings.EqualFold(m.Name, ref) || strings.EqualFold(shortService(m.Service)+"/"+m.Name, ref) {
			matches = append(matches, c.methods[k])
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, fmt.Errorf("unknown rpc %q: %w%s", ref, ErrNotFound, c.closestRPC(ref))
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.FullName)
		}
		return nil, fmt.Errorf("ambiguous rpc %q, candidates: %s", ref, strings.Join(names, ", "))
	}
}

func (c *Catalog) closestRPC(ref string) string {
	byForm := map[string][]string{}
	forms := make([]string, 0, len(c.order))
	for _, k := range c.order {
		m := c.methods[k]
		form := m.Name
		switch {
		case strings.Contains(ref, ".") && strings.Contains(ref, "/"):
			form = m.FullName
		case strings.Contains(ref, "/"):
			form = shortService(m.Service) + "/" + m.Name
		}
		if _, seen := byForm[form]; !seen {
			forms = append(forms, form)
		}
		byForm[form] = append(byForm[form], m.FullName)
	}
	near := []string{}
	for _, form := range namecase.Closest(ref, forms, 3) {
		if len(namecase.Closest(rpcName(ref), []string{rpcName(form)}, 1)) > 0 {
			near = append(near, form)
		}
	}
	if len(near) == 0 {
		return ""
	}
	quoted := []string{}
	for _, form := range near {
		for _, full := range byForm[form] {
			quoted = append(quoted, strconv.Quote(full))
		}
	}
	return " (did you mean " + strings.Join(quoted, " or ") + "?)"
}

func rpcName(ref string) string {
	return ref[strings.LastIndexAny(ref, "/.")+1:]
}

func (c *Catalog) SuggestRPC(ref string) string {
	return c.closestRPC(strings.TrimPrefix(strings.TrimSpace(ref), "/"))
}

func shortService(full string) string {
	return full[strings.LastIndex(full, ".")+1:]
}
