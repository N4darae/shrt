package chain

import (
	"strings"

	"gopkg.in/yaml.v3"
)

func (r *WithoutResult) EditSource(raw []byte, path string) ([]byte, bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false
	}
	root := doc.Content[0]
	lines := strings.SplitAfter(string(raw), "\n")
	removed := map[string]bool{}
	for _, x := range r.Removed {
		removed[x.ID] = true
	}
	cut := map[int]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, seq := root.Content[i], root.Content[i+1]
		if seq.Kind != yaml.SequenceNode || key.Value != "steps" && key.Value != "kept_red" {
			continue
		}
		end := len(lines) + 1
		if i+2 < len(root.Content) {
			end = root.Content[i+2].Line
		}
		gone := 0
		for j, item := range seq.Content {
			next := end
			if j+1 < len(seq.Content) {
				next = seq.Content[j+1].Line
			}
			id := fieldOf(item, "id")
			if key.Value == "kept_red" {
				id = fieldOf(item, "step")
			}
			if removed[id] {
				gone++
				cutLines(lines, cut, item.Line-1, next-2)
			}
		}
		if gone > 0 && gone == len(seq.Content) {
			cutLines(lines, cut, key.Line-1, end-2)
		}
	}
	var b strings.Builder
	for i, l := range lines {
		if !cut[i] {
			b.WriteString(l)
		}
	}
	out := []byte(b.String())
	got, err := loadBytes(out, path)
	if err != nil || got.Digest() != r.Chain.Digest() {
		return nil, false
	}
	return out, true
}

func fieldOf(n *yaml.Node, key string) string {
	if n.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1].Value
		}
	}
	return ""
}

func cutLines(lines []string, cut map[int]bool, from, to int) {
	blankOrComment := func(i int) bool {
		t := strings.TrimSpace(lines[i])
		return t == "" || strings.HasPrefix(t, "#")
	}
	for t := min(to, len(lines)-1); t >= from && blankOrComment(t); t-- {
		if strings.TrimSpace(lines[t]) != "" {
			to = t - 1
		}
	}
	for from > 0 && strings.HasPrefix(strings.TrimSpace(lines[from-1]), "#") {
		from--
	}
	for i := from; i <= to && i < len(lines); i++ {
		cut[i] = true
	}
}
