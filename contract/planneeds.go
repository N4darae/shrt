package contract

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

func (p *Plan) satisfyNeeds(lib *Library) {
	for round := 0; round < 64; round++ {
		if !p.satisfyOneNeed(lib) {
			return
		}
	}
}

func (p *Plan) satisfyOneNeed(lib *Library) bool {
	for i, st := range p.Chain.Steps {
		if chain.IsReadOnlyCall(st.Call) || st.SkipAuth {
			continue
		}
		c, ok := lib.Get(canonicalCall(p.cat, st.Call))
		if !ok {
			continue
		}
		for _, need := range c.Needs {
			rpc, _ := SplitNode(need)
			needRPC := canonicalCall(p.cat, rpc)
			nc, ok := lib.Get(needRPC)
			if !ok || chain.IsReadOnlyCall(needRPC) {
				continue
			}
			nm, err := p.cat.Lookup(needRPC)
			if err != nil || nm.Streaming() {
				continue
			}
			for _, x := range p.touchedEntities(st) {
				field, path := entityField(p.cat, nc, x)
				if field == "" || p.needMetBefore(i, needRPC, x.ID) {
					continue
				}
				added := p.needStep(lib, nm, field, path, x, st)
				if added == nil {
					continue
				}
				at := p.latestReference(added)
				if at < 0 || at >= i {
					continue
				}
				p.insertAfter(p.Chain.Steps[at].ID, added)
				p.note("step %s: %s needs %s first, for every entity it touches; no earlier %s reached %s, so %s is added "+
					"right after it (the step order on the command line does not decide this)", st.ID, shortRPC(st.Call),
					shortRPC(needRPC), shortRPC(needRPC), x.ID, added.ID)
				return true
			}
		}
	}
	return false
}

func (p *Plan) touchedEntities(st *chain.Step) []*chain.Step {
	out := []*chain.Step{}
	seen := map[string]bool{st.ID: true}
	var visit func(v any)
	visit = func(v any) {
		for _, id := range referencedSteps(v) {
			if seen[id] {
				continue
			}
			seen[id] = true
			prod := p.stepByID(id)
			if prod == nil || chain.IsReadOnlyCall(prod.Call) {
				continue
			}
			out = append(out, prod)
			visit(prod.Body)
		}
	}
	visit(st.Body)
	return out
}

func entityField(cat *catalog.Catalog, c *RPCContract, producer *chain.Step) (string, string) {
	for _, name := range sortedFieldNames(c.Fields) {
		ref, err := ParseRef(c.Fields[name].From)
		if err != nil || strings.Contains(name, ".") || canonicalCall(cat, ref.RPC) != canonicalCall(cat, producer.Call) {
			continue
		}
		return name, ref.Path
	}
	return "", ""
}

func (p *Plan) needMetBefore(upto int, rpc, producer string) bool {
	for _, s := range p.Chain.Steps[:upto] {
		if canonicalCall(p.cat, s.Call) != rpc || isRefusalStep(s) || s.SkipAuth {
			continue
		}
		for _, id := range referencedSteps(s.Body) {
			if id == producer {
				return true
			}
		}
	}
	return false
}

func (p *Plan) needStep(lib *Library, m *catalog.Method, field, path string, producer, reader *chain.Step) *chain.Step {
	id := p.freeStepID(defaultID(m.Name) + "_for_" + producer.ID)
	var template *chain.Step
	if src, ok := p.stepOf[m.FullName]; ok {
		template = p.stepByID(src)
	}
	if template == nil || isRefusalStep(template) {
		template = nil
		for _, s := range p.Chain.Steps {
			if canonicalCall(p.cat, s.Call) == m.FullName && !isRefusalStep(s) && !s.SkipAuth {
				template = s
				break
			}
		}
	}
	var st *chain.Step
	if template != nil {
		st = copyStep(template, id)
		st.Export = nil
		renameStepRefs(st, template.ID, id)
	} else {
		notes := len(p.Notes)
		st = p.buildStep(id, "", m, lib)
		p.Notes = p.Notes[:notes]
		st.Export = nil
	}
	if !setBodyPath(st.Body, field, "${"+producer.ID+"."+path+"}") {
		return nil
	}
	st.Description = fmt.Sprintf("%s for %s, which %s touches: its contract needs %s first.", shortRPC(m.FullName), producer.ID,
		reader.ID, shortRPC(m.FullName))
	return st
}

func (p *Plan) latestReference(st *chain.Step) int {
	at := -1
	for _, id := range referencedSteps(st.Body) {
		found := false
		for i, s := range p.Chain.Steps {
			if s.ID == id {
				found = true
				if i > at {
					at = i
				}
			}
		}
		if !found {
			return -1
		}
	}
	return at
}
