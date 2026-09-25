package contract

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
)

type fixtureRegion struct {
	ids     map[string]bool
	order   []string
	targets map[string]bool
}

func (p *Plan) captureRegion(targetSteps map[string]bool) {
	r := &fixtureRegion{ids: map[string]bool{}, targets: map[string]bool{}}
	for id := range targetSteps {
		r.targets[id] = true
	}
	for _, st := range p.Chain.Steps {
		if targetSteps[st.ID] {
			continue
		}
		r.ids[st.ID] = true
		r.order = append(r.order, st.ID)
	}
	p.region = r
}

func (p *Plan) isolating(lib *Library, tag string, probe func()) {
	if p.region == nil {
		probe()
		return
	}
	before := map[*chain.Step]bool{}
	for _, st := range p.Chain.Steps {
		before[st] = true
	}
	probe()
	first, last := -1, -1
	for i, st := range p.Chain.Steps {
		if before[st] {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	if first < 0 {
		return
	}
	for _, st := range p.Chain.Steps[first : last+1] {
		if before[st] {
			return
		}
	}
	group := append([]*chain.Step{}, p.Chain.Steps[first:last+1]...)
	owned := p.ownFixtures(lib, group, tag)
	if len(owned) == len(group) {
		return
	}
	rest := append([]*chain.Step{}, p.Chain.Steps[last+1:]...)
	p.Chain.Steps = append(append(p.Chain.Steps[:first:first], owned...), rest...)
	p.isolated = append(p.isolated, fmt.Sprintf("%s (%d)", tag, len(owned)-len(group)))
}

func (p *Plan) ownFixtures(lib *Library, group []*chain.Step, tag string) []*chain.Step {
	r := p.region
	if r == nil {
		return group
	}
	hasWrite := false
	for _, st := range group {
		if !chain.IsReadOnlyCall(st.Call) {
			hasWrite = true
		}
		for id := range r.targets {
			if readsStep(st, id) {
				return group
			}
		}
	}
	if !hasWrite {
		return group
	}
	owned := map[string]bool{}
	var claim func(s *chain.Step)
	claim = func(s *chain.Step) {
		for _, id := range r.order {
			if owned[id] || !readsStep(s, id) {
				continue
			}
			src := p.stepByID(id)
			if src == nil || chain.IsReadOnlyCall(src.Call) {
				continue
			}
			owned[id] = true
			claim(src)
		}
	}
	for _, st := range group {
		claim(st)
	}
	if len(owned) == 0 {
		return group
	}
	for changed := true; changed; {
		changed = false
		for _, id := range r.order {
			s := p.stepByID(id)
			if owned[id] || s == nil || chain.IsReadOnlyCall(s.Call) || s.SkipAuth || isRefusalStep(s) || p.consumed(id) {
				continue
			}
			for other := range owned {
				if readsStep(s, other) {
					owned[id] = true
					claim(s)
					changed = true
					break
				}
			}
		}
	}
	rename := map[string]string{}
	reserved := map[string]bool{}
	for _, id := range r.order {
		if owned[id] {
			rename[id] = p.freeProbeID(id+"_for_"+tag, reserved)
		}
	}
	out := []*chain.Step{}
	for _, id := range r.order {
		if !owned[id] {
			continue
		}
		src := p.stepByID(id)
		c := copyStep(src, rename[id])
		c.Export = nil
		retarget(c, rename)
		p.freshen(lib, c)
		c.Description = fmt.Sprintf("as %s, a fixture of the %s probes' own, so a defect another probe leaves in %s cannot fail them.", id, strings.ReplaceAll(tag, "_", " "), id)
		p.assertEcho(c)
		out = append(out, c)
	}
	for _, st := range group {
		retarget(st, rename)
	}
	return append(out, group...)
}

func retarget(st *chain.Step, rename map[string]string) {
	st.Body, _ = rewriteRefs(st.Body, rename).(map[string]any)
	for i := range st.Expect {
		st.Expect[i] = st.Expect[i].MapOperands(func(v any) any { return rewriteRefs(v, rename) })
	}
}

func (p *Plan) consumed(id string) bool {
	for _, s := range p.Chain.Steps {
		if s.ID != id && readsStep(s, id) {
			return true
		}
	}
	return false
}

func (p *Plan) noteIsolation() {
	if len(p.isolated) == 0 {
		return
	}
	p.note("probe groups %s run on fixtures of their own (the count is the fixture steps each adds, named <fixture>_for_<group>), "+
		"created as the main path's were with unique fields changed, so a defect one probe exposes (stock drained, a state "+
		"moved) fails only the steps that exercise it instead of every later probe reading the same entity",
		strings.Join(p.isolated, ", "))
}
