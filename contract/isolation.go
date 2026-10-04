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
	if p.region == nil || tag == "" {
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
		if !chain.IsReadOnlyCall(st.Call) && !p.streams(st) {
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
		c := p.fixtureCopy(lib, src, rename[id], rename, fmt.Sprintf("as %s, a fixture of the %s probes' own, so a defect another probe leaves in %s cannot fail them.", id, strings.ReplaceAll(tag, "_", " "), id))
		out = append(out, c)
	}
	for _, st := range group {
		retarget(st, rename)
	}
	return append(out, group...)
}

func (p *Plan) fixtureCopy(lib *Library, src *chain.Step, id string, rename map[string]string, description string) *chain.Step {
	c := probeStep(src, id)
	retarget(c, rename)
	p.freshen(lib, c)
	c.Description = description
	p.assertEcho(c)
	return c
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

func (p *Plan) ownMovedResources(lib *Library, t *listTarget, moves map[*chain.Step]*transition) {
	if p.region == nil {
		return
	}
	keep := map[string]bool{}
	for _, id := range referencedSteps(t.step.Body) {
		keep[id] = true
	}
	for _, prod := range t.producers {
		keep[prod.ID] = true
	}
	moved := []*chain.Step{}
	owned := map[string]bool{}
	for _, prod := range t.producers {
		if moves[prod] == nil || p.region.targets[prod.ID] {
			continue
		}
		moved = append(moved, prod)
		for _, id := range referencedSteps(prod.Body) {
			src := p.stepByID(id)
			if keep[id] || !p.region.ids[id] || src == nil || chain.IsReadOnlyCall(src.Call) {
				continue
			}
			owned[id] = true
		}
	}
	if len(owned) == 0 {
		return
	}
	for _, id := range p.region.order {
		s := p.stepByID(id)
		if owned[id] || keep[id] || s == nil || chain.IsReadOnlyCall(s.Call) || s.SkipAuth || isRefusalStep(s) {
			continue
		}
		reads := referencedSteps(s.Body)
		all := len(reads) > 0
		for _, r := range reads {
			all = all && owned[r]
		}
		if all {
			owned[id] = true
		}
	}
	rename := map[string]string{}
	reserved := map[string]bool{}
	for _, id := range p.region.order {
		if owned[id] {
			rename[id] = p.freeProbeID(id+"_for_filter", reserved)
		}
	}
	copies := []*chain.Step{}
	for _, id := range p.region.order {
		if !owned[id] {
			continue
		}
		c := p.fixtureCopy(lib, p.stepByID(id), rename[id], rename, fmt.Sprintf("as %s, for the fixtures the status filters move, so a defect a main-path write leaves in %s cannot fail the move.", id, id))
		copies = append(copies, c)
	}
	names := []string{}
	for _, prod := range moved {
		retarget(prod, rename)
		names = append(names, prod.ID)
	}
	p.insertBefore(moved[0].ID, copies...)
	p.isolated = append(p.isolated, fmt.Sprintf("filter (%d, for %s)", len(copies), strings.Join(names, ", ")))
}
