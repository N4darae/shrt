package contract

import (
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

func scaffoldPlan(name, noun string, refs, ids []string, lib *Library, cat *catalog.Catalog) (*Plan, error) {
	p := &Plan{stepOf: map[string]string{}, cat: cat, reserved: map[string]bool{}, noun: noun}
	for _, id := range ids {
		p.reserved[id] = true
	}
	p.Chain = &chain.Chain{APIVersion: chain.APIVersion, Name: name}
	methods := make([]*catalog.Method, len(refs))
	for i, ref := range refs {
		m, err := cat.Lookup(ref)
		if err != nil {
			return nil, err
		}
		methods[i] = m
		if _, seen := p.stepOf[m.FullName]; !seen {
			p.stepOf[m.FullName] = ids[i]
		}
	}
	producers := map[string][]string{}
	rpcOf := map[string]string{}
	used := map[string]int{}
	readRun := map[string]int{}
	readerOf := map[string]string{}
	lastWrite := ""
	for i, m := range methods {
		read := chain.IsReadOnlyCall(m.FullName)
		if !read {
			readRun = map[string]int{}
		}
		choose := func(choices []string) string {
			if read {
				return choices[readRun[m.FullName]%len(choices)]
			}
			return leastUsed(choices, used)
		}
		for rpc, ids := range producers {
			p.stepOf[rpc] = choose(ids)
		}
		step := p.buildStep(ids[i], "", m, lib)
		picked, others := p.rewireProducers(step, producers, rpcOf, used, choose)
		if read && readRun[m.FullName] == 0 && readerOf[picked] != "" {
			p.note("step %s reads %s again after %s, as %s did; to read %s instead, reference it", ids[i], picked,
				lastWrite, readerOf[picked], strings.Join(others, " or "))
		}
		if read {
			readRun[m.FullName]++
			if picked != "" {
				readerOf[picked] = ids[i]
			}
		} else {
			lastWrite = ids[i]
		}
		if len(producers[m.FullName]) > 0 {
			distinguishFixtures(step, ids[i], producers[m.FullName][0])
		}
		for _, c := range p.splitSharedProducers(step, p.grown) {
			if rpcOf[c.original] != "" {
				producers[c.call] = append(producers[c.call], c.id)
				rpcOf[c.id] = c.call
			}
		}
		producers[m.FullName] = append(producers[m.FullName], ids[i])
		rpcOf[ids[i]] = m.FullName
		p.Chain.Steps = append(p.Chain.Steps, step)
	}
	p.noteUnevenPreparation(producers, rpcOf)
	return p, nil
}

func (p *Plan) rewireProducers(step *chain.Step, producers map[string][]string, rpcOf map[string]string, used map[string]int,
	choose func([]string) string) (string, []string) {
	var picked string
	var others []string
	var walk func(v any, item int) any
	walk = func(v any, item int) any {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				t[k] = walk(x, item)
			}
		case []any:
			for i, x := range t {
				t[i] = walk(x, i)
			}
		case string:
			if !wholeReference(t) {
				return t
			}
			inner := strings.TrimSuffix(strings.TrimPrefix(t, "${"), "}")
			src, path, ok := strings.Cut(inner, ".")
			if !ok {
				return t
			}
			choices := producers[rpcOf[src]]
			if len(choices) < 2 {
				used[src]++
				return t
			}
			pick := choose(choices)
			if item >= 0 {
				pick = choices[item%len(choices)]
			} else if picked == "" {
				picked = pick
				for _, c := range choices {
					if c != pick {
						others = append(others, c)
					}
				}
			}
			used[pick]++
			return "${" + pick + "." + path + "}"
		}
		return v
	}
	walk(step.Body, -1)
	return picked, others
}

func leastUsed(choices []string, used map[string]int) string {
	pick := choices[0]
	for _, c := range choices[1:] {
		if used[c] < used[pick] {
			pick = c
		}
	}
	return pick
}

func distinguishFixtures(step *chain.Step, id, first string) {
	suffix := strings.TrimPrefix(id, first+"_")
	if suffix == id {
		suffix = id
	}
	var walk func(v any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				t[k] = walk(x)
			}
		case []any:
			for i, x := range t {
				t[i] = walk(x)
			}
		case string:
			loc := planVarRef.FindStringIndex(t)
			if loc == nil || (loc[0] == 0 && loc[1] == len(t)) {
				return t
			}
			return markAfterVar(t, loc, suffix)
		}
		return v
	}
	step.Body, _ = walk(step.Body).(map[string]any)
}

func (p *Plan) noteUnevenPreparation(producers map[string][]string, rpcOf map[string]string) {
	readers := map[string]map[string]bool{}
	for _, st := range p.Chain.Steps {
		if chain.IsReadOnlyCall(st.Call) {
			continue
		}
		for _, ref := range st.References() {
			src, _, _ := strings.Cut(strings.TrimSpace(ref), ".")
			if rpcOf[src] == "" {
				continue
			}
			if readers[src] == nil {
				readers[src] = map[string]bool{}
			}
			readers[src][shortRPC(st.Call)] = true
		}
	}
	rpcs := make([]string, 0, len(producers))
	for rpc := range producers {
		rpcs = append(rpcs, rpc)
	}
	sort.Strings(rpcs)
	for _, rpc := range rpcs {
		ids := producers[rpc]
		if len(ids) < 2 {
			continue
		}
		all := map[string]bool{}
		for _, id := range ids {
			for r := range readers[id] {
				all[r] = true
			}
		}
		for _, id := range ids {
			missing := []string{}
			for r := range all {
				if !readers[id][r] {
					missing = append(missing, r)
				}
			}
			if len(missing) == 0 {
				continue
			}
			sort.Strings(missing)
			p.note("step %s: another %s step is read by %s and this one is not: if it needs the same preparation, "+
				"add one more %s step that reads it", id, shortRPC(rpc), strings.Join(missing, ", "), strings.Join(missing, " / "))
		}
	}
}

func shortRPC(full string) string {
	if i := strings.LastIndex(full, "/"); i >= 0 {
		return full[i+1:]
	}
	return full
}

func ScaffoldChain(name, description string, refs, ids []string, lib *Library, cat *catalog.Catalog) ([]byte, []string, error) {
	p, err := scaffoldPlan(name, "the chain", refs, ids, lib, cat)
	if err != nil {
		return nil, nil, err
	}
	p.Chain.Description = description
	p.discriminateListOrder(lib, nil)
	p.noteRequirements()
	if missing, _ := chain.ExternalInputs(p.Chain); len(missing) > 0 {
		p.declareInterpolatedVars(missing)
	}
	raw, err := p.YAML()
	if err != nil {
		return nil, nil, fmt.Errorf("render chain %s: %w", name, err)
	}
	steps := map[string]bool{}
	for _, st := range p.Chain.Steps {
		steps[st.ID] = true
	}
	return raw, groupStepNotes(p.Notes, steps), nil
}
