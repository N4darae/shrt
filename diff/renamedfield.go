package diff

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/N4darae/shrt/chain"
)

type FieldRename struct {
	Step  string `json:"step"`
	From  string `json:"from"`
	To    string `json:"to"`
	Value any    `json:"value"`

	missing, unexpected int
}

func parentPath(path string) (string, string) {
	segs := chain.SplitPath(path)
	if len(segs) < 1 {
		return "", ""
	}
	return strings.Join(segs[:len(segs)-1], "."), segs[len(segs)-1]
}

func (r *Report) RenamedFields() []FieldRename {
	out := []FieldRename{}
	used := map[int]bool{}
	for i, m := range r.Changes {
		if m.Kind != KindMissing || m.Path == "step" || m.Want == nil || isEmptyValue(m.Want) {
			continue
		}
		parent, _ := parentPath(m.Path)
		for j, u := range r.Changes {
			if used[j] || u.Kind != KindUnexpected || u.Step != m.Step || u.Path == "step" {
				continue
			}
			if p, _ := parentPath(u.Path); p != parent || !reflect.DeepEqual(normalizeNumber(m.Want), normalizeNumber(u.Got)) {
				continue
			}
			used[i], used[j] = true, true
			out = append(out, FieldRename{Step: m.Step, From: m.Path, To: u.Path, Value: m.Want, missing: i, unexpected: j})
			break
		}
	}
	return out
}

func isEmptyValue(v any) bool {
	switch t := v.(type) {
	case string:
		return t == ""
	case bool:
		return !t
	case float64:
		return t == 0
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	}
	return false
}

func normalizeNumber(v any) any {
	switch t := v.(type) {
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case int32:
		return float64(t)
	}
	return v
}

func (fr FieldRename) line() string {
	return fmt.Sprintf("%s -> %s (both %s): likely a renamed field, still a change", fr.From, fr.To, show(fr.Value))
}
