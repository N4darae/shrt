package chain

import (
	"fmt"
	"slices"
	"strings"
)

type Removed struct {
	Index  int    `json:"index"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type WithoutResult struct {
	Source      string    `json:"source"`
	Total       int       `json:"total"`
	Removed     []Removed `json:"removed"`
	DroppedPins []Pin     `json:"dropped_kept_red,omitempty"`
	Chain       *Chain    `json:"-"`
}

func DefaultWithoutName(chainName string, drop []string) string {
	return chainName + "-without-" + strings.Join(drop, "-")
}

func Without(c *Chain, drop []string, name string) (*WithoutResult, error) {
	if len(drop) == 0 {
		return nil, fmt.Errorf("-without names no step")
	}
	idx := newStepIndex(c)
	named := map[string]bool{}
	for _, id := range drop {
		if _, ok := idx.byID[id]; !ok {
			return nil, fmt.Errorf("chain %s has no step %q (steps: %s)", c.Name, id, strings.Join(idx.ids(), ", "))
		}
		named[id] = true
	}
	if name == "" {
		name = DefaultWithoutName(c.Name, drop)
	}
	res := &WithoutResult{Source: c.Name, Total: len(c.Steps)}
	gone := map[int]bool{}
	out := &Chain{
		APIVersion:  c.APIVersion,
		Name:        name,
		Description: c.Description,
		Vars:        c.Vars,
		Volatile:    append([]string{}, c.Volatile...),
		Unordered:   append([]string{}, c.Unordered...),
		Redact:      append([]string{}, c.Redact...),
	}
	for i, s := range c.Steps {
		if named[s.ID] {
			gone[i] = true
			res.Removed = append(res.Removed, Removed{Index: i + 1, ID: s.ID, Reason: "named"})
			continue
		}
		reads := []string{}
		for _, ref := range stepRefs(s) {
			if j, kind := idx.producerOf(ref, i); kind == refStep && gone[j] && !slices.Contains(reads, c.Steps[j].ID) {
				reads = append(reads, c.Steps[j].ID)
			}
		}
		if len(reads) > 0 {
			gone[i] = true
			res.Removed = append(res.Removed, Removed{Index: i + 1, ID: s.ID, Reason: "reads " + strings.Join(reads, ", ")})
			continue
		}
		out.Steps = append(out.Steps, s)
	}
	kept := map[string]bool{}
	for _, s := range out.Steps {
		kept[s.ID] = true
	}
	for _, k := range c.KeptRed {
		if kept[k.Step] {
			out.KeptRed = append(out.KeptRed, k)
		} else {
			res.DroppedPins = append(res.DroppedPins, k)
		}
	}
	if len(out.Steps) == 0 {
		return nil, fmt.Errorf("without %s nothing of %s is left to run", strings.Join(drop, ", "), c.Name)
	}
	res.Chain = out
	return res, nil
}
