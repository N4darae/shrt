package chain

import (
	"fmt"
	"strconv"
)

func (c *Chain) CoerceVars(supplied map[string]any) map[string]any {
	if c == nil || len(supplied) == 0 || len(c.Vars) == 0 {
		return supplied
	}
	out := make(map[string]any, len(supplied))
	for name, value := range supplied {
		declared, ok := c.Vars[name]
		if !ok {
			out[name] = value
			continue
		}
		out[name] = coerceTo(declared, value)
	}
	return out
}

func coerceTo(declared, value any) any {
	switch declared.(type) {
	case string:
		if s, already := value.(string); already {
			return s
		}
		if s, ok := asString(value); ok {
			return s
		}
	case int, int32, int64, float32, float64:
		if s, isText := value.(string); isText {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return n
			}
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return f
			}
		}
	case bool:
		if s, isText := value.(string); isText {
			if b, err := strconv.ParseBool(s); err == nil {
				return b
			}
		}
	}
	return value
}

func asString(v any) (string, bool) {
	switch t := v.(type) {
	case int:
		return strconv.Itoa(t), true
	case int32:
		return strconv.FormatInt(int64(t), 10), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(t), true
	case fmt.Stringer:
		return t.String(), true
	}
	return "", false
}
