package main

import (
	"fmt"
	"slices"
	"strings"
)

type foldedChange struct{ step, path, value string }

const foldedValues = 6

type grouped[V any] struct {
	keys []string
	of   map[string][]V
}

func (g *grouped[V]) add(k string, v V) {
	if g.of == nil {
		g.of = map[string][]V{}
	}
	if _, seen := g.of[k]; !seen {
		g.keys = append(g.keys, k)
	}
	g.of[k] = append(g.of[k], v)
}

func printFold(line string, head foldedChange, more []foldedChange) {
	var byValue grouped[foldedChange]
	for _, c := range more {
		byValue.add(c.value, c)
	}
	at := byValue.of
	same := appendSteps(nil, at[head.value])
	values := slices.DeleteFunc(byValue.keys, func(v string) bool { return v == head.value })
	if len(values) == 0 {
		fmt.Println(wrapNames(fmt.Sprintf("    %s (and %d more at ", line, len(more)), same, ")"))
		return
	}
	fmt.Printf("    %s (and %d more below)\n", line, len(more))
	if len(same) > 0 {
		fmt.Println(wrapNames("      the same at ", same, ""))
	}
	for i, v := range values {
		if i == foldedValues {
			var rest []string
			for _, v := range values[i:] {
				rest = appendSteps(rest, at[v])
			}
			fmt.Println(wrapNames(fmt.Sprintf("      %d more value(s) at ", len(values)-i), rest, ""))
			return
		}
		first := at[v][0]
		shown := "      [" + first.step + "] " + v
		if first.path != head.path {
			shown = "      [" + first.step + "] " + first.path + " " + v
		}
		if also := appendSteps([]string{first.step}, at[v])[1:]; len(also) > 0 {
			shown = wrapNames(shown+", also at ", also, "")
		}
		fmt.Println(shown)
	}
}

func appendSteps(steps []string, changes []foldedChange) []string {
	for _, c := range changes {
		if !slices.Contains(steps, c.step) {
			steps = append(steps, c.step)
		}
	}
	return steps
}

const wrapAt = 120

func wrapNames(lead string, names []string, tail string) string {
	var b strings.Builder
	b.WriteString(lead)
	indent := strings.Repeat(" ", len(lead)-len(strings.TrimLeft(lead, " "))+2)
	width := len(lead)
	for i, n := range names {
		switch {
		case i == 0:
		case width+2+len(n) > wrapAt:
			b.WriteString(",\n" + indent)
			width = len(indent)
		default:
			b.WriteString(", ")
			width += 2
		}
		b.WriteString(n)
		width += len(n)
	}
	return b.String() + tail
}
