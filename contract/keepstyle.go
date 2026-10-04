package contract

import (
	"cmp"
	"gopkg.in/yaml.v3"
	"slices"
)

func KeepWrittenStyle(fresh *yaml.Node, written []byte) {
	var doc yaml.Node
	if err := yaml.Unmarshal(written, &doc); err != nil || len(doc.Content) == 0 {
		return
	}
	prior := doc.Content[0]
	if fresh.Kind == yaml.DocumentNode && len(fresh.Content) > 0 {
		fresh = fresh.Content[0]
	}
	keepStyle(fresh, prior)
}

func keepStyle(fresh, prior *yaml.Node) {
	if fresh == nil || prior == nil || fresh.Kind != prior.Kind {
		return
	}
	fresh.HeadComment, fresh.LineComment, fresh.FootComment = prior.HeadComment, prior.LineComment, prior.FootComment
	switch fresh.Kind {
	case yaml.ScalarNode:
		if fresh.Value == prior.Value {
			fresh.Style = prior.Style
		}
	case yaml.SequenceNode:
		fresh.Style = prior.Style
		for i := range min(len(fresh.Content), len(prior.Content)) {
			keepStyle(fresh.Content[i], prior.Content[i])
		}
	case yaml.MappingNode:
		fresh.Style = prior.Style
		at := map[string]int{}
		for i := 0; i+1 < len(prior.Content); i += 2 {
			at[prior.Content[i].Value] = i
		}
		for i := 0; i+1 < len(fresh.Content); i += 2 {
			j, ok := at[fresh.Content[i].Value]
			if !ok {
				continue
			}
			keepStyle(fresh.Content[i], prior.Content[j])
			keepStyle(fresh.Content[i+1], prior.Content[j+1])
		}
		keepOrder(fresh, at)
	}
}

func keepOrder(mapping *yaml.Node, at map[string]int) {
	type pair struct{ k, v *yaml.Node }
	known, added := []pair{}, []pair{}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		p := pair{mapping.Content[i], mapping.Content[i+1]}
		if _, ok := at[p.k.Value]; ok {
			known = append(known, p)
		} else {
			added = append(added, p)
		}
	}
	slices.SortStableFunc(known, func(a, b pair) int { return cmp.Compare(at[a.k.Value], at[b.k.Value]) })
	mapping.Content = mapping.Content[:0]
	for _, p := range append(known, added...) {
		mapping.Content = append(mapping.Content, p.k, p.v)
	}
}
