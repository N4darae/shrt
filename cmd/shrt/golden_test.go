package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/golden from the current output")

var goldenNoise = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`127\.0\.0\.1:\d+`), "127.0.0.1:PORT"},
	{regexp.MustCompile(`/tmp/[^\s:"']*`), "TMP"},
	{regexp.MustCompile(`\d{8}T\d{6}(\.\d+)?Z?(-[0-9a-f]+)?`), "RUNID"},
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`), "TIME"},
	{regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`), "UUID"},
	{regexp.MustCompile(`(?m) +\d+ms$`), " DUR"},
	{regexp.MustCompile(`\b\d+(\.\d+)?(ms|µs|s)\b`), "DUR"},
	{regexp.MustCompile(`digest: +[0-9a-f]+`), "digest: DIGEST"},
}

func shrtOut(t *testing.T, name string, args ...string) (string, int) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		errOut := captureStderr(t, func() { err = commands[name].run(context.Background(), args) })
		defer func() { fmt.Print(errOut) }()
	})
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		var shown shownError
		if !errors.As(err, &shown) {
			out += "shrt " + name + ": " + err.Error() + "\n"
		} else if _, rest, ok := strings.Cut(err.Error(), "\n"); ok {
			out += rest + "\n"
		}
	}
	for _, n := range goldenNoise {
		out = n.re.ReplaceAllString(out, n.with)
	}
	return out, exitCodeOf(err)
}

func checkGolden(t *testing.T, file, got string) {
	t.Helper()
	path := filepath.Join(goldenDir, file)
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -run TestGolden -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the current output (go test -run TestGolden -update rewrites it):\n--- want\n%s--- got\n%s", file, want, got)
	}
}

var goldenDir = func() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "testdata", "golden")
}()

const goldenProductChain = `apiVersion: shrt/v1
name: shop-product
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: sku-${uuid}
        price_minor: "250"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: get_product
      call: ProductService/GetProduct
      body:
        id_product: ${create_product.product.id_product}
      expect:
        - path: product.price_minor
          equals: "250"
`

func TestGoldenOutput(t *testing.T) {
	shop := newFakeShop()
	chdirToFakeShop(t, shop)
	inProcessGate(t)
	writeFile(t, ".shrt/chains/probe-orders.yaml", strings.Replace(cancelConfirmedChain, "vars:\n    tag: probe\n", "", 1))
	writeFile(t, ".shrt/chains/shop-product.yaml", goldenProductChain)
	golden := map[string]*strings.Builder{}
	seen := map[string]bool{}
	var runs []string
	step := func(name string, args ...string) {
		out, code := shrtOut(t, name, args...)
		files, _ := filepath.Glob(".shrt/runs/probe-orders/*.json")
		for _, f := range files {
			if id := strings.TrimSuffix(filepath.Base(f), ".json"); !seen[id] {
				seen[id] = true
				runs = append(runs, id)
			}
		}
		if golden[name] == nil {
			golden[name] = &strings.Builder{}
		}
		shown := strings.Join(args, " ")
		for _, n := range goldenNoise {
			shown = n.re.ReplaceAllString(shown, n.with)
		}
		fmt.Fprintf(golden[name], "$ shrt %s %s  [exit %d]\n%s\n", name, shown, code, out)
	}
	step("run", "probe-orders")
	step("confirm", "probe-orders", "-note", "orders flow")
	step("confirm", "probe-orders", "-approve", "-by", "alice@example.test")
	step("verify", "probe-orders")
	step("run", "probe-orders", "-quiet")
	step("verify", "probe-orders", "-quiet")
	shop.cancelConfirmedBug = true
	step("run", "probe-orders")
	failed := runs[len(runs)-1]
	step("run", "probe-orders", "-quiet")
	step("run", "probe-orders", "-repeat", "2")
	step("verify", "probe-orders")
	step("diff", runs[0], failed)
	step("gate", "-no-session-check", "-hollow-baseline", "")
	step("gate", "-v", "-no-session-check", "-hollow-baseline", "")
	step("chain", "slice", "probe-orders", "-step", "cancel_confirmed")
	step("chain", "slice", "probe-orders", "-step", "cancel_confirmed", "-run", failed, "-verify")
	step("chain", "slice", "probe-orders", "-without", "confirm_single", "-run", failed, "-verify")
	step("chain", "pin", "probe-orders")
	step("gate", "-no-session-check", "-hollow-baseline", "")
	for _, c := range gateCases() {
		golden["gate"].WriteString(renderGateCase(t, c) + "\n")
	}
	for name, b := range golden {
		checkGolden(t, name+".txt", b.String())
	}
}
