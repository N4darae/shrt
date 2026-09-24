package pathmask

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type encodedSecret struct {
	base64  []string
	percent *regexp.Regexp
}

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
	var pattern strings.Builder
	for i := 0; i < len(secret); i++ {
		c := secret[i]
		fmt.Fprintf(&pattern, "(?:%s|%%(?i:%02x)", regexp.QuoteMeta(string(c)), c)
		if c == ' ' {
			pattern.WriteString(`|\+`)
		}
		pattern.WriteString(")")
	}
	out := &encodedSecret{base64: forms, percent: regexp.MustCompile(pattern.String())}
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
	if strings.ContainsAny(s, "%+") {
		s = forms.percent.ReplaceAllLiteralString(s, MaskRedacted)
	}
	return s
}
