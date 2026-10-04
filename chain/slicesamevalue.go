package chain

import (
	"fmt"
	"sort"
	"strings"
)

func (x *stepIndex) sameValueWrites(at int, keeps map[int]*Keep, opts SliceOptions) []sideEffectWrite {
	target := x.c.Steps[at]
	sent := map[string]string{}
	walkLeaves(target.Body, "", "", func(path, _, t string) { sent[path] = t })
	fields := make([]string, 0, len(sent))
	for field, v := range sent {
		if x.keyValue(at, field, v, opts) {
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	sort.Strings(fields)
	out := []sideEffectWrite{}
	for w, s := range x.c.Steps[:at] {
		if _, kept := keeps[w]; kept || !opts.write(s) || notSent(s, opts) || producesNothing(s, opts) {
			continue
		}
		theirs := map[string]string{}
		walkLeaves(s.Body, "", "", func(path, _, t string) { theirs[path] = t })
		for _, field := range fields {
			if v, ok := theirs[field]; ok && strings.EqualFold(v, sent[field]) && x.keyValue(w, field, v, opts) {
				out = append(out, sideEffectWrite{index: w, reason: fmt.Sprintf("sends the %s %s sends again (%s)", field, target.ID, v)})
				break
			}
		}
	}
	return out
}

func (x *stepIndex) keyValue(i int, field, v string, opts SliceOptions) bool {
	refs := collectRefs(v)
	if len(refs) == 0 || len(strings.TrimSpace(v)) < 4 {
		return false
	}
	for _, ref := range refs {
		if _, kind := x.producerOf(ref, i); kind != refVar {
			return false
		}
	}
	if opts.KeyField == nil {
		return true
	}
	key, known := opts.KeyField(x.rpcOf(i, opts), field)
	return key || !known
}
