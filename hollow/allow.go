package hollow

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const DefaultAllowFile = ".shrt/hollow-allow.txt"

type Allowlist struct {
	Path    string
	entries map[string]string
}

func (a *Allowlist) Reason(chain, step string) (string, bool) {
	if a == nil || a.entries == nil {
		return "", false
	}
	reason, ok := a.entries[chain+"\x00"+step]
	return reason, ok
}

func (a *Allowlist) Len() int {
	if a == nil {
		return 0
	}
	return len(a.entries)
}

func LoadAllowlist(path string, required bool) (*Allowlist, error) {
	a := &Allowlist{Path: path, entries: map[string]string{}}
	f, err := os.Open(path)
	if os.IsNotExist(err) && !required {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		raw := sc.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		chain, rest, ok := cutField(trimmed)
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected '<chain> <step-id> <reason>', got %q", path, line, trimmed)
		}
		step, reason, ok := cutField(rest)
		if !ok || strings.TrimSpace(reason) == "" {
			return nil, fmt.Errorf("%s:%d: entry %q has no reason — an allowlisted hollow read must say why an empty body is the correct answer, or it exempts a step nobody examined", path, line, strings.TrimSpace(chain+" "+strings.TrimSpace(rest)))
		}
		a.entries[chain+"\x00"+step] = strings.TrimSpace(reason)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return a, nil
}

func cutField(s string) (head, rest string, ok bool) {
	i := strings.IndexFunc(s, func(r rune) bool { return r == ' ' || r == '\t' })
	if i < 0 {
		return s, "", false
	}
	return s[:i], strings.TrimLeft(s[i:], " \t"), true
}
