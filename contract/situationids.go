package contract

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var (
	stepRefHead = lazyRegexp(`\$\{(\s*)(steps\.)?([A-Za-z_][A-Za-z0-9_]*)\.`)
	stepToken   = lazyRegexp(`\b[a-z][a-z0-9_]*\b`)
)

func (p *Plan) nameBySituation() {
	count := map[string]int{}
	for _, st := range p.Chain.Steps {
		count[st.Call]++
	}
	methods := map[*chain.Step]*catalog.Method{}
	taken := map[string]bool{}
	for _, st := range p.Chain.Steps {
		m, err := p.cat.Lookup(st.Call)
		if err != nil || count[st.Call] < 2 || p.creates(st, m) {
			taken[st.ID] = true
			continue
		}
		methods[st] = m
	}
	to := map[string]string{}
	final := func(id string) string { return cmp.Or(to[id], id) }
	acted := map[string]bool{}
	reaches := map[string][]string{}
	touched := map[string]string{}
	lastWrite := ""
	for _, st := range p.Chain.Steps {
		read := chain.IsReadOnlyCall(st.Call)
		reach := []string{st.ID}
		for _, ref := range referencedSteps(st.Body) {
			reach = append(reach, reaches[ref]...)
		}
		reaches[st.ID] = reach
		id := st.ID
		if m := methods[st]; m != nil {
			subject := p.subjectOf(st, m)
			base := chain.SnakeCase(m.Name) + p.indexSuffix(subject, final(subject))
			key := st.Call + " " + subject
			after := touched[subject]
			if subject == "" {
				after = lastWrite
			}
			switch {
			case read && after != "" && after != final(subject):
				base += "_after_" + after
			case !read && subject != "" && acted[key]:
				base += "_again"
			}
			acted[key] = true
			id = base
			for i := 2; taken[id]; i++ {
				id = fmt.Sprintf("%s_%d", base, i)
			}
			taken[id] = true
			if id != st.ID {
				to[st.ID] = id
			}
		}
		if !read {
			lastWrite = id
			for _, r := range reach {
				touched[r] = id
			}
		}
	}
	if len(to) == 0 {
		return
	}
	rename := func(v any) any { return renameStepsIn(v, to) }
	for _, st := range p.Chain.Steps {
		st.ID = final(st.ID)
		st.Body, _ = rename(st.Body).(map[string]any)
		for i := range st.Expect {
			st.Expect[i] = st.Expect[i].MapOperands(rename)
		}
		for k, v := range st.Headers {
			st.Headers[k], _ = rename(v).(string)
		}
	}
	for i, n := range p.Notes {
		p.Notes[i] = stepToken().ReplaceAllStringFunc(n, final)
	}
}

func (p *Plan) creates(st *chain.Step, m *catalog.Method) bool {
	car := singleCarrier(m)
	if car == nil || chain.IsReadOnlyCall(st.Call) {
		return false
	}
	for _, id := range referencedSteps(st.Body) {
		if s := p.stepByID(id); s != nil {
			if sm, err := p.cat.Lookup(s.Call); err == nil {
				if sc := singleCarrier(sm); sc != nil && sc.Message == car.Message {
					return false
				}
			}
		}
	}
	return true
}

func (p *Plan) subjectOf(st *chain.Step, m *catalog.Method) string {
	for _, f := range catalog.DescribeMessage(m.Input()).Fields {
		key, ok := namecase.LookupKey(st.Body, f.Name)
		if !ok {
			continue
		}
		text, _ := st.Body[key].(string)
		if src, isRef := refSource(text); isRef && p.stepByID(src) != nil {
			return src
		}
	}
	return ""
}

func (p *Plan) indexSuffix(subject, named string) string {
	s := p.stepByID(subject)
	if s == nil {
		return ""
	}
	m, err := p.cat.Lookup(s.Call)
	if err != nil {
		return ""
	}
	suffix, ok := strings.CutPrefix(named, chain.SnakeCase(m.Name))
	if !ok || !isIndexSuffix(suffix) {
		return ""
	}
	return suffix
}

func renameStepsIn(v any, to map[string]string) any {
	return mapStrings(v, func(t string) string {
		return stepRefHead().ReplaceAllStringFunc(t, func(ref string) string {
			sub := stepRefHead().FindStringSubmatch(ref)
			if id, ok := to[sub[3]]; ok {
				return "${" + sub[1] + sub[2] + id + "."
			}
			return ref
		})
	})
}
