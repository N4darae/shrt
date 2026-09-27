package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

func (a attribution) otherRead(step, list string) string {
	st, ok := a.rec.Step(step)
	if !ok || st == nil {
		return ""
	}
	segs := chain.SplitPath(list)
	if len(segs) < 2 {
		return ""
	}
	field, name := segs[len(segs)-1], ""
	for i := len(segs) - 2; i >= 0 && name == ""; i-- {
		if _, err := strconv.Atoi(segs[i]); err != nil {
			name = segs[i]
		}
	}
	var body any
	if json.Unmarshal(st.Response, &body) != nil {
		return ""
	}
	parent, _ := chain.Get(body, strings.Join(segs[:len(segs)-1], "."))
	record, _ := parent.(map[string]any)
	items, _ := record[field].([]any)
	key := ""
	for k, v := range record {
		if s, ok := v.(string); ok && s != "" && namecase.IDNamed(k) && strings.Contains(namecase.Fold(k), namecase.Fold(name)) {
			key = k
		}
	}
	if key == "" {
		return ""
	}
	same := ""
	for _, other := range a.rec.Steps {
		if other == nil || other == st || isWrite(other) {
			continue
		}
		var ob any
		if json.Unmarshal(other.Response, &ob) != nil {
			continue
		}
		n := -1
		eachObject(ob, func(m map[string]any) {
			if l, ok := m[field].([]any); ok && n < 0 && m[key] == record[key] {
				n = len(l)
			}
		})
		switch {
		case n < 0:
		case n != len(items):
			return fmt.Sprintf("; %s read the same %s with %d %s", methodName(other.Call), name, n, field)
		case same == "" && methodName(other.Call) != methodName(st.Call):
			same = fmt.Sprintf("; %s read the same %s with %d %s too", methodName(other.Call), name, n, field)
		}
	}
	return same
}
