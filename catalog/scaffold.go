package catalog

import (
	"encoding/json"

	"google.golang.org/protobuf/reflect/protoreflect"
)

const defaultScaffoldDepth = 4

type ScaffoldOptions struct {
	Prefer []string
}

func Scaffold(md protoreflect.MessageDescriptor) map[string]any {
	return ScaffoldWith(md, ScaffoldOptions{})
}

func ScaffoldWith(md protoreflect.MessageDescriptor, opts ScaffoldOptions) map[string]any {
	return scaffoldMessage(md, defaultScaffoldDepth, map[string]bool{}, opts)
}

func ScaffoldJSON(md protoreflect.MessageDescriptor) ([]byte, error) {
	return json.MarshalIndent(Scaffold(md), "", "  ")
}

func scaffoldMessage(md protoreflect.MessageDescriptor, depth int, seen map[string]bool, opts ScaffoldOptions) map[string]any {
	out := map[string]any{}
	fds := md.Fields()
	for i := range fds.Len() {
		fd := fds.Get(i)
		if od := realOneof(fd); od != nil && OneofMember(od, opts.Prefer) != fd {
			continue
		}
		out[string(fd.Name())] = scaffoldValue(fd, depth, seen, opts)
	}
	return out
}

func realOneof(fd protoreflect.FieldDescriptor) protoreflect.OneofDescriptor {
	od := fd.ContainingOneof()
	if od == nil || od.IsSynthetic() {
		return nil
	}
	return od
}

func OneofMember(od protoreflect.OneofDescriptor, prefer []string) protoreflect.FieldDescriptor {
	fds := od.Fields()
	if fds.Len() == 0 {
		return nil
	}
	for _, name := range prefer {
		for i := range fds.Len() {
			if string(fds.Get(i).Name()) == name {
				return fds.Get(i)
			}
		}
	}
	return fds.Get(0)
}

func scaffoldValue(fd protoreflect.FieldDescriptor, depth int, seen map[string]bool, opts ScaffoldOptions) any {
	if fd.IsMap() {
		return map[string]any{}
	}
	if fd.IsList() {
		return []any{scalarOrMessage(fd, depth, seen, opts)}
	}
	return scalarOrMessage(fd, depth, seen, opts)
}

func scalarOrMessage(fd protoreflect.FieldDescriptor, depth int, seen map[string]bool, opts ScaffoldOptions) any {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		nested := fd.Message()
		if v, ok := wellKnownValue(nested); ok {
			return v
		}
		name := string(nested.FullName())
		if depth <= 0 || seen[name] {
			return map[string]any{}
		}
		seen[name] = true
		v := scaffoldMessage(nested, depth-1, seen, opts)
		delete(seen, name)
		return v
	case protoreflect.EnumKind:
		vals := fd.Enum().Values()
		if vals.Len() > 0 {
			return string(vals.Get(0).Name())
		}
		return ""
	case protoreflect.BoolKind:
		return false
	case protoreflect.StringKind, protoreflect.BytesKind:
		return ""
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return 0.0
	case protoreflect.Int64Kind, protoreflect.Uint64Kind, protoreflect.Sint64Kind,
		protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind:
		return "0"
	default:
		return 0
	}
}
