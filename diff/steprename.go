package diff

import (
	"fmt"
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
	inWas, inNow := map[string]bool{}, map[string]bool{}
	for _, st := range was {
		inWas[st.ID] = true
	}
	for _, st := range now {
		inNow[st.ID] = true
	}
	missing, added := map[string]int{}, map[string]int{}
	for _, st := range was {
		if !inNow[st.ID] {
			missing[callKey(st)]++
		}
	}
	for _, st := range now {
		if !inWas[st.ID] {
			added[callKey(st)]++
		}
	}
	out := []StepRename{}
	for i := range min(len(was), len(now)) {
		w, n := was[i], now[i]
		if w.ID == n.ID || inNow[w.ID] || inWas[n.ID] || !SameCall(w, n) {
			continue
		}
		if missing[callKey(w)] != 1 || added[callKey(n)] != 1 {
			continue
		}
		out = append(out, StepRename{Was: w.ID, Now: n.ID, Index: i})
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
				for was, now := range to {
					v = strings.ReplaceAll(v, "${"+was+".", "${"+now+".")
					v = strings.ReplaceAll(v, "${steps."+was+".", "${steps."+now+".")
				}
				cp.BodyRefs[k] = v
			}
		}
		out[i] = &cp
	}
	return out
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
	return fmt.Sprintf("renamed step(s): %s (named in %s -> in %s); the call and the position are the same, so each is compared "+
		"as one step under its new name", strings.Join(list, ", "), was, now)
}
