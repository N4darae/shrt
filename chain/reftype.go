package chain

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/pathmask"
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
		return cmp.Or(f.Message, "message"), true, false
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
			never = append(never, structureProblems("is interpolated inside other text in "+path, value, refs, false, responses, exports)...)
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
		if !IsNumericKind(target.Kind) {
			if !isMessage(target) && isMessage(src) && !dynamicWellKnown[src.Message] && !scalarWellKnown[src.Message] {
				never = append(never, fmt.Sprintf("${%s} fills %s, declared %s, from %s, declared %s — a whole "+
					"message is sent as a JSON object, which a %s field never accepts, so the request would be rejected "+
					"after every earlier step had already hit the backend, and shrt run refuses the chain before "+
					"sending anything. Reference or export one scalar field of it instead", refs[0], path,
					target.Kind, where, cmp.Or(src.Message, "message"), target.Kind))
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
	for _, name := range sortedKeys(s.Headers) {
		never = append(never, structureProblems("fills header "+name, s.Headers[name], collectRefs(s.Headers[name]), true, responses, exports)...)
	}
	return never, maybe
}

func structureProblems(how, value string, refs []string, header bool, responses map[string]*catalog.Method, exports map[string]exportOrigin) []string {
	carries, fix := "", "Interpolate"
	if header {
		carries, fix = "a header carries text only, and ", "Reference"
	}
	out := []string{}
	for _, ref := range refs {
		if kind, where, ok := structureOf(ref, responses, exports); ok {
			out = append(out, fmt.Sprintf("${%s} %s (%q), from %s, declared %s — %sa message, list or map has no text form, "+
				"so it would be sent as Go syntax (map[...] or [...]) instead of anything the backend reads, and shrt run "+
				"refuses the chain before sending anything. %s one scalar field of it instead", ref, how, value, where, kind, carries, fix))
		}
	}
	return out
}

func structureOf(ref string, responses map[string]*catalog.Method, exports map[string]exportOrigin) (kind, where string, ok bool) {
	src, where, collection, ok := refSourceField(ParseRef(ref), responses, exports)
	switch {
	case !ok || dynamicWellKnown[src.Message]:
		return "", "", false
	case collection:
		return collectionKind(src), where, true
	case isMessage(src) && !scalarWellKnown[src.Message]:
		return cmp.Or(src.Message, "message"), where, true
	}
	return "", "", false
}

func refSourceField(r Ref, responses map[string]*catalog.Method, exports map[string]exportOrigin) (*catalog.Field, string, bool, bool) {
	step, rest, ok := refOrigin(r, exports)
	if !ok {
		return nil, "", false, false
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
	return f, fmt.Sprintf("%s.%s", msg, rest), f.Repeated && !IsDigits(last), true
}

func refOrigin(r Ref, exports map[string]exportOrigin) (step, rest string, ok bool) {
	if r.Err != nil {
		return "", "", false
	}
	if name, isExport := r.ExportName(); isExport {
		if r.Kind == RefExports {
			if _, sub, _ := strings.Cut(r.Rest, "."); sub != "" {
				return "", "", false
			}
		}
		if origin, known := exports[name]; known {
			return origin.step, strings.TrimPrefix(origin.path, "response."), true
		}
		if r.Kind == RefExports {
			return "", "", false
		}
	}
	if r.Kind != RefStep {
		return "", "", false
	}
	return r.Head, r.Rest, true
}

func walkTypedBody(body map[string]any, fields []*catalog.Field, prefix string, fn func(string, *catalog.Field, string, bool)) {
	for _, key := range sortedKeys(body) {
		f, found := catalog.ResponseFieldAt(fields, []string{key})
		if !found || f == nil || f.Truncated {
			continue
		}
		path := pathmask.Join(prefix, key)
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

type Wire struct{ Step, Field, Call, Path string }

func (c *Chain) Wires() []Wire {
	calls := map[string]string{}
	exports := map[string]exportOrigin{}
	out := []Wire{}
	for _, s := range c.Steps {
		walkText(s.Body, "", func(field, value string) {
			refs := collectRefs(value)
			if len(refs) != 1 || strings.TrimSpace(value) != "${"+refs[0]+"}" {
				return
			}
			step, rest, ok := refOrigin(ParseRef(refs[0]), exports)
			segs := slices.DeleteFunc(SplitPath(strings.TrimPrefix(rest, "response.")), IsDigits)
			if call, known := calls[step]; ok && known && len(segs) > 0 && segs[0] != "request" {
				out = append(out, Wire{s.ID, field, call, strings.Join(segs, ".")})
			}
		})
		calls[s.ID] = s.Call
		noteExports(s, exports)
	}
	return out
}

func walkLeaves(v any, path, key string, fn func(path, key, s string)) {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(t) {
			walkLeaves(t[k], pathmask.Join(path, k), k, fn)
		}
	case []any:
		for i, x := range t {
			walkLeaves(x, fmt.Sprintf("%s.%d", path, i), key, fn)
		}
	case string:
		fn(path, key, t)
	}
}

func walkText(v any, path string, fn func(string, string)) {
	switch t := v.(type) {
	case string:
		fn(path, t)
	case []any:
		for _, item := range t {
			walkText(item, path, fn)
		}
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(t)) {
			walkText(t[k], strings.TrimPrefix(path+"."+k, "."), fn)
		}
	}
}
