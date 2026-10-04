package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

type priorScan struct {
	answered map[string]bool
	bodies   map[string]any
	groups   map[string]*listGroup
}

type listGroup struct {
	ids   []string
	paths map[string]*listPath
}

type listPath struct {
	keys []string
	pos  []int
	upTo []int
}

func newPriorScan(rec *runner.Record, index int) *priorScan {
	s := &priorScan{answered: map[string]bool{}, bodies: map[string]any{}, groups: map[string]*listGroup{}}
	if index > len(rec.Steps) {
		index = len(rec.Steps)
	}
	for _, prior := range rec.Steps[:index] {
		if !answeredCleanly(prior) {
			continue
		}
		s.answered[prior.ID] = true
		var body any
		if json.Unmarshal(prior.Response, &body) != nil {
			continue
		}
		s.bodies[prior.ID] = body
		key, ok := requestKey(prior)
		if !ok {
			continue
		}
		g := s.groups[key]
		if g == nil {
			g = &listGroup{paths: map[string]*listPath{}}
			s.groups[key] = g
		}
		g.add(prior.ID, body)
	}
	return s
}

func requestKey(st *runner.StepRecord) (string, bool) {
	var x any
	if json.Unmarshal(st.Request, &x) != nil {
		return "", false
	}
	canon, err := json.Marshal(x)
	if err != nil {
		return "", false
	}
	return st.Call + "\x00" + string(canon), true
}

func (g *listGroup) add(id string, body any) {
	at := len(g.ids)
	g.ids = append(g.ids, id)
	walkLists(body, nil, func(keys []string, n int) {
		name := strings.Join(keys, "\x00")
		p := g.paths[name]
		if p == nil {
			p = &listPath{keys: append([]string(nil), keys...)}
			g.paths[name] = p
		}
		best := n
		if len(p.upTo) > 0 && p.upTo[len(p.upTo)-1] > best {
			best = p.upTo[len(p.upTo)-1]
		}
		p.pos = append(p.pos, at)
		p.upTo = append(p.upTo, best)
	})
}

func walkLists(v any, keys []string, visit func([]string, int)) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for k, child := range m {
		path := append(keys[:len(keys):len(keys)], k)
		if list, isList := child.([]any); isList && len(list) > 0 {
			visit(path, len(list))
			continue
		}
		walkLists(child, path, visit)
	}
}

func (p *listPath) firstLonger(after any) (int, bool) {
	cur := after
	for _, k := range p.keys[:len(p.keys)-1] {
		m, ok := cur.(map[string]any)
		if !ok {
			return 0, false
		}
		cur = m[k]
	}
	m, ok := cur.(map[string]any)
	if !ok {
		return 0, false
	}
	other, _ := m[p.keys[len(p.keys)-1]].([]any)
	n := len(other)
	i := sort.Search(len(p.upTo), func(i int) bool { return p.upTo[i] > n })
	if i == len(p.upTo) {
		return 0, false
	}
	return p.pos[i], true
}

func (s *priorScan) shrunkList(st *runner.StepRecord) string {
	if !answeredCleanly(st) {
		return ""
	}
	after := decoded(st.Response)
	key, ok := requestKey(st)
	if !ok {
		return ""
	}
	g := s.groups[key]
	if g == nil {
		return ""
	}
	best := -1
	for _, p := range g.paths {
		if at, ok := p.firstLonger(after); ok && (best < 0 || at < best) {
			best = at
		}
	}
	if best < 0 {
		return ""
	}
	return g.ids[best]
}

func (s *priorScan) goneAt(st *runner.StepRecord) string {
	if prior := s.shrunkList(st); prior != "" {
		return fmt.Sprintf("Data created before it was gone after the re-login (step %s lists fewer items than step %s did before the refusal)", st.ID, prior)
	}
	if s.conflictVanished(st) {
		return fmt.Sprintf("Data created before it was gone after the re-login (step %s expected a refusal over a value created before it and was accepted)", st.ID)
	}
	why := stepRefusalText(st)
	if why == "" || st.Status == runner.StatusPassed || st.Transport != nil && strings.EqualFold(st.Transport.Code, "unauthenticated") {
		return ""
	}
	for _, value := range s.createdValues(st) {
		if strings.Contains(why, value) || notFound(why) {
			return fmt.Sprintf("Data created before it was gone after the re-login (step %s: %s)", st.ID, why)
		}
	}
	return ""
}

func (s *priorScan) conflictVanished(st *runner.StepRecord) bool {
	if !answeredCleanly(st) || st.Status != runner.StatusFailed || !s.readsBefore(st) {
		return false
	}
	return slices.ContainsFunc(st.Expect, func(e chain.ExpectResult) bool {
		return !e.Passed && e.Path == chain.EnvelopePath() && e.Want != nil && fmt.Sprint(e.Want) != chain.EnvelopeOK()
	})
}

func (s *priorScan) readsBefore(st *runner.StepRecord) bool {
	for _, text := range st.BodyRefs {
		for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
			ref := chain.ParseRef(m[1])
			if ref.Kind == chain.RefStep && s.answered[ref.Head] {
				return true
			}
		}
	}
	return false
}

func (s *priorScan) createdValues(st *runner.StepRecord) []string {
	var out []string
	for _, text := range st.BodyRefs {
		for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
			ref := chain.ParseRef(m[1])
			body, ok := s.bodies[ref.Head]
			if !ok || ref.Kind != chain.RefStep || strings.HasPrefix(ref.Rest, "request.") {
				continue
			}
			v, ok := chain.Get(body, strings.TrimPrefix(ref.Rest, "response."))
			if text, isText := v.(string); ok && isText && text != "" {
				out = append(out, text)
			}
		}
	}
	return out
}
