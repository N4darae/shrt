package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func stepRefs(st *runner.StepRecord) map[string]bool {
	out := map[string]bool{}
	for _, ref := range st.BodyRefs {
		for _, m := range gateRef.FindAllStringSubmatch(ref, -1) {
			switch m[1] {
			case "vars", "env", "exports":
			default:
				out[m[1]] = true
			}
		}
	}
	return out
}

func isWrite(st *runner.StepRecord) bool {
	return st != nil && !chain.IsReadOnlyCall(st.Call)
}

func suspectWrite(rec *runner.Record, step string, bad map[string]bool) (int, bool) {
	at, pos := -1, map[string]int{}
	for i, st := range rec.Steps {
		if st == nil {
			continue
		}
		if _, seen := pos[st.ID]; !seen {
			pos[st.ID] = i
		}
		if st.ID == step {
			at = i
		}
	}
	if at < 0 || isWrite(rec.Steps[at]) {
		return -1, false
	}
	if _, w, ok := strings.Cut(rec.Steps[at].ID, "_after_"); ok {
		if j, ok := pos[w]; ok && j < at && isWrite(rec.Steps[j]) {
			return j, false
		}
	}
	closure := map[int]map[string]bool{}
	var reach func(i int) map[string]bool
	reach = func(i int) map[string]bool {
		if c, ok := closure[i]; ok {
			return c
		}
		c := map[string]bool{}
		closure[i] = c
		for ref := range stepRefs(rec.Steps[i]) {
			c[ref] = true
			if j, ok := pos[ref]; ok && j < i {
				for r := range reach(j) {
					c[r] = true
				}
			}
		}
		return c
	}
	entities := stepRefs(rec.Steps[at])
	nearest, nearestBad := -1, -1
	for i := at - 1; i >= 0 && len(entities) > 0; i-- {
		w := rec.Steps[i]
		if !isWrite(w) {
			continue
		}
		match := entities[w.ID]
		for e := range reach(i) {
			match = match || entities[e]
		}
		if !match {
			continue
		}
		if nearest < 0 {
			nearest = i
		}
		if bad[w.ID] && nearestBad < 0 {
			nearestBad = i
		}
	}
	switch {
	case nearestBad >= 0:
		return nearestBad, false
	case nearest >= 0:
		return nearest, false
	}
	for i := at - 1; i >= 0; i-- {
		if w := rec.Steps[i]; isWrite(w) && bad[w.ID] {
			return i, true
		}
	}
	return -1, false
}

type blame struct {
	write int
	knock bool
	own   string
}

type attribution struct {
	e         *env
	rec       *runner.Record
	bad       map[string]bool
	unchanged func(step, path string) bool
	reordered func(step, path string) bool
}

func (a attribution) of(step, path string) blame {
	b := blame{write: -1}
	st, ok := a.rec.Step(step)
	if !ok || st == nil || isWrite(st) {
		return b
	}
	if why := serverError(st); why != "" {
		b.own = fmt.Sprintf("%s fails on its own (%s)", methodName(st.Call), why)
		return b
	}
	if src := heldBackBy(st); src != "" && src != step {
		if up := a.of(src, ""); up.own != "" {
			b.own = up.own
			return b
		}
	}
	if a.reordered != nil && path != "" && a.reordered(step, path) {
		b.own = fmt.Sprintf("%s answers the same items in another order", methodName(st.Call))
		return b
	}
	b.write, b.knock = suspectWrite(a.rec, step, a.bad)
	if b.write >= 0 && !b.knock {
		if why := a.echoed(a.rec.Steps[b.write], st, path); why != "" {
			return blame{write: -1, own: why}
		}
	}
	return b
}

func heldBackBy(st *runner.StepRecord) string {
	src := ""
	for _, ex := range st.Expect {
		if ex.Passed {
			continue
		}
		_, rest, ok := strings.Cut(ex.Detail, `reads step "`)
		if ex.Rule != "unevaluated" || !ok {
			return ""
		}
		if name, _, _ := strings.Cut(rest, `"`); src == "" {
			src = name
		}
	}
	return src
}

func (a attribution) echoed(w, r *runner.StepRecord, path string) string {
	if a.e == nil || a.e.cat == nil || a.unchanged == nil || path == "" {
		return ""
	}
	rm, err := a.e.cat.Lookup(r.Call)
	if err != nil {
		return ""
	}
	wm, err := a.e.cat.Lookup(w.Call)
	if err != nil {
		return ""
	}
	want, ok := carrierOf(rm.Output(), path)
	if !ok {
		return ""
	}
	var rb, wb any
	if json.Unmarshal(r.Response, &rb) != nil || json.Unmarshal(w.Response, &wb) != nil {
		return ""
	}
	rv, ok := chain.Get(rb, path)
	if !ok {
		return ""
	}
	found := false
	eachLeaf(wb, "", func(p string, v any) {
		if found {
			return
		}
		if c, ok := carrierOf(wm.Output(), p); !ok || c != want {
			return
		}
		found = sameEntity(rb, path, wb, p) && a.unchanged(w.ID, p) && compactValue(v) != compactValue(rv)
	})
	if !found {
		return ""
	}
	return fmt.Sprintf("%s answers %s differently from what %s returned for the same record",
		methodName(r.Call), gateIndex.ReplaceAllString(path, "[]$1"), methodName(w.Call))
}

func carrierOf(md protoreflect.MessageDescriptor, path string) (string, bool) {
	segs := chain.SplitPath(path)
	for len(segs) > 0 {
		if _, err := strconv.Atoi(segs[len(segs)-1]); err != nil {
			break
		}
		segs = segs[:len(segs)-1]
	}
	skip := false
	for i, seg := range segs {
		if skip {
			skip = false
			continue
		}
		if _, err := strconv.Atoi(seg); err == nil {
			continue
		}
		if md == nil {
			return "", false
		}
		fd := md.Fields().ByName(protoreflect.Name(seg))
		if fd == nil {
			fd = md.Fields().ByJSONName(seg)
		}
		if fd == nil {
			return "", false
		}
		if i == len(segs)-1 {
			return string(md.FullName()) + "." + string(fd.Name()), true
		}
		if fd.IsMap() {
			skip = true
			md = fd.MapValue().Message()
			continue
		}
		md = fd.Message()
	}
	return "", false
}

func eachLeaf(v any, path string, visit func(string, any)) {
	join := func(k string) string {
		if path == "" {
			return k
		}
		return path + "." + k
	}
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			eachLeaf(x, join(k), visit)
		}
	case []any:
		for i, x := range t {
			eachLeaf(x, join(strconv.Itoa(i)), visit)
		}
	default:
		visit(path, v)
	}
}

func sameEntity(a any, pa string, b any, pb string) bool {
	parent := func(root any, path string) map[string]any {
		segs := chain.SplitPath(path)
		if len(segs) < 2 {
			return nil
		}
		v, _ := chain.Get(root, strings.Join(segs[:len(segs)-1], "."))
		m, _ := v.(map[string]any)
		return m
	}
	x, y := parent(a, pa), parent(b, pb)
	sawID := false
	for k, v := range x {
		lower := strings.ToLower(k)
		if lower != "id" && !strings.HasPrefix(lower, "id_") && !strings.HasSuffix(lower, "_id") {
			continue
		}
		if w, ok := y[k]; ok {
			if compactValue(v) == compactValue(w) {
				return true
			}
			sawID = true
		}
	}
	return !sawID
}

func requestLine(rec *runner.Record, step string, b blame) string {
	st, ok := rec.Step(step)
	if !ok || st == nil {
		return ""
	}
	lead := step
	if b.own != "" {
		lead = fmt.Sprintf("suspect read %s (%s)", step, shortRPC(st.Call))
	}
	if b.write >= 0 {
		st = rec.Steps[b.write]
		lead = fmt.Sprintf("suspect write %s (%s)", st.ID, shortRPC(st.Call))
	}
	if st.AuthProfile != "" && st.AuthProfile != "default" {
		lead += " as " + st.AuthProfile
	}
	var buf bytes.Buffer
	if len(st.Request) == 0 || json.Compact(&buf, st.Request) != nil {
		return ""
	}
	return lead + " sent " + capText(buf.String(), 300)
}
