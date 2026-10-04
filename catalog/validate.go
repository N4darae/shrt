package catalog

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/N4darae/shrt/namecase"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func (c *Catalog) ValidateInput(m *Method, body []byte) error {
	if len(body) == 0 {
		return nil
	}
	md := m.Input()
	msg := dynamicpb.NewMessage(md)
	opts := protojson.UnmarshalOptions{Resolver: c.types, DiscardUnknown: false}
	if err := opts.Unmarshal(body, msg); err != nil {
		if hint := nameHint(md, body); hint != "" {
			return fmt.Errorf("request does not match %s: %w\n       %s", md.FullName(), err, hint)
		}
		return fmt.Errorf("request does not match %s: %w", md.FullName(), err)
	}
	return nil
}

func nameHint(md protoreflect.MessageDescriptor, body []byte) string {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	return walkNames(md, v, "")
}

func walkNames(md protoreflect.MessageDescriptor, v any, at string) string {
	obj, ok := v.(map[string]any)
	if !ok || strings.HasPrefix(string(md.FullName()), "google.protobuf.") {
		return ""
	}
	fields := md.Fields()
	for _, k := range slices.Sorted(maps.Keys(obj)) {
		fd := fieldByJSONKey(md, k)
		if fd == nil {
			names := make([]string, 0, fields.Len())
			for i := 0; i < fields.Len(); i++ {
				names = append(names, string(fields.Get(i).Name()))
			}
			return fmt.Sprintf("%q is not a field of %s; %s", at+k, md.FullName(), namesHint(k, names, "fields"))
		}
		if hint := walkValue(fd, obj[k], at+k); hint != "" {
			return hint
		}
	}
	return ""
}

func walkValue(fd protoreflect.FieldDescriptor, v any, at string) string {
	if fd.IsMap() {
		m, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		for key, inner := range m {
			if hint := walkSingle(fd.MapValue(), inner, at+"."+key); hint != "" {
				return hint
			}
		}
		return ""
	}
	if list, ok := v.([]any); ok && fd.IsList() {
		for i, inner := range list {
			if hint := walkSingle(fd, inner, fmt.Sprintf("%s.%d", at, i)); hint != "" {
				return hint
			}
		}
		return ""
	}
	return walkSingle(fd, v, at)
}

func walkSingle(fd protoreflect.FieldDescriptor, v any, at string) string {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return walkNames(fd.Message(), v, at+".")
	case protoreflect.EnumKind:
		text, ok := v.(string)
		if !ok || fd.Enum().Values().ByName(protoreflect.Name(text)) != nil {
			return ""
		}
		values := fd.Enum().Values()
		names := make([]string, 0, values.Len())
		for i := 0; i < values.Len(); i++ {
			names = append(names, string(values.Get(i).Name()))
		}
		return fmt.Sprintf("%q is not a value of %s at %q; %s", text, fd.Enum().FullName(), at, namesHint(text, names, "values"))
	}
	return ""
}

func namesHint(name string, valid []string, what string) string {
	if len(valid) <= 20 {
		return "valid " + what + ": " + strings.Join(valid, ", ")
	}
	if near := namecase.Closest(name, valid, 3); len(near) > 0 {
		return "closest " + what + ": " + strings.Join(near, ", ")
	}
	if len(valid) > 40 {
		return fmt.Sprintf("valid %s (first 40 of %d): %s", what, len(valid), strings.Join(valid[:40], ", "))
	}
	return "valid " + what + ": " + strings.Join(valid, ", ")
}

func (c *Catalog) Canonicalize(md protoreflect.MessageDescriptor, body []byte) ([]byte, error) {
	full, _, err := c.CanonicalizeWithPresence(md, body)
	return full, err
}

func (c *Catalog) CanonicalizeWithPresence(md protoreflect.MessageDescriptor, body []byte) (full, present []byte, err error) {
	return c.canonicalize(md, body, false)
}

func (c *Catalog) CanonicalizeDiscardingUnknown(md protoreflect.MessageDescriptor, body []byte) (full, present []byte, unknown []string, err error) {
	full, present, err = c.canonicalize(md, body, false)
	if err == nil {
		return full, present, nil, nil
	}
	unknown = UnknownFields(md, body)
	if len(unknown) == 0 {
		return nil, nil, nil, err
	}
	full, present, lerr := c.canonicalize(md, body, true)
	if lerr != nil {
		return nil, nil, nil, err
	}
	if enums := UnknownEnumValues(md, body); len(enums) > 0 {
		full, lerr = keepEnumsAsSent(full, enums)
		if lerr == nil {
			present, lerr = keepEnumsAsSent(present, enums)
		}
		if lerr != nil {
			return nil, nil, nil, err
		}
	}
	return full, present, unknown, err
}

type UnknownEnum struct {
	Path  string
	Value string
	Enum  string

	at   []string
	sent any
}

func UnknownEnumValues(md protoreflect.MessageDescriptor, body []byte) []UnknownEnum {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil
	}
	var out []UnknownEnum
	collectUnknownEnums(md, v, nil, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func unknownEnumName(fd protoreflect.FieldDescriptor, v any) (string, bool) {
	text, ok := v.(string)
	if !ok || fd.Enum().Values().ByName(protoreflect.Name(text)) != nil {
		return "", false
	}
	return text, true
}

func collectUnknownEnums(md protoreflect.MessageDescriptor, v any, at []string, out *[]UnknownEnum) {
	obj, ok := v.(map[string]any)
	if !ok || strings.HasPrefix(string(md.FullName()), "google.protobuf.") {
		return
	}
	for k, inner := range obj {
		fd := fieldByJSONKey(md, k)
		if fd == nil {
			continue
		}
		here := append(append([]string{}, at...), string(fd.Name()))
		switch {
		case fd.IsMap():
			m, ok := inner.(map[string]any)
			if !ok {
				continue
			}
			for key, x := range m {
				switch fd.MapValue().Kind() {
				case protoreflect.EnumKind:
					if name, bad := unknownEnumName(fd.MapValue(), x); bad {
						*out = append(*out, unknownEnumAt(append(append([]string{}, here...), key), name, fd.MapValue(), x))
					}
				case protoreflect.MessageKind, protoreflect.GroupKind:
					collectUnknownEnums(fd.MapValue().Message(), x, append(append([]string{}, here...), key), out)
				}
			}
		case fd.IsList():
			list, ok := inner.([]any)
			if !ok {
				continue
			}
			for i, x := range list {
				switch fd.Kind() {
				case protoreflect.EnumKind:
					if name, bad := unknownEnumName(fd, x); bad {
						hit := unknownEnumAt(append(append([]string{}, here...), fmt.Sprint(i)), name, fd, x)
						hit.at, hit.sent = here, inner
						*out = append(*out, hit)
					}
				case protoreflect.MessageKind, protoreflect.GroupKind:
					collectUnknownEnums(fd.Message(), x, append(append([]string{}, here...), fmt.Sprint(i)), out)
				}
			}
		case fd.Kind() == protoreflect.EnumKind:
			if name, bad := unknownEnumName(fd, inner); bad {
				*out = append(*out, unknownEnumAt(here, name, fd, inner))
			}
		case fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind:
			collectUnknownEnums(fd.Message(), inner, here, out)
		}
	}
}

func unknownEnumAt(at []string, name string, fd protoreflect.FieldDescriptor, sent any) UnknownEnum {
	return UnknownEnum{Path: strings.Join(at, "."), Value: name, Enum: string(fd.Enum().FullName()), at: at, sent: sent}
}

func keepEnumsAsSent(raw []byte, enums []UnknownEnum) ([]byte, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	for _, e := range enums {
		root = setAt(root, e.at, e.sent)
	}
	return json.Marshal(root)
}

func setAt(v any, at []string, value any) any {
	if len(at) == 0 {
		return value
	}
	switch t := v.(type) {
	case map[string]any:
		t[at[0]] = setAt(t[at[0]], at[1:], value)
		return t
	case []any:
		var i int
		if _, err := fmt.Sscan(at[0], &i); err == nil && i >= 0 && i < len(t) {
			t[i] = setAt(t[i], at[1:], value)
		}
		return t
	case nil:
		return map[string]any{at[0]: setAt(nil, at[1:], value)}
	}
	return v
}

func UnknownFields(md protoreflect.MessageDescriptor, body []byte) []string {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	collectUnknown(md, v, "", seen, &out)
	sort.Strings(out)
	return out
}

func collectUnknown(md protoreflect.MessageDescriptor, v any, at string, seen map[string]bool, out *[]string) {
	obj, ok := v.(map[string]any)
	if !ok || strings.HasPrefix(string(md.FullName()), "google.protobuf.") {
		return
	}
	for k, inner := range obj {
		fd := fieldByJSONKey(md, k)
		if fd == nil {
			if !seen[at+k] {
				seen[at+k] = true
				*out = append(*out, at+k)
			}
			continue
		}
		if fd.Kind() != protoreflect.MessageKind && fd.Kind() != protoreflect.GroupKind {
			continue
		}
		name := string(fd.Name())
		switch {
		case fd.IsMap():
			if m, ok := inner.(map[string]any); ok && fd.MapValue().Message() != nil {
				for _, x := range m {
					collectUnknown(fd.MapValue().Message(), x, at+name+"[].", seen, out)
				}
			}
		case fd.IsList():
			if list, ok := inner.([]any); ok {
				for _, x := range list {
					collectUnknown(fd.Message(), x, at+name+"[].", seen, out)
				}
			}
		default:
			collectUnknown(fd.Message(), inner, at+name+".", seen, out)
		}
	}
}

func (c *Catalog) canonicalize(md protoreflect.MessageDescriptor, body []byte, discard bool) (full, present []byte, err error) {
	msg := dynamicpb.NewMessage(md)
	if err := (protojson.UnmarshalOptions{Resolver: c.types, DiscardUnknown: discard}).Unmarshal(body, msg); err != nil {
		return nil, nil, err
	}
	full, err = protojson.MarshalOptions{
		Resolver:        c.types,
		EmitUnpopulated: true,
		UseProtoNames:   true,
	}.Marshal(msg)
	if err != nil {
		return nil, nil, err
	}
	present, err = protojson.MarshalOptions{
		Resolver:      c.types,
		UseProtoNames: true,
	}.Marshal(msg)
	if err != nil {
		return nil, nil, err
	}
	return full, present, nil
}
