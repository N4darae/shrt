package pathmask

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/N4darae/shrt/namecase"
)

type Masker struct {
	patterns    []string
	split       [][]string
	value       string
	secretsOnly bool
	numeric     func(path string) bool
	secrets     *secretSet
}

type secretSet struct {
	mu     sync.Mutex
	values []string
}

func NewMasker(patterns []string) *Masker {
	return &Masker{patterns: patterns, split: splitPatterns(patterns), value: MaskVolatile}
}

func NewRedactor(patterns []string) *Masker {
	return &Masker{patterns: patterns, split: splitPatterns(patterns), value: MaskRedacted, secretsOnly: true, secrets: &secretSet{}}
}

const minSubstringSecret = 4

const minFoldedSecret = 8

const maxTokenPrefix = 5

func (m *Masker) AddSecret(v string) {
	m.addSecret(v, true)
}

func (m *Masker) AddWholeSecret(v string) {
	m.addSecret(v, false)
}

func (m *Masker) addSecret(v string, withTail bool) {
	if m == nil || m.secrets == nil || v == "" || v == MaskRedacted {
		return
	}
	m.secrets.mu.Lock()
	defer m.secrets.mu.Unlock()
	if !m.secrets.add(v) {
		return
	}
	if tail := tokenTail(v); withTail && tail != "" {
		m.secrets.add(tail)
	}
	sort.SliceStable(m.secrets.values, func(i, j int) bool { return len(m.secrets.values[i]) > len(m.secrets.values[j]) })
}

func (s *secretSet) add(v string) bool {
	if slices.Contains(s.values, v) {
		return false
	}
	s.values = append(s.values, v)
	return true
}

func (m *Masker) knownSecrets() []string {
	if m == nil || m.secrets == nil {
		return nil
	}
	m.secrets.mu.Lock()
	defer m.secrets.mu.Unlock()
	return append([]string(nil), m.secrets.values...)
}

func (m *Masker) ScrubText(s string) string {
	return scrubText(s, m.knownSecrets())
}

func scrubText(s string, secrets []string) string {
	if s == "" || len(secrets) == 0 {
		return s
	}
	parts := strings.Split(s, MaskRedacted)
	for i, part := range parts {
		for _, secret := range secrets {
			switch {
			case part == secret:
				part = MaskRedacted
			case len(secret) >= minSubstringSecret:
				part = strings.ReplaceAll(part, secret, MaskRedacted)
			}
			if len(secret) >= minFoldedSecret {
				part = replaceFold(part, secret)
				part = replaceEncoded(part, secret)
			}
		}
		parts[i] = part
	}
	return strings.Join(parts, MaskRedacted)
}

func replaceFold(s, secret string) string {
	if isASCII(s) && isASCII(secret) && !strings.Contains(strings.ToLower(s), strings.ToLower(secret)) {
		return s
	}
	pieces := strings.Split(s, MaskRedacted)
	for i, piece := range pieces {
		var b strings.Builder
		last := 0
		for at := 0; at+len(secret) <= len(piece); {
			if strings.EqualFold(piece[at:at+len(secret)], secret) {
				b.WriteString(piece[last:at])
				b.WriteString(MaskRedacted)
				at += len(secret)
				last = at
				continue
			}
			at++
		}
		if last > 0 {
			b.WriteString(piece[last:])
			pieces[i] = b.String()
		}
	}
	return strings.Join(pieces, MaskRedacted)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func tokenTail(secret string) string {
	cut := strings.IndexAny(secret, "-_")
	if cut < 1 || cut > maxTokenPrefix {
		return ""
	}
	for _, r := range secret[:cut] {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return ""
		}
	}
	if tail := secret[cut+1:]; len(tail) >= minFoldedSecret {
		return tail
	}
	return ""
}

func (m *Masker) ScrubValue(v any) any {
	secrets := m.knownSecrets()
	if len(secrets) == 0 {
		return v
	}
	return scrubValue(v, secrets)
}

func scrubValue(v any, secrets []string) any {
	switch t := v.(type) {
	case string:
		return scrubText(t, secrets)
	case json.Number:
		if scrubbed := scrubText(string(t), secrets); scrubbed != string(t) {
			return scrubbed
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if scrubText(k, secrets) == k {
				out[k] = scrubValue(t[k], secrets)
			}
		}
		for _, k := range keys {
			key := scrubText(k, secrets)
			if key == k {
				continue
			}
			for n := 2; ; n++ {
				if _, taken := out[key]; !taken {
					break
				}
				key = scrubText(k, secrets) + "#" + strconv.Itoa(n)
			}
			out[key] = scrubValue(t[k], secrets)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			out = append(out, scrubValue(item, secrets))
		}
		return out
	}
	return v
}

func (m *Masker) ScrubJSON(raw json.RawMessage) json.RawMessage {
	secrets := m.knownSecrets()
	if len(raw) == 0 || len(secrets) == 0 {
		return raw
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		if text := scrubText(string(raw), secrets); text != string(raw) {
			return json.RawMessage(strconv.Quote(text))
		}
		return raw
	}
	scrubbed := scrubValue(v, secrets)
	if reflect.DeepEqual(scrubbed, v) {
		return raw
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(scrubbed); err != nil {
		return json.RawMessage(strconv.Quote(MaskRedacted))
	}
	return json.RawMessage(bytes.TrimSpace(buf.Bytes()))
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
			out[k] = m.walkChild(item, Join(path, k))
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for i, item := range t {
			out = append(out, m.walkChild(item, Join(path, IndexKey(i))))
		}
		return out
	default:
		return v
	}
}

func (m *Masker) walkChild(item any, path string) any {
	if m.masked(path) && m.coversValue(path, item) {
		return m.value
	}
	return m.walk(item, path)
}

func (m *Masker) Masks(path string) bool {
	if m == nil || len(m.patterns) == 0 {
		return false
	}
	return m.masked(path)
}

func (m *Masker) HidingPattern(path string, leaf bool) (string, bool) {
	if m == nil || len(m.patterns) == 0 {
		return "", false
	}
	segs := foldSegments(strings.Split(path, "."))
	limit := len(segs)
	if !leaf {
		limit--
	}
	for i := limit; i > 0; i-- {
		for j, p := range m.split {
			if matchFolded(p, segs[:i]) {
				return m.patterns[j], true
			}
		}
	}
	return "", false
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

func splitPatterns(patterns []string) [][]string {
	out := make([][]string, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, foldSegments(strings.Split(p, ".")))
	}
	return out
}

func foldSegments(segs []string) []string {
	for i, seg := range segs {
		if seg != "**" {
			segs[i] = namecase.Fold(seg)
		}
	}
	return segs
}

func (m *Masker) masked(path string) bool {
	segs := foldSegments(strings.Split(path, "."))
	for _, p := range m.split {
		if matchFolded(p, segs) {
			return true
		}
	}
	return false
}

func Match(pattern, path string) bool {
	return matchWith(strings.Split(pattern, "."), strings.Split(path, "."), segmentMatches)
}

func matchFolded(pat, seg []string) bool {
	return matchWith(pat, seg, globFolded)
}

func matchWith(pat, seg []string, matches func(pattern, segment string) bool) bool {
	if len(pat) == 0 {
		return len(seg) == 0
	}
	head := pat[0]
	if head == "**" {
		for i := 0; i <= len(seg); i++ {
			if matchWith(pat[1:], seg[i:], matches) {
				return true
			}
		}
		return false
	}
	if len(seg) == 0 {
		return false
	}
	if head != "*" && !matches(head, seg[0]) {
		return false
	}
	return matchWith(pat[1:], seg[1:], matches)
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
	return strconv.Itoa(i)
}
