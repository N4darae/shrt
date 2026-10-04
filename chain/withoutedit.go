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
	if v := mappingValue(n, key); v != nil {
		return v.Value
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

func AppendDescriptionLine(raw []byte, path, line string) ([]byte, bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false
	}
	before, err := loadBytes(raw, path)
	if err != nil {
		return nil, false
	}
	root := doc.Content[0]
	lines := strings.SplitAfter(string(raw), "\n")
	from, to, text, header := -1, -1, line, "description: |-\n"
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		if key.Value == "name" && from < 0 {
			from, to = key.Line, key.Line-1
		}
		if key.Value != "description" {
			continue
		}
		end := len(lines) + 1
		if i+2 < len(root.Content) {
			end = root.Content[i+2].Line
		}
		from, to = key.Line-1, end-2
		for to > from && strings.TrimSpace(lines[to]) == "" {
			to--
		}
		value := root.Content[i+1].Value
		text = strings.TrimRight(value, "\n") + "\n" + line
		if strings.HasSuffix(value, "\n") {
			header, text = "description: |\n", text+"\n"
		}
	}
	if from < 0 {
		return nil, false
	}
	var b strings.Builder
	b.WriteString(header)
	for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if l != "" {
			b.WriteString("    " + l)
		}
		b.WriteString("\n")
	}
	var out strings.Builder
	for i, l := range lines {
		if i == from {
			out.WriteString(b.String())
		}
		if i < from || i > to {
			out.WriteString(l)
		}
	}
	edited := []byte(out.String())
	got, err := loadBytes(edited, path)
	if err != nil || got.Description != text {
		return nil, false
	}
	before.Description = text
	if got.Digest() != before.Digest() {
		return nil, false
	}
	return edited, true
}
