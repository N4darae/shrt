package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAKeptRedGotOnARedactedPathIsRefusedBeforeAnythingIsSent(t *testing.T) {
	r := rawStockRunner(t, `{"status":{"code":"SUCCESS"},"qtyOnHand":"5"}`)
	for _, got := range []string{"5", "<redacted>"} {
		c := normalized(t, keptRedChain(5, 7, chain.Pin{Step: "second", Path: "qty_on_hand", Got: strp(got)}))
		_, err := r.Run(context.Background(), c, runner.Options{Redact: []string{"**.qty_on_hand"}})
		if err == nil || !strings.Contains(err.Error(), "redact") || !strings.Contains(err.Error(), "qty_on_hand") ||
			!strings.Contains(err.Error(), "nothing was sent") {
			t.Fatalf("got %q on a redacted path is compared with <redacted>, never with the value; want a refusal, got %v", got, err)
		}
	}
	c := normalized(t, keptRedChain(5, 7, chain.Pin{Step: "second", Path: "qty_on_hand"}))
	rec, err := r.Run(context.Background(), c, runner.Options{Redact: []string{"**.qty_on_hand"}})
	if err != nil || rec.KeptRed != runner.KeptRedAsPinned {
		t.Fatalf("a pin without got on a redacted path still works; got %v %+v", err, rec)
	}
}

func TestAKeptRedGotOnARedactedPathIsALintErrorAndALoadErrorForChainRedact(t *testing.T) {
	c := keptRedChain(5, 7, chain.Pin{Step: "second", Path: "qty_on_hand", Got: strp("5")})
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, i := range chain.LintWith(c, catalogtest.Shop(), chain.LintOptions{Redact: []string{"**.qty_on_hand"}}) {
		if i.IsError() && strings.Contains(i.Message, "kept_red") && strings.Contains(i.Message, "redact") {
			found = true
		}
	}
	if !found {
		t.Fatalf("lint must error on a kept_red got on a redacted path: %+v", chain.LintWith(c, catalogtest.Shop(), chain.LintOptions{Redact: []string{"**.qty_on_hand"}}))
	}
	own := keptRedChain(5, 7, chain.Pin{Step: "second", Path: "qty_on_hand", Got: strp("5")})
	own.Redact = []string{"**.qty_on_hand"}
	if err := own.Normalize(); err == nil || !strings.Contains(err.Error(), "redact") {
		t.Fatalf("the chain's own redact covers the pinned path, so load must refuse it; got %v", err)
	}
}
