package namecase

import "strings"

func Fold(s string) string {
	if folded(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '_' || r == '-' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r += 32
		}
		b.WriteRune(r)
	}
	return b.String()
}

func folded(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 0x80 || c == '_' || c == '-' || c >= 'A' && c <= 'Z' {
			return false
		}
	}
	return true
}

func Equal(a, b string) bool {
	if a == b {
		return true
	}
	i, j := 0, 0
	for {
		for i < len(a) && (a[i] == '_' || a[i] == '-') {
			i++
		}
		for j < len(b) && (b[j] == '_' || b[j] == '-') {
			j++
		}
		if i == len(a) || j == len(b) {
			return i == len(a) && j == len(b)
		}
		if lowerASCII(a[i]) != lowerASCII(b[j]) {
			return false
		}
		i++
		j++
	}
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}

func Words(s string) []string {
	out := []string{}
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	r := []rune(s)
	for i, c := range r {
		switch {
		case c == '_' || c == '-' || c == '.' || c == ' ':
			flush()
		case c >= 'A' && c <= 'Z':
			if cur.Len() > 0 {
				prev := r[i-1]
				if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') || startsWord(r, i+1) {
					flush()
				}
			}
			cur.WriteRune(c + 32)
		default:
			cur.WriteRune(c)
		}
	}
	flush()
	return out
}

func startsWord(r []rune, i int) bool {
	if i >= len(r) || r[i] < 'a' || r[i] > 'z' {
		return false
	}
	j := i
	for j < len(r) && r[j] >= 'a' && r[j] <= 'z' {
		j++
	}
	return !(j-i == 1 && r[i] == 's' && j == len(r))
}

func LookupKey(m map[string]any, key string) (string, bool) {
	if _, ok := m[key]; ok {
		return key, true
	}
	folded := Fold(key)
	found, n := "", 0
	for k := range m {
		if Fold(k) == folded && protojsonName(k) {
			found, n = k, n+1
		}
	}
	if n != 1 {
		return "", false
	}
	return found, true
}

func protojsonName(k string) bool {
	if k == "" || (k[0] >= 'A' && k[0] <= 'Z') || strings.Contains(k, "-") {
		return false
	}
	if strings.Contains(k, "_") {
		return strings.ToLower(k) == k
	}
	return true
}
