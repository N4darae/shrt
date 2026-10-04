package contract

import (
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

func varyScalars(body map[string]any, fields []*catalog.Field, marker string, skip func(path string) bool) []string {
	changed := []string{}
	var walk func(m map[string]any, fs []*catalog.Field, path string)
	walk = func(m map[string]any, fs []*catalog.Field, path string) {
		for _, f := range fs {
			key, ok := namecase.LookupKey(m, f.Name)
			if !ok {
				continue
			}
			at := join(path, f.Name)
			if skip != nil && skip(at) {
				continue
			}
			if f.Repeated || f.MapKey != "" || f.Oneof != "" || len(f.EnumValues) > 0 || f.Kind == "bool" || idLike(f.Name) {
				continue
			}
			if len(f.Fields) > 0 {
				if nested, ok := m[key].(map[string]any); ok {
					walk(nested, f.Fields, at)
				}
				continue
			}
			if v, ok := otherValue(m[key], f.Kind, marker); ok {
				m[key] = v
				changed = append(changed, at)
			}
		}
	}
	walk(body, fields, "")
	return changed
}

func otherValue(v any, kind, marker string) (any, bool) {
	if chain.IsNumericKind(kind) {
		n, ok := numericValue(v)
		if !ok || n == 0 {
			return v, false
		}
		return strconv.FormatInt(n*2+1, 10), true
	}
	if kind != "string" {
		return v, false
	}
	text, ok := v.(string)
	if !ok || strings.TrimSpace(text) == "" || wholeReference(text) || refersToStep(text) {
		return v, false
	}
	return lengthen(text, marker), true
}

func literalKey(body map[string]any, name string) (string, bool) {
	key, ok := namecase.LookupKey(body, name)
	_, literal := numericValue(body[key])
	return key, ok && literal
}

func numericValue(v any) (int64, bool) {
	switch t := v.(type) {
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return n, err == nil
	case float64:
		return int64(t), true
	case int:
		return int64(t), true
	case int64:
		return t, true
	}
	return 0, false
}

func refersToStep(text string) bool {
	for _, loc := range anyValueRef().FindAllString(text, -1) {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(loc, "${"), "}"))
		if !freshValueRef().MatchString(loc) && !strings.HasPrefix(inner, "vars.") && !strings.HasPrefix(inner, "env.") {
			return true
		}
	}
	return false
}
