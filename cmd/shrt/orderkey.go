package main

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func orderKey(rec *runner.Record, changes []diff.Change, step, path string) string {
	segs := chain.SplitPath(path)
	list := ""
	for i := len(segs) - 1; i > 0; i-- {
		if _, err := strconv.Atoi(segs[i]); err == nil {
			list = strings.Join(segs[:i], ".")
			break
		}
	}
	st, ok := rec.Step(step)
	if list == "" || !ok || st == nil {
		return ""
	}
	var body any
	if json.Unmarshal(st.Response, &body) != nil {
		return ""
	}
	got, _ := lookupPath(body, list).([]any)
	if len(got) < 2 {
		return ""
	}
	was := make([]map[string]any, len(got))
	now := make([]map[string]any, len(got))
	for i, item := range got {
		m, ok := item.(map[string]any)
		if !ok {
			return ""
		}
		now[i], was[i] = m, map[string]any{}
		for k, v := range m {
			was[i][k] = v
		}
	}
	for _, c := range changes {
		rest, ok := strings.CutPrefix(c.Path, list+".")
		if c.Step != step || c.Kind != diff.KindChanged || !ok {
			continue
		}
		at, field, ok := strings.Cut(rest, ".")
		i, err := strconv.Atoi(at)
		if ok && err == nil && i < len(was) && !strings.Contains(field, ".") {
			was[i][field] = c.Want
		}
	}
	nowBy, wasBy := sortKeys(now), sortKeys(was)
	if len(nowBy) != 1 {
		return ""
	}
	if len(wasBy) == 1 && wasBy[0] != nowBy[0] {
		return "now by " + nowBy[0] + ", was by " + wasBy[0]
	}
	return "now by " + nowBy[0]
}

func sortKeys(items []map[string]any) []string {
	var keys []string
	for k := range items[0] {
		if !isIDKey(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		ascending := true
		for i := 0; i+1 < len(items) && ascending; i++ {
			a, okA := scalarText(items[i][k])
			b, okB := scalarText(items[i+1][k])
			less, known := chain.StaticLess(a, b)
			ascending = okA && okB && known && less
		}
		if ascending {
			out = append(out, k)
		}
	}
	return out
}

func scalarText(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	}
	return "", false
}
