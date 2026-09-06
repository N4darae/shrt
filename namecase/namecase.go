package namecase

import "strings"

func Fold(s string) string {
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

func Equal(a, b string) bool {
	return a == b || Fold(a) == Fold(b)
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
	for k := range m {
		if Fold(k) == folded {
			return k, true
		}
	}
	return "", false
}
