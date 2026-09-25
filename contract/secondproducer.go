package contract

import (
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

type producerSecond struct {
	src    string
	clone  string
	reader string
}

type producerClone struct {
	id       string
	call     string
	original string
}

func (p *Plan) splitSharedProducers(step *chain.Step, grown []string) []producerClone {
	if step == nil || len(grown) == 0 {
		return nil
	}
	clones := map[string]string{}
	made := []producerClone{}
	consumers := map[string][]string{}
	for _, path := range grown {
		for _, list := range listsAt(step.Body, chain.SplitPath(path)) {
			if len(list) < 2 {
				continue
			}
			first, ok1 := list[0].(map[string]any)
			second, ok2 := list[1].(map[string]any)
			if !ok1 || !ok2 {
				continue
			}
			shared := sharedProducers(first, second, func(id string) bool {
				s := p.stepByID(id)
				return s != nil && s != step && !chain.IsReadOnlyCall(s.Call)
			})
			if len(shared) == 0 {
				continue
			}
			for _, src := range shared {
				if _, done := clones[src]; done {
					continue
				}
				id, extra := p.cloneProducer(src, step)
				clones[src] = id
				p.seconds = append(p.seconds, producerSecond{src: src, clone: id, reader: step.ID})
				made = append(made, producerClone{id: id, call: p.stepByID(src).Call, original: src})
				for _, c := range extra {
					made = append(made, c)
					consumers[src] = append(consumers[src], c.id)
				}
			}
			list[1] = rewriteRefs(second, clones)
			p.noteSecondProducer(step.ID, path, shared, clones, consumers)
		}
	}
	return made
}

func listsAt(v any, segs []string) [][]any {
	if len(segs) == 0 {
		if list, ok := v.([]any); ok {
			return [][]any{list}
		}
		return nil
	}
	switch t := v.(type) {
	case map[string]any:
		return listsAt(t[segs[0]], segs[1:])
	case []any:
		out := [][]any{}
		for _, item := range t {
			out = append(out, listsAt(item, segs)...)
		}
		return out
	}
	return nil
}

func refSource(s string) (string, bool) {
	if !wholeReference(s) {
		return "", false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(s, "${"), "}")
	src, rest, ok := strings.Cut(inner, ".")
	if !ok {
		return "", false
	}
	if src == "steps" {
		src, _, ok = strings.Cut(rest, ".")
		if !ok {
			return "", false
		}
	}
	return src, true
}

func sharedProducers(first, second map[string]any, producer func(string) bool) []string {
	out := []string{}
	seen := map[string]bool{}
	var walk func(a, b any)
	walk = func(a, b any) {
		switch t := b.(type) {
		case map[string]any:
			am, _ := a.(map[string]any)
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(am[k], t[k])
			}
		case []any:
			al, _ := a.([]any)
			for i, x := range t {
				var ax any
				if i < len(al) {
					ax = al[i]
				}
				walk(ax, x)
			}
		case string:
			as, _ := a.(string)
			src, ok := refSource(t)
			if !ok || as != t || seen[src] || !producer(src) {
				return
			}
			seen[src] = true
			out = append(out, src)
		}
	}
	walk(first, second)
	return out
}

func rewriteRefs(v any, to map[string]string) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = rewriteRefs(x, to)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = rewriteRefs(x, to)
		}
		return t
	case string:
		for from, id := range to {
			t = strings.ReplaceAll(t, "${"+from+".", "${"+id+".")
			t = strings.ReplaceAll(t, "${steps."+from+".", "${steps."+id+".")
		}
		return t
	}
	return v
}

func (p *Plan) freeStepID(base string) string {
	id := base
	for i := 2; ; i++ {
		if _, exists := p.Chain.Step(id); !exists && !p.reserved[id] {
			return id
		}
		id = fmt.Sprintf("%s_%d", base, i)
	}
}

func (p *Plan) cloneProducer(src string, reader *chain.Step) (string, []producerClone) {
	orig := p.stepByID(src)
	id := p.freeStepID(src)
	clone := copyStep(orig, id)
	suffix := strings.TrimPrefix(id, src+"_")
	if m, err := p.cat.Lookup(orig.Call); err == nil {
		distinctProducer(clone.Body, catalog.DescribeMessage(m.Input()).Fields, suffix)
	}
	p.insertAfter(src, clone)
	extra := []producerClone{}
	for _, s := range append([]*chain.Step{}, p.Chain.Steps...) {
		if s == orig || s == clone || s == reader || chain.IsReadOnlyCall(s.Call) || !readsStep(s, src) || readsStep(reader, s.ID) {
			continue
		}
		cid := p.freeStepID(s.ID)
		c := copyStep(s, cid)
		c.Body, _ = rewriteRefs(c.Body, map[string]string{src: id}).(map[string]any)
		for i := range c.Expect {
			c.Expect[i] = c.Expect[i].MapOperands(func(v any) any { return rewriteRefs(v, map[string]string{src: id}) })
		}
		p.distinctPreparation(c, cid, s.ID)
		p.insertAfter(s.ID, c)
		extra = append(extra, producerClone{id: cid, call: s.Call, original: s.ID})
	}
	return id, extra
}

func readsStep(s *chain.Step, id string) bool {
	if s == nil {
		return false
	}
	for _, ref := range s.References() {
		src, rest, _ := strings.Cut(strings.TrimSpace(ref), ".")
		if src == "steps" {
			src, _, _ = strings.Cut(rest, ".")
		}
		if src == id {
			return true
		}
	}
	return false
}

func copyStep(s *chain.Step, id string) *chain.Step {
	c := *s
	c.ID = id
	c.Body, _ = cloneBody(s.Body).(map[string]any)
	c.Expect = append([]chain.Expectation(nil), s.Expect...)
	for i := range c.Expect {
		c.Expect[i] = c.Expect[i].MapOperands(cloneBody)
	}
	if s.Headers != nil {
		c.Headers = map[string]string{}
		for k, v := range s.Headers {
			c.Headers[k] = v
		}
	}
	if s.Export != nil {
		c.Export = map[string]string{}
		for k, v := range s.Export {
			c.Export[id+strings.TrimPrefix(k, s.ID)] = v
		}
	}
	c.Volatile = append([]string(nil), s.Volatile...)
	c.Unordered = append([]string(nil), s.Unordered...)
	return &c
}

func (p *Plan) insertAfter(id string, s *chain.Step) {
	steps := p.Chain.Steps
	for i, x := range steps {
		if x.ID == id {
			out := append([]*chain.Step{}, steps[:i+1]...)
			out = append(out, s)
			p.Chain.Steps = append(out, steps[i+1:]...)
			return
		}
	}
	p.Chain.Steps = append(steps, s)
}

func distinctProducer(body map[string]any, fields []*catalog.Field, suffix string) {
	for _, f := range fields {
		key, ok := namecase.LookupKey(body, f.Name)
		if !ok || f.Repeated || f.MapKey != "" || len(f.EnumValues) > 0 || f.JSONForm != "" {
			continue
		}
		if len(f.Fields) > 0 {
			if nested, ok := body[key].(map[string]any); ok {
				distinctProducer(nested, f.Fields, suffix)
			}
			continue
		}
		if t, ok := body[key].(string); ok {
			if loc := planVarRef.FindStringIndex(t); loc != nil && !(loc[0] == 0 && loc[1] == len(t)) {
				body[key] = t[:loc[1]] + "-" + suffix + t[loc[1]:]
				continue
			}
		}
		body[key] = nextValue(body[key], f.Kind)
	}
}

func (p *Plan) noteSecondProducer(id, path string, shared []string, clones map[string]string, consumers map[string][]string) {
	parts := []string{}
	for _, src := range shared {
		part := fmt.Sprintf("%s, a second %s with its own values", clones[src], shortRPC(p.stepByID(src).Call))
		if extra := consumers[src]; len(extra) > 0 {
			part += fmt.Sprintf(" (prepared by %s, as %s is)", strings.Join(extra, ", "), src)
		}
		parts = append(parts, part+", instead of "+src)
	}
	p.note("step %s: the second %s item reads %s, so the two items point at different resources with different values: "+
		"logic that uses each item's own resource (a price per line, stock per product) is exercised, and a backend "+
		"that applies the first item's resource to every item fails. Keep that step, or point the item at a resource of your own",
		id, path, strings.Join(parts, "; "))
}

func (p *Plan) distinctPreparation(c *chain.Step, id, first string) {
	distinguishFixtures(c, id, first)
	if m, err := p.cat.Lookup(c.Call); err == nil {
		raiseNumbers(c.Body, catalog.DescribeMessage(m.Input()).Fields)
	}
}

func raiseNumbers(body map[string]any, fields []*catalog.Field) {
	for _, f := range fields {
		key, ok := namecase.LookupKey(body, f.Name)
		if !ok || f.Repeated || f.MapKey != "" || len(f.EnumValues) > 0 || f.JSONForm != "" {
			continue
		}
		if len(f.Fields) > 0 {
			if nested, ok := body[key].(map[string]any); ok {
				raiseNumbers(nested, f.Fields)
			}
			continue
		}
		switch f.Kind {
		case "string", "bytes", "bool", "message", "enum", "group":
			continue
		}
		body[key] = nextValue(body[key], f.Kind)
	}
}

func (p *Plan) prepareSecondProducers(step *chain.Step) {
	if step == nil || chain.IsReadOnlyCall(step.Call) {
		return
	}
	for _, sec := range p.seconds {
		if step.ID == sec.reader || !readsStep(step, sec.src) || readsStep(step, sec.clone) || readsStep(step, sec.reader) {
			continue
		}
		cid := p.freeStepID(step.ID)
		c := copyStep(step, cid)
		to := map[string]string{sec.src: sec.clone}
		c.Body, _ = rewriteRefs(c.Body, to).(map[string]any)
		for i := range c.Expect {
			c.Expect[i] = c.Expect[i].MapOperands(func(v any) any { return rewriteRefs(v, to) })
		}
		p.distinctPreparation(c, cid, step.ID)
		p.insertAfter(step.ID, c)
		p.note("step %s: a copy of %s reading %s instead of %s, its numbers raised by one: %s prepares what the first item of %s "+
			"reads, and the second item reads %s, which needs the same preparation. Keep it, or the second item meets an unprepared resource",
			cid, step.ID, sec.clone, sec.src, step.ID, sec.reader, sec.clone)
	}
}
