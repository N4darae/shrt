package chain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
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
	walkTypedBody(s.Body, catalog.DescribeMessage(m.Input()).Fields, "", func(path string, target *catalog.Field, value string) {
		refs := collectRefs(value)
		if len(refs) != 1 || strings.TrimSpace(value) != "${"+refs[0]+"}" {
			return
		}
		src, where, ok := refSourceField(ParseRef(refs[0]), responses, exports)
		if !ok {
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
	return never, maybe
}

func refSourceField(r Ref, responses map[string]*catalog.Method, exports map[string]exportOrigin) (*catalog.Field, string, bool) {
	if r.Err != nil {
		return nil, "", false
	}
	step, rest := "", ""
	if name, ok := r.ExportName(); ok {
		if r.Kind == RefExports {
			if _, sub, _ := strings.Cut(r.Rest, "."); sub != "" {
				return nil, "", false
			}
		}
		origin, known := exports[name]
		if !known {
			if r.Kind == RefExports {
				return nil, "", false
			}
		} else {
			step, rest = origin.step, strings.TrimPrefix(origin.path, "response.")
		}
	}
	if step == "" {
		if r.Kind != RefStep {
			return nil, "", false
		}
		step, rest = r.Head, r.Rest
	}
	m, ok := responses[step]
	if !ok || m == nil || rest == "" {
		return nil, "", false
	}
	fields := catalog.DescribeMessage(m.Output()).Fields
	msg := m.Output().FullName()
	if path, isRequest := strings.CutPrefix(rest, "request."); isRequest {
		fields, msg, rest = catalog.DescribeMessage(m.Input()).Fields, m.Input().FullName(), path
	} else {
		rest = strings.TrimPrefix(rest, "response.")
	}
	segs := SplitPath(rest)
	f, found := catalog.ResponseFieldAt(fields, segs)
	if !found || f == nil || f.Truncated || f.MapKey != "" {
		return nil, "", false
	}
	if f.Repeated && !isIndex(segs[len(segs)-1]) {
		return nil, "", false
	}
	return f, fmt.Sprintf("%s.%s", msg, rest), true
}

func walkTypedBody(v any, fields []*catalog.Field, prefix string, fn func(string, *catalog.Field, string)) {
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
		if !found || f == nil || f.Truncated || f.MapKey != "" {
			continue
		}
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		visit := func(p string, item any) {
			switch t := item.(type) {
			case string:
				if numericKinds[f.Kind] {
					fn(p, f, t)
				}
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
