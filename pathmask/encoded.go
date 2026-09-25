package pathmask

import (
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type encodedSecret struct {
	base64  []string
	folded  []string
	lowered []string
	percent *regexp.Regexp
	entity  *regexp.Regexp
}

var htmlNamed = map[rune]string{'&': "amp", '<': "lt", '>': "gt", '"': "quot", '\'': "apos"}

var encodedCache sync.Map

func encodedFormsOf(secret string) *encodedSecret {
	if cached, ok := encodedCache.Load(secret); ok {
		return cached.(*encodedSecret)
	}
	seen := map[string]bool{secret: true}
	forms := []string{}
	add := func(s string) {
		if len(s) >= minFoldedSecret && !seen[s] {
			seen[s] = true
			forms = append(forms, s)
		}
	}
	raw := []byte(secret)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		add(enc.EncodeToString(raw))
	}
	for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
		for shift := 0; shift < 3; shift++ {
			full := enc.EncodeToString(append(make([]byte, shift), raw...))
			from, to := (8*shift+5)/6, 8*(shift+len(raw))/6
			if from < to && to <= len(full) {
				add(full[from:to])
			}
		}
	}
	sort.SliceStable(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	folded := []string{}
	addFolded := func(s string) {
		key := strings.ToLower(s)
		if len(s) >= minFoldedSecret && !seen[key] {
			seen[key] = true
			folded = append(folded, s)
		}
	}
	addFolded(hex.EncodeToString(raw))
	for _, enc := range []*base32.Encoding{base32.StdEncoding, base32.HexEncoding} {
		addFolded(enc.EncodeToString(raw))
		bare := enc.WithPadding(base32.NoPadding)
		addFolded(bare.EncodeToString(raw))
		for shift := 0; shift < 5; shift++ {
			full := bare.EncodeToString(append(make([]byte, shift), raw...))
			from, to := (8*shift+4)/5, 8*(shift+len(raw))/5
			if from < to && to <= len(full) {
				addFolded(full[from:to])
			}
		}
	}
	sort.SliceStable(folded, func(i, j int) bool { return len(folded[i]) > len(folded[j]) })
	var entity strings.Builder
	for _, r := range secret {
		fmt.Fprintf(&entity, "(?:%s|&#0*%d;|&#[xX]0*(?i:%x);", regexp.QuoteMeta(string(r)), r, r)
		if name, ok := htmlNamed[r]; ok {
			fmt.Fprintf(&entity, "|&%s;", name)
		}
		entity.WriteString(")")
	}
	var pattern strings.Builder
	for i := 0; i < len(secret); i++ {
		c := secret[i]
		fmt.Fprintf(&pattern, "(?:%s|%%(?i:%02x)", regexp.QuoteMeta(string(c)), c)
		if c == ' ' {
			pattern.WriteString(`|\+`)
		}
		pattern.WriteString(")")
	}
	lowered := make([]string, 0, len(folded))
	for _, f := range folded {
		lowered = append(lowered, strings.ToLower(f))
	}
	out := &encodedSecret{base64: forms, folded: folded, lowered: lowered, percent: regexp.MustCompile(pattern.String()), entity: regexp.MustCompile(entity.String())}
	encodedCache.Store(secret, out)
	return out
}

func replaceEncoded(s, secret string) string {
	if s == "" {
		return s
	}
	forms := encodedFormsOf(secret)
	for _, form := range forms.base64 {
		s = strings.ReplaceAll(s, form, MaskRedacted)
	}
	var lower string
	for i, form := range forms.folded {
		if i == 0 {
			lower = strings.ToLower(s)
		}
		if strings.Contains(lower, forms.lowered[i]) {
			s = replaceFold(s, form)
			lower = strings.ToLower(s)
		}
	}
	if strings.ContainsAny(s, "%+") {
		s = forms.percent.ReplaceAllLiteralString(s, MaskRedacted)
	}
	if strings.Contains(s, "&") {
		s = forms.entity.ReplaceAllLiteralString(s, MaskRedacted)
	}
	return s
}
