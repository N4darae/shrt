package main

import (
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func (a attribution) orderKey(st *runner.StepRecord, path string) string {
	segs := chain.SplitPath(path)
	list := strings.Join(segs, ".")
	for i := len(segs) - 1; i >= 0; i-- {
		if _, err := strconv.Atoi(segs[i]); err == nil {
			list = strings.Join(segs[:i], ".")
			break
		}
	}
	var body any
	if a.was == nil || !a.decode(st, &body) {
		return ""
	}
	got, _ := lookupPath(body, list).([]any)
	if len(got) < 2 {
		return ""
	}
	now, was := make([]map[string]any, len(got)), make([]map[string]any, len(got))
	for i, item := range got {
		m, ok := item.(map[string]any)
		if !ok {
			return ""
		}
		now[i], was[i] = m, map[string]any{}
		for k, v := range m {
			was[i][k] = v
			if old, ok := a.was(st.ID, list+"."+strconv.Itoa(i)+"."+k); ok {
				was[i][k] = old
			}
		}
	}
	nowBy, wasBy := sortKeys(now), sortKeys(was)
	switch {
	case len(nowBy) != 1:
		return ""
	case len(wasBy) == 1 && wasBy[0] != nowBy[0]:
		return "now by " + nowBy[0] + ", was by " + wasBy[0]
	}
	return "now by " + nowBy[0]
}

func sortKeys(items []map[string]any) []string {
	var keys, out []string
	for k := range items[0] {
		if !isIDKey(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		ascending := true
		for i := 0; i+1 < len(items) && ascending; i++ {
			x, okA := items[i][k].(string)
			y, okB := items[i+1][k].(string)
			less, known := chain.StaticLess(x, y)
			ascending = okA && okB && known && less
		}
		if ascending {
			out = append(out, k)
		}
	}
	return out
}
