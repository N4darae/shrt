package catalog

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

const defaultSchemaDepth = 6

type Schema struct {
	Message string   `json:"message" yaml:"message"`
	Doc     string   `json:"doc,omitempty" yaml:"doc,omitempty"`
	Fields  []*Field `json:"fields" yaml:"fields"`
}

type Field struct {
	Name         string   `json:"name" yaml:"name"`
	Number       int32    `json:"number" yaml:"number"`
	Kind         string   `json:"kind" yaml:"kind"`
	Repeated     bool     `json:"repeated,omitempty" yaml:"repeated,omitempty"`
	Optional     bool     `json:"optional,omitempty" yaml:"optional,omitempty"`
	MapKey       string   `json:"map_key,omitempty" yaml:"map_key,omitempty"`
	Message      string   `json:"message,omitempty" yaml:"message,omitempty"`
	EnumValues   []string `json:"enum_values,omitempty" yaml:"enum_values,omitempty"`
	Oneof        string   `json:"oneof,omitempty" yaml:"oneof,omitempty"`
	OneofMembers []string `json:"oneof_members,omitempty" yaml:"oneof_members,omitempty"`
	JSONForm     string   `json:"json_form,omitempty" yaml:"json_form,omitempty"`
	JSONExample  any      `json:"json_example,omitempty" yaml:"json_example,omitempty"`
	Doc          string   `json:"doc,omitempty" yaml:"doc,omitempty"`
	Fields       []*Field `json:"fields,omitempty" yaml:"fields,omitempty"`
	Truncated    bool     `json:"truncated,omitempty" yaml:"truncated,omitempty"`
}

func DescribeMessage(md protoreflect.MessageDescriptor) *Schema {
	return &Schema{
		Message: string(md.FullName()),
		Doc:     leadingComment(md),
		Fields:  describeFields(md, defaultSchemaDepth, map[string]bool{}),
	}
}

func describeFields(md protoreflect.MessageDescriptor, depth int, seen map[string]bool) []*Field {
	fds := md.Fields()
	out := make([]*Field, 0, fds.Len())
	for i := range fds.Len() {
		out = append(out, describeField(fds.Get(i), depth, seen))
	}
	return out
}

func describeField(fd protoreflect.FieldDescriptor, depth int, seen map[string]bool) *Field {
	f := &Field{
		Name:     string(fd.Name()),
		Number:   int32(fd.Number()),
		Kind:     fd.Kind().String(),
		Repeated: fd.IsList(),
		Optional: fd.HasOptionalKeyword(),
		Doc:      leadingComment(fd),
	}
	if od := realOneof(fd); od != nil {
		f.Oneof = string(od.Name())
		f.OneofMembers = oneofMemberNames(od)
	}
	if fd.IsMap() {
		f.Repeated = false
		f.MapKey = fd.MapKey().Kind().String()
		f.Kind = "map<" + f.MapKey + ", " + valueKind(fd.MapValue()) + ">"
		if fd.MapValue().Kind() == protoreflect.MessageKind {
			f.Message = string(fd.MapValue().Message().FullName())
			if form, ok := WellKnownForm(fd.MapValue().Message()); ok {
				f.JSONForm = form
			}
		}
		return f
	}
	switch fd.Kind() {
	case protoreflect.EnumKind:
		ed := fd.Enum()
		f.Message = string(ed.FullName())
		vals := ed.Values()
		for i := range vals.Len() {
			f.EnumValues = append(f.EnumValues, string(vals.Get(i).Name()))
		}
	case protoreflect.MessageKind, protoreflect.GroupKind:
		nested := fd.Message()
		name := string(nested.FullName())
		f.Message = name
		if form, ok := WellKnownForm(nested); ok {
			f.JSONForm = form
			f.JSONExample, _ = wellKnownValue(nested)
			return f
		}
		if depth <= 0 || seen[name] {
			f.Truncated = true
			return f
		}
		seen[name] = true
		f.Fields = describeFields(nested, depth-1, seen)
		delete(seen, name)
	}
	return f
}

func (f *Field) WellKnownExample() (any, bool) {
	if f.JSONForm == "" || strings.HasPrefix(f.Kind, "map<") {
		return nil, false
	}
	return f.JSONExample, true
}

func FieldAt(fields []*Field, segs []string) (*Field, bool) {
	var cur *Field
	for len(segs) > 0 {
		head := segs[0]
		segs = segs[1:]
		if isIndex(head) {
			continue
		}
		var next *Field
		for _, f := range fields {
			if f.Name == head {
				next = f
				break
			}
		}
		if next == nil {
			return nil, false
		}
		cur = next
		if cur.Truncated || cur.MapKey != "" {
			return cur, true
		}
		fields = cur.Fields
	}
	return cur, true
}

func HasPath(fields []*Field, segs []string) bool {
	_, ok := FieldAt(fields, segs)
	return ok
}

func MissingIndex(fields []*Field, segs []string) (string, bool) {
	walked := make([]string, 0, len(segs))
	for i := 0; i < len(segs); i++ {
		head := segs[i]
		walked = append(walked, head)
		if isIndex(head) {
			continue
		}
		var next *Field
		for _, f := range fields {
			if f.Name == head {
				next = f
				break
			}
		}
		if next == nil {
			return "", false
		}
		if next.Truncated || next.MapKey != "" {
			return "", false
		}
		if next.Repeated && i+1 < len(segs) && !isIndex(segs[i+1]) {
			return strings.Join(walked, "."), true
		}
		fields = next.Fields
	}
	return "", false
}

func isIndex(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func oneofMemberNames(od protoreflect.OneofDescriptor) []string {
	fds := od.Fields()
	out := make([]string, 0, fds.Len())
	for i := range fds.Len() {
		out = append(out, string(fds.Get(i).Name()))
	}
	return out
}

func valueKind(fd protoreflect.FieldDescriptor) string {
	if fd.Kind() == protoreflect.MessageKind {
		return string(fd.Message().FullName())
	}
	return fd.Kind().String()
}

func (s *Schema) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", s.Message)
	if s.Doc != "" {
		fmt.Fprintf(&b, "  # %s\n", s.Doc)
	}
	writeFields(&b, s.Fields, "  ")
	return b.String()
}

func writeFields(b *strings.Builder, fields []*Field, indent string) {
	for _, f := range fields {
		kind := f.Kind
		if f.Repeated {
			kind = "repeated " + kind
		}
		if f.Message != "" && len(f.EnumValues) == 0 && !strings.HasPrefix(f.Kind, "map<") {
			kind = kind + " (" + f.Message + ")"
		}
		fmt.Fprintf(b, "%s%-28s %s", indent, f.Name, kind)
		if len(f.EnumValues) > 0 {
			fmt.Fprintf(b, " [%s]", strings.Join(f.EnumValues, "|"))
		}
		if f.Oneof != "" {
			fmt.Fprintf(b, " {oneof %s: %s — send AT MOST ONE}", f.Oneof, strings.Join(f.OneofMembers, " | "))
		}
		if f.JSONForm != "" {
			fmt.Fprintf(b, " {json: %s}", f.JSONForm)
		}
		if f.Truncated {
			b.WriteString(" ...")
		}
		if f.Doc != "" {
			fmt.Fprintf(b, "  # %s", f.Doc)
		}
		b.WriteString("\n")
		writeFields(b, f.Fields, indent+"  ")
	}
}
