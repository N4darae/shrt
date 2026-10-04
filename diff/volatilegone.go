package diff

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/N4darae/shrt/pathmask"
)

func blankValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		if t == "" {
			return true
		}
		if n, err := strconv.ParseFloat(t, 64); err == nil {
			return n == 0
		}
		if at, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return at.IsZero() || at.Unix() == 0
		}
		return false
	case float64:
		return t == 0
	case int:
		return t == 0
	case int64:
		return t == 0
	case json.Number:
		n, err := t.Float64()
		return err == nil && n == 0
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

func valueVanished(c Change) bool {
	var wantBlank, gotBlank bool
	switch c.Kind {
	case KindMissing:
		wantBlank, gotBlank = blankValue(c.Want), true
	case KindUnexpected:
		wantBlank, gotBlank = true, blankValue(c.Got)
	case KindChanged, KindType, KindLength:
		wantBlank, gotBlank = blankValue(c.Want), blankValue(c.Got)
	default:
		return false
	}
	return wantBlank != gotBlank
}

func vanishedUnderMask(m *pathmask.Masker, c Change) bool {
	return valueVanished(c) && m.Masks(c.Path)
}

func hidingPattern(m *pathmask.Masker, path string, leaf bool) string {
	p, _ := m.HidingPattern(path, leaf)
	return p
}

func vanishedDetail(m *pathmask.Masker, c Change, was, now string) string {
	pattern := hidingPattern(m, c.Path, true)
	if valueVanishedNow(c) {
		return fmt.Sprintf("under volatile pattern %s, which tolerates a changed value but not a lost one: %s had a value, %s has none", pattern, was, now)
	}
	return fmt.Sprintf("under volatile pattern %s, which tolerates a changed value but not a new one: %s had none, %s has a value", pattern, was, now)
}

func valueVanishedNow(c Change) bool {
	return c.Kind == KindMissing || (c.Kind != KindUnexpected && blankValue(c.Got))
}
