package main

import (
	"context"
	"strings"
	"testing"
)

func TestDiffMasksFixtureEchoesOfAChainRunFromAFileOutsideTheChainsDir(t *testing.T) {
	chdirToFakeShop(t, newFakeShop())
	writeFile(t, ".shrt/scratch/probe.yaml", `name: probe
steps:
    - id: create
      call: shop.catalog.v1.ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}-a
        name: Probe
        price_minor: "5"
      expect:
        - path: status.code
          equals: SUCCESS
`)
	ctx := context.Background()
	for range 2 {
		captureStdout(t, func() {
			if err := runRun(ctx, []string{".shrt/scratch/probe.yaml", "-quiet"}); err != nil {
				t.Fatalf("shrt run: %v", err)
			}
		})
	}
	var err error
	out := captureStdout(t, func() { err = runDiff(ctx, []string{"probe"}) })
	if err != nil || !strings.Contains(out, "echoing the fixture name") || strings.Contains(out, "product.sku") {
		t.Fatalf("the sku only echoes the tag the run sent, as verify masks it: %v\n%s", err, out)
	}
}
