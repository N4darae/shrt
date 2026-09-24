package pathmask

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/namecase"
)

type Masker struct {
	patterns    []string
	value       string
	secretsOnly bool
	numeric     func(path string) bool
}

func NewMasker(patterns []string) *Masker {
	return &Masker{patterns: patterns, value: MaskVolatile}
}

func NewRedactor(patterns []string) *Masker {
	return &Masker{patterns: patterns, value: MaskRedacted, secretsOnly: true}
}

func (m *Masker) WithNumeric(numeric func(path string) bool) *Masker {
	if m == nil {
		return nil
	}
	out := *m
	out.numeric = numeric
	return &out
}

func (m *Masker) Patterns() []string {
	if m == nil {
		return nil
	}
	return m.patterns
}

const (
	MaskVolatile = "<volatile>"
	MaskRedacted = "<redacted>"
)

func (m *Masker) Apply(root any) any {
	if m == nil || len(m.patterns) == 0 {
		return root
	}
	return m.walk(root, "")
}

func (m *Masker) walk(v any, path string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			child := Join(path, k)
			if m.masked(child) && m.coversValue(child, item) {
				out[k] = m.value
				continue
			}
			out[k] = m.walk(item, child)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for i, item := range t {
			child := Join(path, IndexKey(i))
			if m.masked(child) && m.coversValue(child, item) {
				out = append(out, m.value)
				continue
			}
			out = append(out, m.walk(item, child))
		}
		return out
	default:
		return v
	}
}

func (m *Masker) Masks(path string) bool {
	if m == nil || len(m.patterns) == 0 {
		return false
	}
	return m.masked(path)
}

func (m *Masker) MasksValue(path string, v any) bool {
	return m.Masks(path) && m.coversValue(path, v)
}

func (m *Masker) coversValue(path string, v any) bool {
	if m == nil || !m.secretsOnly {
		return true
	}
	_, isBool := v.(bool)
	return !isBool && !isEmpty(v) && !m.numericZero(path, v)
}

func (m *Masker) numericZero(path string, v any) bool {
	text, ok := v.(string)
	if !ok || m.numeric == nil || !m.numeric(path) {
		return false
	}
	n, err := strconv.ParseFloat(text, 64)
	return err == nil && n == 0
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case float64:
		return t == 0
	case int:
		return t == 0
	case int64:
		return t == 0
	case json.Number:
		return t == "0"
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

func (m *Masker) masked(path string) bool {
	for _, p := range m.patterns {
		if Match(p, path) {
			return true
		}
	}
	return false
}

func Match(pattern, path string) bool {
	return matchSegments(strings.Split(pattern, "."), strings.Split(path, "."))
}

func matchSegments(pat, seg []string) bool {
	if len(pat) == 0 {
		return len(seg) == 0
	}
	head := pat[0]
	if head == "**" {
		for i := 0; i <= len(seg); i++ {
			if matchSegments(pat[1:], seg[i:]) {
				return true
			}
		}
		return false
	}
	if len(seg) == 0 {
		return false
	}
	if head != "*" && !segmentMatches(head, seg[0]) {
		return false
	}
	return matchSegments(pat[1:], seg[1:])
}

func segmentMatches(pattern, segment string) bool {
	if !strings.Contains(pattern, "*") {
		return namecase.Equal(pattern, segment)
	}
	return globFolded(namecase.Fold(pattern), namecase.Fold(segment))
}

func globFolded(pattern, s string) bool {
	star := strings.IndexByte(pattern, '*')
	if star < 0 {
		return pattern == s
	}
	if !strings.HasPrefix(s, pattern[:star]) {
		return false
	}
	rest := pattern[star+1:]
	for i := star; i <= len(s); i++ {
		if globFolded(rest, s[i:]) {
			return true
		}
	}
	return false
}

func Join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func IndexKey(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	digits := []byte{}
	for n := i; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}
