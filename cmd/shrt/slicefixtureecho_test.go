package main

import (
	"context"
	"strings"
	"testing"
)

const skuEchoChain = `apiVersion: shrt/v1
name: sku-echo
vars:
    tag: sku-echo
steps:
    - id: create
      call: ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}
        price_minor: "5"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: get
      call: ProductService/GetProduct
      body:
        id_product: ${create.product.id_product}
      expect:
        - path: status.code
          equals: SUCCESS
        - path: product.sku
          equals: ${steps.create.request.sku}
`

func TestSliceVerifyMasksTheFixtureTagEchoedInAFailingValue(t *testing.T) {
	shop := newFakeShop()
	shop.skuEchoBug = true
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/chains/sku-echo.yaml", skuEchoChain)
	_ = runRun(context.Background(), []string{"sku-echo", "-quiet", "-var", "tag=src1"})
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"sku-echo", "-step", "get", "-run", "latest", "-verify", "-var", "tag=sl1"})
	})
	if !strings.Contains(out, "verify reproduced 3/3") || exitCodeOf(err) != 0 {
		t.Fatalf("source want sku-src1 got wrong-sku-src1 and slice want sku-sl1 got wrong-sku-sl1 differ only by the fixture tag, so the slice reproduced the failure: %v\n%s", err, out)
	}
}
