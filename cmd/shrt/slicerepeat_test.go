package main

import (
	"context"
	"strings"
	"testing"
)

const flakyGetChain = `apiVersion: shrt/v1
name: probe-get
steps:
    - id: create
      call: ProductService/CreateProduct
      body:
        sku: sku-${uuid}
        price_minor: "5"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: get_a
      call: ProductService/GetProduct
      body:
        id_product: ${create.product.id_product}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: get_b
      call: ProductService/GetProduct
      body:
        id_product: ${create.product.id_product}
      expect:
        - path: status.code
          equals: SUCCESS
`

func sliceFlaky(t *testing.T, failEvery int, args ...string) (string, error) {
	t.Helper()
	shop := newFakeShop()
	shop.getProductFailN = failEvery
	if failEvery == 0 {
		shop.getProductFailAt = map[int]bool{2: true, 3: true, 4: true, 5: true}
	}
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/scratch/probe-get.yaml", flakyGetChain)
	_ = runRun(context.Background(), []string{".shrt/scratch/probe-get.yaml", "-quiet", "-keep-going"})
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), append([]string{".shrt/scratch/probe-get.yaml", "-step", "get_b", "-run", "latest", "-verify"}, args...))
	})
	return out, err
}

func TestSliceVerifyRepeatsAndCallsAPartialMatchIntermittent(t *testing.T) {
	out, err := sliceFlaky(t, 0)
	if !strings.Contains(out, "intermittent: reproduced 1/3") {
		t.Fatalf("GetProduct calls 2 to 5 fail, so of three slice runs, each re-sending a failed read once, one reproduces the source run's internal error; "+
			"a single-run receipt either way would be wrong:\n%s", out)
	}
	if got := exitCodeOf(err); got != 1 {
		t.Fatalf("an intermittent reproduction exits %d, want 1:\n%s", got, out)
	}
	if strings.Count(out, "verify reproduced") > 0 || strings.Contains(out, "verify NOT REPRODUCED") {
		t.Fatalf("no single run's verdict may stand for the whole:\n%s", out)
	}
}

func TestSliceVerifyRepeatReportsEveryRunReproduced(t *testing.T) {
	out, err := sliceFlaky(t, 1)
	if !strings.Contains(out, "verify reproduced 3/3") {
		t.Fatalf("every GetProduct fails, so both runs reproduce:\n%s", out)
	}
	if got := exitCodeOf(err); got != 0 {
		t.Fatalf("reproduced in every run exits %d, want 0:\n%s", got, out)
	}
}
