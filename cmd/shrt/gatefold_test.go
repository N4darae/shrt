package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestGateVerboseNamesEveryFoldedStepAndEachOtherValue(t *testing.T) {
	shown := []string{"[create_order] changed    order.total_minor want=600 got=400"}
	var steps []string
	for i := 1; i <= 25; i++ {
		step := fmt.Sprintf("fetch_order_after_create_order_number_%d", i)
		steps = append(steps, step)
		shown = append(shown, "["+step+"] changed    order.total_minor want=600 got=400")
	}
	for i := 3; i <= 10; i++ {
		write, read := fmt.Sprintf("create_order_%d_lines", i), fmt.Sprintf("fetch_order_after_create_order_%d_lines", i)
		steps = append(steps, write, read)
		for _, step := range []string{write, read} {
			shown = append(shown, fmt.Sprintf("[%s] changed    order.total_minor want=%d00 got=%d00", step, 2*i, i))
		}
	}
	shown = append(shown, "[list_orders] changed    orders.1.total_minor want=900 got=700")
	g := &gateChain{name: "orders", failed: true, shown: shown}
	out := captureStdout(t, func() { g.printChanges(nil) })
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	want := []string{
		"    [create_order] changed    order.total_minor want=600 got=400 (and 41 more below)",
		"      the same at fetch_order_after_create_order_number_1, fetch_order_after_create_order_number_2,",
		"      [create_order_3_lines] want=600 got=300, also at fetch_order_after_create_order_3_lines",
		"      [create_order_8_lines] want=1600 got=800, also at fetch_order_after_create_order_8_lines",
		"      2 more value(s) at create_order_9_lines, fetch_order_after_create_order_9_lines, create_order_10_lines,",
		"    [list_orders] changed    orders.1.total_minor want=900 got=700",
	}
	for _, w := range want {
		if !strings.Contains(out, w+"\n") {
			t.Errorf("lacks %q:\n%s", w, out)
		}
	}
	values := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "      [") {
			values++
		}
	}
	if values != foldedValues {
		t.Errorf("8 other values print %d lines of their own, want %d, the rest named on one wrapped line:\n%s", values, foldedValues, out)
	}
	for _, l := range lines[1:] {
		if len(l) > wrapAt {
			t.Errorf("a folded line is %d wide, past %d: %q", len(l), wrapAt, l)
		}
	}
	for _, step := range append(steps, "list_orders") {
		if !strings.Contains(out, " "+step+",") && !strings.Contains(out, " "+step+"\n") && !strings.Contains(out, "["+step+"]") {
			t.Errorf("%s is not named:\n%s", step, out)
		}
	}
}

func TestGateVerboseKeepsAFoldAtOneValueOnOneLine(t *testing.T) {
	g := &gateChain{name: "one", failed: true, shown: []string{
		"[make] changed    thing.n want=1 got=2",
		"[get] changed    thing.n want=1 got=2",
		"[list] changed    things.0.n want=1 got=2",
		"[list] changed    things.1.n want=1 got=3",
	}}
	out := captureStdout(t, func() { g.printChanges(nil) })
	want := "    [make] changed    thing.n want=1 got=2 (and 1 more at get)\n" +
		"    [list] changed    things.0.n want=1 got=2 (and 1 more below)\n" +
		"      [list] things.1.n want=1 got=3\n"
	if out != want {
		t.Errorf("got:\n%swant:\n%s", out, want)
	}
}

func TestGateVerboseNamesEveryKnockOnStep(t *testing.T) {
	g := &gateChain{name: "one", failed: true}
	for _, step := range []string{"a", "b", "c", "d", "e"} {
		g.items = append(g.items, gateItem{Step: step, Path: "status", Want: "passed", Got: "failed", Reason: reason{Kind: reasonKnockOn, Step: "make", RPC: "x.v1.S/Make"}})
	}
	out := captureStdout(t, func() { g.printChanges(nil) })
	if !strings.HasPrefix(out, "    5 step(s) ") || !strings.HasSuffix(out, " (a, b, c, d, e)\n") {
		t.Errorf("got %q", out)
	}
}
