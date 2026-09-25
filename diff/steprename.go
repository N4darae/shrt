package diff

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

type StepRename struct {
	Was   string `json:"was"`
	Now   string `json:"now"`
	Index int    `json:"index"`
}

func (s StepRename) String() string {
	return s.Was + " -> " + s.Now
}

func StepRenames(was, now []*runner.StepRecord) []StepRename {
	if !uniqueIDs(was) || !uniqueIDs(now) {
		return nil
	}
	if out := positionalRenames(was, now); out != nil {
		return out
	}
	return alignedRenames(was, now)
}

func alignedRenames(was, now []*runner.StepRecord) []StepRename {
	nowAt := map[string]int{}
	for j, st := range now {
		nowAt[st.ID] = j
	}
	type anchor struct{ i, j int }
	anchors := []anchor{{-1, -1}}
	for i, st := range was {
		if j, ok := nowAt[st.ID]; ok {
			if j < anchors[len(anchors)-1].j {
				return nil
			}
			anchors = append(anchors, anchor{i, j})
		}
	}
	anchors = append(anchors, anchor{len(was), len(now)})
	out := []StepRename{}
	for k := 1; k < len(anchors); k++ {
		a, b := anchors[k-1], anchors[k]
		w, n := was[a.i+1:b.i], now[a.j+1:b.j]
		for _, m := range sameCallAlignment(w, n) {
			out = append(out, StepRename{Was: w[m[0]].ID, Now: n[m[1]].ID, Index: a.j + 1 + m[1]})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sameCallAlignment(was, now []*runner.StepRecord) [][2]int {
	if len(was) == 0 || len(now) == 0 {
		return nil
	}
	dp := make([][]int, len(was)+1)
	for i := range dp {
		dp[i] = make([]int, len(now)+1)
	}
	for i := len(was) - 1; i >= 0; i-- {
		for j := len(now) - 1; j >= 0; j-- {
			if SameCall(was[i], now[j]) {
				dp[i][j] = 1 + dp[i+1][j+1]
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	out := [][2]int{}
	for i, j := 0, 0; i < len(was) && j < len(now); {
		switch {
		case SameCall(was[i], now[j]) && dp[i][j] == 1+dp[i+1][j+1]:
			out = append(out, [2]int{i, j})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}
	return out
}

func positionalRenames(was, now []*runner.StepRecord) []StepRename {
	wasAt, nowAt := map[string][]int{}, map[string][]int{}
	for i, st := range was {
		wasAt[callKey(st)] = append(wasAt[callKey(st)], i)
	}
	for i, st := range now {
		nowAt[callKey(st)] = append(nowAt[callKey(st)], i)
	}
	wasCall, nowCall := map[string]string{}, map[string]string{}
	for _, st := range was {
		wasCall[st.ID] = callKey(st)
	}
	for _, st := range now {
		nowCall[st.ID] = callKey(st)
	}
	out := []StepRename{}
	for i := range min(len(was), len(now)) {
		w, n := was[i], now[i]
		if w.ID == n.ID || !SameCall(w, n) {
			continue
		}
		k := callKey(w)
		if !samePositions(wasAt[k], nowAt[k]) {
			continue
		}
		if c, ok := nowCall[w.ID]; ok && c != k {
			continue
		}
		if c, ok := wasCall[n.ID]; ok && c != k {
			continue
		}
		out = append(out, StepRename{Was: w.ID, Now: n.ID, Index: i})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func samePositions(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func callKey(st *runner.StepRecord) string {
	return strings.ToLower(st.Call[strings.LastIndex(st.Call, "/")+1:])
}

func RenameSteps(steps []*runner.StepRecord, renames []StepRename) []*runner.StepRecord {
	if len(renames) == 0 {
		return steps
	}
	to := map[string]string{}
	for _, r := range renames {
		to[r.Was] = r.Now
	}
	out := make([]*runner.StepRecord, len(steps))
	for i, st := range steps {
		cp := *st
		if now, ok := to[st.ID]; ok {
			cp.ID = now
		}
		if len(st.BodyRefs) > 0 {
			cp.BodyRefs = map[string]string{}
			for k, v := range st.BodyRefs {
				cp.BodyRefs[k] = renameRefs(v, to)
			}
		}
		out[i] = &cp
	}
	return out
}

var stepRefPattern = regexp.MustCompile(`\$\{(steps\.)?([^.}]+)\.`)

func renameRefs(v string, to map[string]string) string {
	return stepRefPattern.ReplaceAllStringFunc(v, func(m string) string {
		sub := stepRefPattern.FindStringSubmatch(m)
		if now, ok := to[sub[2]]; ok {
			return "${" + sub[1] + now + "."
		}
		return m
	})
}

func RenameSpotSteps(spot *store.SafeSpot, now []*runner.StepRecord) (*store.SafeSpot, []StepRename) {
	renames := StepRenames(spot.Steps, now)
	if len(renames) == 0 {
		return spot, nil
	}
	cp := *spot
	cp.Steps = RenameSteps(spot.Steps, renames)
	return &cp, renames
}

func (r *Report) NoteRenamedSteps(renames []StepRename) {
	r.RenamedSteps = append(r.RenamedSteps, renames...)
}

func RenamedLine(renames []StepRename, was, now string) string {
	if len(renames) == 0 {
		return ""
	}
	list := []string{}
	for _, rn := range renames {
		list = append(list, fmt.Sprintf("step %d %s -> %s", rn.Index+1, rn.Was, rn.Now))
	}
	return fmt.Sprintf("renamed step(s): %s (named in %s -> in %s); the call is the same and the step keeps its place among the steps whose ids did not change, so each is compared "+
		"as one step under its new name", strings.Join(list, ", "), was, now)
}
