package main

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func sliceShop(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), append([]string{".shrt/scratch/probe-orders.yaml", "-step", "cancel_confirmed", "-run", "latest", "-verify"}, args...))
	})
	return out, err
}

func nextCommand(out string) []string {
	m := regexp.MustCompile(`next: shrt chain slice (.*)`).FindStringSubmatch(out)
	if m == nil {
		return nil
	}
	return strings.Fields(m[1])
}

func TestSliceKeepSuggestsOnlyWritesOnTheEntitiesTheKeptStepsUse(t *testing.T) {
	shop := newFakeShop()
	shop.cancelConfirmedBug = true
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/scratch/probe-orders.yaml", cancelConfirmedChain)
	_ = runRun(context.Background(), []string{".shrt/scratch/probe-orders.yaml", "-quiet", "-var", "tag=src"})

	out, err := sliceShop(t, "-var", "tag=s1")
	if exitCodeOf(err) != 1 {
		t.Fatalf("without the confirm the cancel succeeds, so the slice is not reproduced (exit %d):\n%s", exitCodeOf(err), out)
	}
	next := nextCommand(out)
	keep := ""
	for i, f := range next {
		if f == "-keep" && i+1 < len(next) {
			keep = next[i+1]
		}
	}
	if keep != "add_stock,confirm_single" {
		t.Fatalf("the confirm acts on the order the target cancels and the stock add on the product its line holds; "+
			"no other dropped write touches those, so next must keep exactly those two, got -keep %q:\n%s", keep, out)
	}

	next = append(next[:len(next):len(next)], "-var", "tag=s2")
	again := captureStdout(t, func() { err = chainSlice(context.Background(), next) })
	if exitCodeOf(err) != 0 || !strings.Contains(again, "verify reproduced") {
		t.Fatalf("with the confirm and the stock kept the verdict matched and every other dropped write acts on another entity, "+
			"so the slice reproduced (exit %d):\n%s", exitCodeOf(err), again)
	}
	if strings.Contains(again, "INCONCLUSIVE") {
		t.Fatalf("writes on other entities do not make a matching verdict inconclusive:\n%s", again)
	}
	for _, id := range []string{"create_product_2", "create_order_three", "cancel_pending", "retry_single"} {
		if !strings.Contains(again, id) {
			t.Errorf("the dropped write %s is still named, as information:\n%s", id, again)
		}
	}
}

func TestSliceTreatsAnAnsweredWriteThatFailedAnExpectationAsDone(t *testing.T) {
	shop := newFakeShop()
	shop.cancelConfirmedBug = true
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/scratch/probe-orders.yaml", strings.Replace(cancelConfirmedChain,
		"        qty: \"50\"\n      expect:\n        - path: status.code\n          equals: SUCCESS\n",
		"        qty: \"50\"\n      expect:\n        - path: status.code\n          equals: SUCCESS\n        - path: qty_on_hand\n          equals: \"999\"\n", 1))
	_ = runRun(context.Background(), []string{".shrt/scratch/probe-orders.yaml", "-quiet", "-keep-going", "-var", "tag=src"})

	out, err := sliceShop(t, "-keep", "confirm_single", "-var", "tag=s1")
	if exitCodeOf(err) != 2 {
		t.Fatalf("without the stock the confirm is refused and the slice stops before the target (exit %d):\n%s", exitCodeOf(err), out)
	}
	if strings.Contains(out, "No -keep command can reproduce") {
		t.Fatalf("add_stock was answered and only an expectation failed, so its write took effect and it can be kept:\n%s", out)
	}
	next := nextCommand(out)
	if next == nil || !strings.Contains(strings.Join(next, " "), "add_stock") {
		t.Fatalf("next must keep add_stock, the write on the product the confirm needs stock for:\n%s", out)
	}
	next = append(next[:len(next):len(next)], "-var", "tag=s2")
	again := captureStdout(t, func() { err = chainSlice(context.Background(), next) })
	if exitCodeOf(err) != 0 || !strings.Contains(again, "verify reproduced") {
		t.Fatalf("with add_stock kept and its failed expectation relaxed the slice reproduces (exit %d):\n%s", exitCodeOf(err), again)
	}
	if !strings.Contains(again, "add_stock qty_on_hand equals") {
		t.Errorf("the slice must say which expectation it relaxed:\n%s", again)
	}
}
