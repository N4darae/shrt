package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestSliceFailedLinePrintsObjectOperandsAsCompactJSON(t *testing.T) {
	want := map[string]any{"id_product": "p-1", "qty": float64(2), "note": "a<b"}
	got := map[string]any{"id_product": "p-1", "qty": float64(3), "note": "a<b"}
	list := []any{"x", map[string]any{"k": true}}
	source := chain.Verdict{Expect: []chain.ExpectResult{
		{Path: "order.lines.0", Rule: "equals", Want: want, Got: got},
		{Path: "tags", Rule: "equals", Want: list, Got: []any{"x"}},
	}}
	replay := chain.Verdict{Expect: []chain.ExpectResult{
		{Path: "order.lines.0", Rule: "equals", Want: want, Got: got},
		{Path: "tags", Rule: "equals", Want: list, Got: []any{"x"}},
		{Path: "order", Rule: "equals", Want: map[string]any{"status": "OPEN"}, Got: map[string]any{"status": "DONE"}},
	}}
	lines := strings.Join(failedExpectLines(source, replay), "\n")
	for _, s := range []string{
		`failed: order.lines.0 equals want={"id_product":"p-1","note":"a<b","qty":2} source got={"id_product":"p-1","note":"a<b","qty":3}, slice got={"id_product":"p-1","note":"a<b","qty":3}`,
		`failed: tags equals want=["x",{"k":true}] source got=["x"], slice got=["x"]`,
		`failed in the slice only: order equals want={"status":"OPEN"} got={"status":"DONE"}`,
	} {
		if !strings.Contains(lines, s) {
			t.Errorf("want line %s\nin:\n%s", s, lines)
		}
	}
	if strings.Contains(lines, "map[") {
		t.Errorf("object operands must not print in Go map format:\n%s", lines)
	}
}
