package catalog

import (
	"encoding/json"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func UndeclaredValues(md protoreflect.MessageDescriptor, body []byte) any {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil
	}
	out, found := undeclaredIn(md, v)
	if !found {
		return nil
	}
	return out
}

func undeclaredIn(md protoreflect.MessageDescriptor, v any) (any, bool) {
	obj, ok := v.(map[string]any)
	if !ok || md == nil || strings.HasPrefix(string(md.FullName()), "google.protobuf.") {
		return nil, false
	}
	out := map[string]any{}
	fields := md.Fields()
	for k, inner := range obj {
		fd := fields.ByName(protoreflect.Name(k))
		if fd == nil {
			fd = fields.ByJSONName(k)
		}
		if fd == nil {
			out[k] = inner
			continue
		}
		if fd.Kind() != protoreflect.MessageKind && fd.Kind() != protoreflect.GroupKind {
			continue
		}
		name := string(fd.Name())
		switch {
		case fd.IsMap():
			m, ok := inner.(map[string]any)
			if !ok || fd.MapValue().Message() == nil {
				continue
			}
			sub := map[string]any{}
			for key, x := range m {
				if got, found := undeclaredIn(fd.MapValue().Message(), x); found {
					sub[key] = got
				}
			}
			if len(sub) > 0 {
				out[name] = sub
			}
		case fd.IsList():
			list, ok := inner.([]any)
			if !ok {
				continue
			}
			sub, hit := make([]any, len(list)), false
			for i, x := range list {
				got, found := undeclaredIn(fd.Message(), x)
				if !found {
					got = map[string]any{}
				}
				sub[i] = got
				hit = hit || found
			}
			if hit {
				out[name] = sub
			}
		default:
			if got, found := undeclaredIn(fd.Message(), inner); found {
				out[name] = got
			}
		}
	}
	return out, len(out) > 0
}
