package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestABatchItemWithNoVerdictIsFlaggedWhenItsSiblingsCarryOne(t *testing.T) {
	defer chain.SetItemEnvelope("")
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	chain.SetItemEnvelope("results[].status.code")

	for _, raw := range []string{
		`{"status":{"code":"SUCCESS"},"results":[{"status":{"code":"SUCCESS"}},{"status":null,"qty_on_hand":"0"}]}`,
		`{"status":{"code":"SUCCESS"},"results":[{"status":{"code":"SUCCESS"}},{"qty_on_hand":"0"}]}`,
	} {
		got, err := chain.ItemRefusals(decodeBody(t, raw))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Line != "results.1" || !strings.Contains(got[0].String(), "no verdict") {
			t.Fatalf("results.1 carries no verdict while results.0 says SUCCESS explicitly, so nothing says "+
				"line 1 succeeded: it must be reported like a refused line; got %v for %s", got, raw)
		}
	}
}

func TestABatchWhereNoItemCarriesAVerdictIsStillHealthy(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	body := decodeBody(t, `{"error":{"code":"OK"},"results":[{"error":null},{"error":{"code":"invalid_argument"}},{"error":null}]}`)
	got, err := chain.ItemRefusals(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Line != "results.1" {
		t.Fatalf("on a backend that writes an item's error only when it refuses it, an unset one is success; "+
			"only the refused line is reported, got %v", got)
	}
}
