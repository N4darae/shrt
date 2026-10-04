package contract

import (
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var emptyListsAll = lazyRegexp(`(?i)\b(?:empty|blank|absent|unset|omitted|missing|no)\b[^.;]*?\b(?:lists?|returns?|matches?|means?|shows?|gives?)\b[^.;]*?\b(?:all|every|everything|any|no filter|unfiltered)\b`)

func EmptyMeansAll(c *RPCContract, field string) bool {
	if c == nil || field == "" {
		return false
	}
	if fc := c.Fields[field]; fc != nil && emptyListsAll().MatchString(fc.Note) {
		return true
	}
	words := namecase.Words(field)
	for _, text := range []string{c.Summary, c.Note} {
		for _, clause := range clauseBreaks().Split(text, -1) {
			m := emptyListsAll().FindString(clause)
			if m == "" {
				continue
			}
			if mentionsField(m, field) || (len(words) > 0 && mentionsField(m, strings.ToLower(words[len(words)-1]))) {
				return true
			}
		}
	}
	return false
}

func (p *Plan) probeEmptyFilter(lib *Library, t *listTarget, key string) {
	c, _ := lib.Get(canonicalCall(p.cat, t.step.Call))
	if !EmptyMeansAll(c, key) {
		return
	}
	if v, _ := t.step.Body[key].(string); v == "" {
		return
	}
	fixtures := []*chain.Step{}
	for _, s := range p.Chain.Steps {
		if s == t.step {
			break
		}
		if s.Call == t.producers[0].Call && !isRefusalStep(s) && !s.AllowFail && effectOutcome(s) == outcomeSuccess {
			fixtures = append(fixtures, s)
		}
	}
	if len(fixtures) == 0 {
		return
	}
	m, err := p.cat.Lookup(t.step.Call)
	if err != nil {
		return
	}
	probe := probeStep(t.step, p.freeStepID(t.step.ID+"_empty_"+key))
	probe.Body[key] = ""
	probe.Expect = SuccessExpectation(m)
	ids := []string{}
	for _, f := range fixtures {
		probe.Expect = append(probe.Expect, chain.Expectation{Path: t.listPath, Includes: map[string]any{t.itemID: "${" + f.ID + "." + t.carrier + "." + t.itemID + "}"}})
		ids = append(ids, f.ID)
	}
	probe.Expect = append(probe.Expect, chain.Expectation{Path: fmt.Sprintf("%s.%d", t.listPath, len(fixtures)-1), Exists: boolPtr(true)})
	probe.Description = fmt.Sprintf("%s with %s empty, which the contract says lists all: every fixture this run created is among them, whatever else the backend holds.", t.step.ID, key)
	p.insertAfter(t.step.ID, probe)
	p.note("step %s: its contract says an empty %s lists all, so %s sends it empty and asserts that each of %s is in %s by id "+
		"(includes:), and at least %d item(s), not a position or an exact count: other runs' records share an unfiltered list",
		t.step.ID, key, probe.ID, strings.Join(ids, ", "), t.listPath, len(fixtures))
}

type EmptyFilterGap struct {
	RPC    string   `json:"rpc"`
	Field  string   `json:"field"`
	Chains []string `json:"chains"`
}

func EmptyFilterGaps(chains []*chain.Chain, lib *Library, cat *catalog.Catalog) []EmptyFilterGap {
	type tally struct {
		empty  bool
		chains map[string]bool
	}
	seen := map[string]*tally{}
	for _, c := range chains {
		if c == nil {
			continue
		}
		for _, s := range c.Steps {
			if s == nil || effectOutcome(s) != outcomeSuccess {
				continue
			}
			m, err := cat.Lookup(s.Call)
			if err != nil || !chain.IsReadOnlyCall(m.FullName) || repeatedMessageField(m) == nil {
				continue
			}
			rc, ok := lib.Get(m.FullName)
			if !ok {
				continue
			}
			for _, f := range catalog.DescribeMessage(m.Input()).Fields {
				if f.Kind != "string" || f.Repeated || !EmptyMeansAll(rc, f.Name) {
					continue
				}
				k := m.FullName + "\x00" + f.Name
				t := seen[k]
				if t == nil {
					t = &tally{chains: map[string]bool{}}
					seen[k] = t
				}
				t.chains[c.Name] = true
				key, sent := namecase.LookupKey(s.Body, f.Name)
				if text, _ := s.Body[key].(string); !sent || text == "" {
					t.empty = true
				}
			}
		}
	}
	out := []EmptyFilterGap{}
	for k, t := range seen {
		if t.empty {
			continue
		}
		rpc, field, _ := strings.Cut(k, "\x00")
		names := sortedKeys(t.chains)
		out = append(out, EmptyFilterGap{RPC: rpc, Field: field, Chains: names})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RPC != out[j].RPC {
			return out[i].RPC < out[j].RPC
		}
		return out[i].Field < out[j].Field
	})
	return out
}
