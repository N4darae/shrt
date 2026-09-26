package chain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/namecase"
)

type exportOrigin struct {
	step string
	path string
}

func noteExports(s *Step, into map[string]exportOrigin) {
	for name, path := range s.Export {
		into[name] = exportOrigin{step: s.ID, path: path}
	}
}

var numericKinds = map[string]bool{
	"int32": true, "int64": true, "uint32": true, "uint64": true, "sint32": true, "sint64": true,
	"fixed32": true, "fixed64": true, "sfixed32": true, "sfixed64": true, "float": true, "double": true,
}

var dynamicWellKnown = map[string]bool{
	"google.protobuf.Value": true, "google.protobuf.ListValue": true, "google.protobuf.Any": true,
	"google.protobuf.Struct": true,
}

var scalarWellKnown = map[string]bool{
	"google.protobuf.Timestamp": true, "google.protobuf.Duration": true, "google.protobuf.FieldMask": true,
	"google.protobuf.StringValue": true, "google.protobuf.BytesValue": true, "google.protobuf.BoolValue": true,
	"google.protobuf.Int32Value": true, "google.protobuf.Int64Value": true,
	"google.protobuf.UInt32Value": true, "google.protobuf.UInt64Value": true,
	"google.protobuf.FloatValue": true, "google.protobuf.DoubleValue": true,
}

func isMessage(f *catalog.Field) bool {
	return f.Kind == "message" || f.Kind == "group"
}

func collectionKind(f *catalog.Field) string {
	elem := f.Kind
	if isMessage(f) && f.Message != "" {
		elem = f.Message
	}
	if f.MapKey != "" {
		return "map"
	}
	return "repeated " + elem
}

var numericWellKnown = map[string]bool{
	"google.protobuf.Int32Value": true, "google.protobuf.Int64Value": true,
	"google.protobuf.UInt32Value": true, "google.protobuf.UInt64Value": true,
	"google.protobuf.FloatValue": true, "google.protobuf.DoubleValue": true,
	"google.protobuf.Value": true, "google.protobuf.Any": true, "google.protobuf.Struct": true,
}

func cannotBeNumber(f *catalog.Field) (kind string, never, maybe bool) {
	switch f.Kind {
	case "string":
		return f.Kind, false, true
	case "bytes", "bool", "enum":
		return f.Kind, true, false
	case "message", "group":
		if numericWellKnown[f.Message] {
			return "", false, false
		}
		if f.Message != "" {
			return f.Message, true, false
		}
		return "message", true, false
	}
	return "", false, false
}

func refTypeProblems(s *Step, m *catalog.Method, responses map[string]*catalog.Method, exports map[string]exportOrigin) (never, maybe []string) {
	if s == nil || m == nil {
		return nil, nil
	}
	walkTypedBody(s.Body, catalog.DescribeMessage(m.Input()).Fields, "", func(path string, target *catalog.Field, value string, whole bool) {
		refs := collectRefs(value)
		if len(refs) == 0 {
			return
		}
		if len(refs) != 1 || strings.TrimSpace(value) != "${"+refs[0]+"}" {
			never = append(never, interpolatedStructures(path, value, refs, responses, exports)...)
			return
		}
		src, where, collection, ok := refSourceField(ParseRef(refs[0]), responses, exports)
		if !ok {
			return
		}
		targetKind := target.Kind
		if whole {
			targetKind = collectionKind(target)
		}
		if whole || collection {
			if whole == collection || dynamicWellKnown[src.Message] || (!whole && isMessage(target)) ||
				(whole && target.MapKey != "" && isMessage(src)) {
				return
			}
			sourceKind := src.Kind
			if collection {
				sourceKind = collectionKind(src)
			}
			never = append(never, fmt.Sprintf("${%s} fills %s, declared %s, from %s, declared %s — a list or map "+
				"cannot be sent as a single value, nor a single value as a list or map, so the request would be "+
				"rejected after every earlier step had already hit the backend, and shrt run refuses the chain "+
				"before sending anything", refs[0], path, targetKind, where, sourceKind))
			return
		}
		if !numericKinds[target.Kind] {
			if !isMessage(target) && isMessage(src) && !dynamicWellKnown[src.Message] && !scalarWellKnown[src.Message] {
				kind := src.Message
				if kind == "" {
					kind = "message"
				}
				never = append(never, fmt.Sprintf("${%s} fills %s, declared %s, from %s, declared %s — a whole "+
					"message is sent as a JSON object, which a %s field never accepts, so the request would be rejected "+
					"after every earlier step had already hit the backend, and shrt run refuses the chain before "+
					"sending anything. Reference or export one scalar field of it instead", refs[0], path,
					target.Kind, where, kind, target.Kind))
			}
			return
		}
		kind, isNever, isMaybe := cannotBeNumber(src)
		switch {
		case isNever:
			never = append(never, fmt.Sprintf("${%s} fills %s, declared %s, from %s, declared %s — no value of that "+
				"type can be sent as %s, so the request would be rejected after every earlier step had already hit "+
				"the backend, and shrt run refuses the chain before sending anything", refs[0], path, target.Kind,
				where, kind, target.Kind))
		case isMaybe:
			maybe = append(maybe, fmt.Sprintf("${%s} fills %s, declared %s, from %s, declared %s — the request is "+
				"valid only if that string holds digits, such as \"5\"; any other text is rejected when the step is "+
				"sent. Check that the source really carries a number", refs[0], path, target.Kind, where, kind))
		}
	})
	never = append(never, headerStructures(s, responses, exports)...)
	return never, maybe
}

func interpolatedStructures(path, value string, refs []string, responses map[string]*catalog.Method, exports map[string]exportOrigin) []string {
	out := []string{}
	for _, ref := range refs {
		src, where, collection, ok := refSourceField(ParseRef(ref), responses, exports)
		if !ok || dynamicWellKnown[src.Message] {
			continue
		}
		kind := ""
		switch {
		case collection:
			kind = collectionKind(src)
		case isMessage(src) && !scalarWellKnown[src.Message]:
			kind = src.Message
			if kind == "" {
				kind = "message"
			}
		default:
			continue
		}
		out = append(out, fmt.Sprintf("${%s} is interpolated inside other text in %s (%q), from %s, declared %s — "+
			"a message, list or map has no text form, so it would be sent as Go syntax (map[...] or [...]) instead of "+
			"anything the backend reads, and shrt run refuses the chain before sending anything. Interpolate one "+
			"scalar field of it instead", ref, path, value, where, kind))
	}
	return out
}

func refSourceField(r Ref, responses map[string]*catalog.Method, exports map[string]exportOrigin) (*catalog.Field, string, bool, bool) {
	if r.Err != nil {
		return nil, "", false, false
	}
	step, rest := "", ""
	if name, ok := r.ExportName(); ok {
		if r.Kind == RefExports {
			if _, sub, _ := strings.Cut(r.Rest, "."); sub != "" {
				return nil, "", false, false
			}
		}
		origin, known := exports[name]
		if !known {
			if r.Kind == RefExports {
				return nil, "", false, false
			}
		} else {
			step, rest = origin.step, strings.TrimPrefix(origin.path, "response.")
		}
	}
	if step == "" {
		if r.Kind != RefStep {
			return nil, "", false, false
		}
		step, rest = r.Head, r.Rest
	}
	m, ok := responses[step]
	if !ok || m == nil || rest == "" {
		return nil, "", false, false
	}
	fields := m.Response().Fields
	msg := m.Output().FullName()
	if path, isRequest := strings.CutPrefix(rest, "request."); isRequest {
		fields, msg, rest = catalog.DescribeMessage(m.Input()).Fields, m.Input().FullName(), path
	} else {
		rest = strings.TrimPrefix(rest, "response.")
	}
	segs := SplitPath(rest)
	f, found := catalog.ResponseFieldAt(fields, segs)
	if !found || f == nil || f.Truncated || len(segs) == 0 {
		return nil, "", false, false
	}
	last := segs[len(segs)-1]
	if f.MapKey != "" {
		if !namecase.Equal(f.Name, last) {
			return nil, "", false, false
		}
		return f, fmt.Sprintf("%s.%s", msg, rest), true, true
	}
	return f, fmt.Sprintf("%s.%s", msg, rest), f.Repeated && !isIndex(last), true
}

func walkTypedBody(v any, fields []*catalog.Field, prefix string, fn func(string, *catalog.Field, string, bool)) {
	body, ok := v.(map[string]any)
	if !ok {
		return
	}
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		f, found := catalog.ResponseFieldAt(fields, []string{key})
		if !found || f == nil || f.Truncated {
			continue
		}
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if text, isText := body[key].(string); isText && (f.Repeated || f.MapKey != "") {
			fn(path, f, text, true)
			continue
		}
		if f.MapKey != "" {
			continue
		}
		visit := func(p string, item any) {
			switch t := item.(type) {
			case string:
				fn(p, f, t, false)
			case map[string]any:
				walkTypedBody(t, f.Fields, p, fn)
			}
		}
		if list, isList := body[key].([]any); isList && f.Repeated {
			for i, item := range list {
				visit(fmt.Sprintf("%s.%d", path, i), item)
			}
			continue
		}
		if f.Repeated {
			continue
		}
		visit(path, body[key])
	}
}
