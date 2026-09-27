package chain

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func (x *stepIndex) sameValueWrites(at int, keeps map[int]*Keep, mode string, opts SliceOptions) []sideEffectWrite {
	target := x.c.Steps[at]
	sent := map[string]string{}
	stringLeaves(target.Body, "", sent)
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
	for w := 0; w < at; w++ {
		if _, kept := keeps[w]; kept {
			continue
		}
		s := x.c.Steps[w]
		if !isWriteCall(s.Call) || notSent(s, opts) || producesNothing(s, opts) || (opts.IsLogin != nil && opts.IsLogin(s)) {
			continue
		}
		if mode == SliceModePin && opts.Performed != nil && opts.Performed(s.ID) {
			continue
		}
		theirs := map[string]string{}
		stringLeaves(s.Body, "", theirs)
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

func stringLeaves(v any, prefix string, into map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			stringLeaves(child, joinPath(prefix, k), into)
		}
	case []any:
		for i, child := range t {
			stringLeaves(child, joinPath(prefix, strconv.Itoa(i)), into)
		}
	case string:
		into[prefix] = t
	}
}

func joinPath(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + "." + k
}
