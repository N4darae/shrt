package main

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

type outsideValue struct {
	path  string
	value string
}

func outsideState(run *runner.Record, target *runner.StepRecord) []string {
	if run == nil || target == nil {
		return nil
	}
	body := decoded(target.Response)
	candidates := []outsideValue{}
	seen := map[string]bool{}
	addCandidate := func(path string, v any) {
		s, ok := v.(string)
		if !ok || len(s) < 6 || s == pathmask.MaskRedacted || s == pathmask.MaskVolatile || seen[s] {
			return
		}
		if !diff.IDNamedPath(path) {
			return
		}
		seen[s] = true
		candidates = append(candidates, outsideValue{path: path, value: s})
	}
	creates := target.Call != "" && !chain.IsReadOnlyCall(target.Call)
	for _, ex := range target.Expect {
		if ex.Passed || ex.Rule == "unevaluated" {
			continue
		}
		item := listItemPrefix(ex.Path)
		if item == "" && creates {
			continue
		}
		addCandidate(ex.Path, ex.Got)
		if item == "" {
			continue
		}
		if fields, ok := lookupPath(body, item).(map[string]any); ok {
			for k, v := range fields {
				if diff.IDNamedPath(k) {
					addCandidate(item+"."+k, v)
				}
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	others := []string{}
	for _, st := range run.Steps {
		if st == nil || st == target || st.ID == target.ID {
			continue
		}
		others = append(others, string(st.Request), string(st.Response))
	}
	if len(target.Request) > 0 {
		others = append(others, string(target.Request))
	}
	out := []string{}
	for _, c := range candidates {
		found := false
		for _, text := range others {
			if strings.Contains(text, c.value) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, fmt.Sprintf("%s %s", c.path, c.value))
		}
	}
	return out
}

func listItemPrefix(path string) string {
	segs := strings.Split(path, ".")
	for i := len(segs) - 1; i > 0; i-- {
		if digitsOnly(segs[i]) {
			return strings.Join(segs[:i+1], ".")
		}
	}
	return ""
}

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func lookupPath(v any, path string) any {
	for _, seg := range strings.Split(path, ".") {
		switch t := v.(type) {
		case map[string]any:
			v = t[seg]
		case []any:
			if !digitsOnly(seg) {
				return nil
			}
			i := 0
			fmt.Sscan(seg, &i)
			if i >= len(t) {
				return nil
			}
			v = t[i]
		default:
			return nil
		}
	}
	return v
}

func outsideStateCaveat(step string, values []string) string {
	return fmt.Sprintf("caveat: step %s fails on server state the slice did not create (%s), so a fresh backend may pass it",
		step, strings.Join(values, ", "))
}
