package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

const minimalScratchChain = `apiVersion: shrt/v1
name: min-stock
vars:
    tag: min
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}
        price_minor: "5"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: peek
      call: ProductService/GetProduct
      body:
        id_product: ${create_product.product.id_product}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "3"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: get_after
      call: ProductService/GetProduct
      body:
        id_product: ${create_product.product.id_product}
      expect:
        - path: status.code
          equals: SUCCESS
`

func minimalScratch(t *testing.T) {
	t.Helper()
	chdirToFakeShop(t, newFakeShop())
	writeFile(t, ".shrt/scratch/min-stock.yaml", minimalScratchChain)
	if err := runRun(context.Background(), []string{".shrt/scratch/min-stock.yaml", "-quiet", "-var", "tag=src"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
}

func TestSliceWriteOfAScratchChainLandsNextToItNotInTheChainsDirectory(t *testing.T) {
	minimalScratch(t)
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{".shrt/scratch/min-stock.yaml", "-step", "get_after", "-keep", "writes", "-write"})
	})
	if err != nil {
		t.Fatalf("slice -write: %v\n%s", err, out)
	}
	if _, err := os.Stat(".shrt/chains/min-stock-slice-get_after.yaml"); err == nil {
		t.Fatalf("a slice of a scratch chain must not land in paths.chains, where every sweep and gate runs it:\n%s", out)
	}
	if _, err := os.Stat(".shrt/scratch/min-stock-slice-get_after.yaml"); err != nil {
		t.Fatalf("the slice of .shrt/scratch/min-stock.yaml belongs next to it: %v\n%s", err, out)
	}
}

func TestSliceWriteMayReplaceTheSourceWhenItIsNamedExplicitly(t *testing.T) {
	minimalScratch(t)
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{".shrt/scratch/min-stock.yaml", "-step", "get_after", "-run", "latest",
			"-keep", "writes", "-verify", "-repeat", "1", "-var", "tag=v1", "-write", ".shrt/scratch/min-stock.yaml"})
	})
	if strings.Contains(out+errText(err), "already exists and is not a slice") {
		t.Fatalf("the source file itself was named with -write, so replacing it is what was asked:\n%s\n%v", out, err)
	}
	if exitCodeOf(err) != 0 {
		t.Fatalf("the slice reproduces, exit %d: %v\n%s", exitCodeOf(err), err, out)
	}
	raw := string(mustRead(t, ".shrt/scratch/min-stock.yaml"))
	if !strings.Contains(raw, "VERIFIED by 'shrt chain slice -verify'") || strings.Contains(raw, "id: peek") {
		t.Fatalf("the source now holds the verified slice:\n%s", raw)
	}
}

func TestSliceWriteRefusalIsNeverExitZero(t *testing.T) {
	minimalScratch(t)
	writeFile(t, ".shrt/scratch/other.yaml", "apiVersion: shrt/v1\nname: other\nsteps: []\n")
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{".shrt/scratch/min-stock.yaml", "-step", "get_after", "-run", "latest",
			"-keep", "writes", "-verify", "-var", "tag=v2", "-write", ".shrt/scratch/other.yaml"})
	})
	if !strings.Contains(errText(err), "already exists and is not a slice") {
		t.Fatalf("an unrelated file is not overwritten: %v\n%s", err, out)
	}
	if exitCodeOf(err) != 2 {
		t.Fatalf("under -verify a refusal before anything was sent exits 2, got %d", exitCodeOf(err))
	}
	err = chainSlice(context.Background(), []string{".shrt/scratch/min-stock.yaml", "-step", "get_after", "-keep", "writes", "-write", ".shrt/scratch/other.yaml"})
	if exitCodeOf(err) != 1 {
		t.Fatalf("without -verify the refusal exits 1, got %d (%v)", exitCodeOf(err), err)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
