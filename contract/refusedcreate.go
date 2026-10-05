package contract

import (
	"slices"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

const refusedCreateNote = "leave no id to read back"

func (p *Plan) refusedCreate(st *chain.Step) bool {
	if st == nil || chain.IsReadOnlyCall(st.Call) {
		return false
	}
	for _, ref := range allStepRefs(st.Body) {
		if prod := p.stepByID(ref[0]); prod != nil && !chain.IsReadOnlyCall(prod.Call) {
			return false
		}
	}
	m, err := p.cat.Lookup(st.Call)
	if err != nil {
		return false
	}
	for _, fd := range carriersOf(m) {
		for _, sf := range fd.Fields {
			if !IsEntityIDField(sf.Name) || sf.Kind != "string" || sf.Repeated {
				continue
			}
			if _, sent := namecase.LookupKey(st.Body, sf.Name); !sent {
				return true
			}
		}
	}
	return false
}

func (p *Plan) textOnlyReaders(lib *Library, st *chain.Step, createdPath string) []string {
	out := []string{}
	add := func(prod *chain.Step, idPath string) {
		if e, ok := p.readerMatching(lib, prod, idPath, false); ok && !slices.Contains(out, shortRPC(e.reader.FullName)) {
			out = append(out, shortRPC(e.reader.FullName))
		}
	}
	if createdPath != "" {
		add(st, createdPath)
	}
	for _, ref := range allStepRefs(st.Body) {
		if prod := p.stepByID(ref[0]); prod != nil && !chain.IsReadOnlyCall(prod.Call) {
			add(prod, ref[1])
		}
	}
	return out
}

func (p *Plan) noteRefusedCreate(st *chain.Step) {
	for _, n := range p.Notes {
		if strings.Contains(n, refusedCreateNote) {
			return
		}
	}
	p.note("step %s and the other steps expecting a create to be refused %s: the refusal returns none, so no read "+
		"follows them; their expectations assert the refusal and that no created object came back", st.ID, refusedCreateNote)
}
