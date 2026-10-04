package catalog

import (
	"strconv"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func UnsentDefault(md protoreflect.MessageDescriptor, path string, v any) bool {
	segs := strings.Split(path, ".")
	for i := 0; i < len(segs); i++ {
		if md == nil {
			return false
		}
		fd := fieldByJSONKey(md, segs[i])
		if fd == nil {
			return false
		}
		last := i == len(segs)-1
		switch {
		case fd.IsMap():
			if last {
				m, ok := v.(map[string]any)
				return ok && len(m) == 0
			}
			i++
			if i == len(segs)-1 {
				return false
			}
			md = fd.MapValue().Message()
		case fd.IsList():
			if last {
				l, ok := v.([]any)
				return ok && len(l) == 0
			}
			i++
			if _, err := strconv.Atoi(segs[i]); err != nil || i == len(segs)-1 {
				return false
			}
			md = fd.Message()
		case fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind:
			if last {
				return v == nil
			}
			md = fd.Message()
		default:
			if !last || fd.HasPresence() {
				return false
			}
			return scalarDefault(fd, v)
		}
	}
	return false
}

func fieldByJSONKey(md protoreflect.MessageDescriptor, key string) protoreflect.FieldDescriptor {
	fields := md.Fields()
	if fd := fields.ByName(protoreflect.Name(key)); fd != nil {
		return fd
	}
	return fields.ByJSONName(key)
}

func scalarDefault(fd protoreflect.FieldDescriptor, v any) bool {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		b, ok := v.(bool)
		return ok && !b
	case protoreflect.StringKind, protoreflect.BytesKind:
		s, ok := v.(string)
		return ok && s == ""
	case protoreflect.EnumKind:
		switch x := v.(type) {
		case string:
			zero := fd.Enum().Values().ByNumber(0)
			return zero != nil && string(zero.Name()) == x
		case float64:
			return x == 0
		}
		return false
	default:
		switch x := v.(type) {
		case float64:
			return x == 0
		case string:
			return x == "0"
		}
		return false
	}
}
