package catalog

import (
	"sort"
	"strings"
)

type EnvelopeCandidate struct {
	Path        string
	Field       string
	Count       int
	SameMessage bool
}

var envelopeFieldNames = []string{"error", "status", "result", "err", "response_status"}

var envelopeCodeNames = []string{"code", "status", "status_code", "error_code", "reason"}

func DetectItemEnvelope(cat *Catalog, envelopePath string) []EnvelopeCandidate {
	segs := splitDots(envelopePath)
	if cat == nil || len(segs) == 0 {
		return nil
	}
	envelopeMsg := topLevelEnvelopeMessage(cat, segs)
	seen := map[string]*EnvelopeCandidate{}
	for _, m := range cat.Methods() {
		for _, list := range DescribeMessage(m.Output()).Fields {
			if !list.Repeated || list.Kind != "message" {
				continue
			}
			leaf, ok := scalarAt(list.Fields, segs)
			if !ok || leaf == nil {
				continue
			}
			path := list.Name + "[]." + envelopePath
			c := seen[path]
			if c == nil {
				c = &EnvelopeCandidate{Path: path, Field: segs[0]}
				if len(segs) > 1 {
					if head, ok := FieldAt(list.Fields, segs[:1]); ok {
						c.SameMessage = envelopeMsg != "" && head.Message == envelopeMsg
					}
				}
				seen[path] = c
			}
			c.Count++
		}
	}
	out := []EnvelopeCandidate{}
	for _, c := range seen {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SameMessage != out[j].SameMessage {
			return out[i].SameMessage
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func topLevelEnvelopeMessage(cat *Catalog, segs []string) string {
	if len(segs) < 2 {
		return ""
	}
	counts := map[string]int{}
	for _, m := range cat.Methods() {
		head, ok := FieldAt(DescribeMessage(m.Output()).Fields, segs[:1])
		if !ok || head.Repeated || head.Kind != "message" || head.Message == "" {
			continue
		}
		if _, ok := scalarAt(DescribeMessage(m.Output()).Fields, segs); ok {
			counts[head.Message]++
		}
	}
	best, bestN := "", 0
	for name, n := range counts {
		if n > bestN || (n == bestN && name < best) {
			best, bestN = name, n
		}
	}
	return best
}

func scalarAt(fields []*Field, segs []string) (*Field, bool) {
	cur := fields
	var f *Field
	for i, seg := range segs {
		f = nil
		for _, c := range cur {
			if c.Name == seg {
				f = c
				break
			}
		}
		if f == nil || f.Repeated || f.MapKey != "" || f.Truncated {
			return nil, false
		}
		last := i == len(segs)-1
		if last != (f.Kind != "message") {
			return nil, false
		}
		cur = f.Fields
	}
	return f, f != nil
}

func splitDots(path string) []string {
	out := []string{}
	for _, s := range strings.Split(strings.TrimSpace(path), ".") {
		if s == "" {
			return nil
		}
		out = append(out, s)
	}
	return out
}

func DetectEnvelope(cat *Catalog) []EnvelopeCandidate {
	if cat == nil {
		return nil
	}
	seen := map[string]int{}
	total := 0
	for _, m := range cat.Methods() {
		total++
		for _, f := range DescribeMessage(m.Output()).Fields {
			if f.Repeated || f.Kind != "message" {
				continue
			}
			if !matchesAny(f.Name, envelopeFieldNames) {
				continue
			}
			for _, inner := range f.Fields {
				if inner.Kind == "message" || inner.Repeated {
					continue
				}
				if matchesAny(inner.Name, envelopeCodeNames) {
					seen[f.Name+"."+inner.Name]++
				}
			}
		}
	}
	out := []EnvelopeCandidate{}
	for path, n := range seen {
		if n*2 < total {
			continue
		}
		field := path
		for i := 0; i < len(path); i++ {
			if path[i] == '.' {
				field = path[:i]
				break
			}
		}
		out = append(out, EnvelopeCandidate{Path: path, Field: field, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func matchesAny(name string, wanted []string) bool {
	for _, w := range wanted {
		if name == w {
			return true
		}
	}
	return false
}
