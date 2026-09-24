package catalog

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/namecase"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func (c *Catalog) ValidateInput(m *Method, body []byte) error {
	return c.validate(m.Input(), body, "request")
}

func (c *Catalog) ValidateOutput(m *Method, body []byte) error {
	return c.validate(m.Output(), body, "response")
}

func (c *Catalog) validate(md protoreflect.MessageDescriptor, body []byte, side string) error {
	if len(body) == 0 {
		return nil
	}
	msg := dynamicpb.NewMessage(md)
	opts := protojson.UnmarshalOptions{Resolver: c.types, DiscardUnknown: false}
	if err := opts.Unmarshal(body, msg); err != nil {
		if hint := nameHint(md, body); hint != "" {
			return fmt.Errorf("%s does not match %s: %w\n       %s", side, md.FullName(), err, hint)
		}
		return fmt.Errorf("%s does not match %s: %w", side, md.FullName(), err)
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
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := md.Fields()
	for _, k := range keys {
		fd := fields.ByName(protoreflect.Name(k))
		if fd == nil {
			fd = fields.ByJSONName(k)
		}
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
	msg := dynamicpb.NewMessage(md)
	if err := (protojson.UnmarshalOptions{Resolver: c.types}).Unmarshal(body, msg); err != nil {
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
