package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestABatchItemWithAnEmptyVerdictIsReportedWhenAnotherItemCarriesOne(t *testing.T) {
	defer chain.SetItemEnvelope("")
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	chain.SetItemEnvelope("results[].status.code")

	for raw, lines := range map[string][]string{
		`{"status":{"code":"SUCCESS"},"results":[{"status":{"code":""}},{"status":{"code":"SUCCESS"}}]}`:  {"results.0"},
		`{"status":{"code":"SUCCESS"},"results":[{"status":{"code":""}},{"status":{"code":"REJECTED"}}]}`: {"results.0", "results.1"},
	} {
		got, err := chain.ItemRefusals(decodeBody(t, raw))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(lines) {
			t.Fatalf("an item whose envelope is present with an empty code carries no verdict, like an empty top-level "+
				"code; want %v reported, got %v for %s", lines, got, raw)
		}
		for i, line := range lines {
			if got[i].Line != line {
				t.Fatalf("want %v reported in order, got %v for %s", lines, got, raw)
			}
		}
		if got[0].Code != chain.NoItemVerdict {
			t.Fatalf("the empty line must be reported as %s, got %v", chain.NoItemVerdict, got)
		}
	}
}

func TestABatchWhereEveryItemVerdictIsEmptyIsLeftAlone(t *testing.T) {
	defer chain.SetItemEnvelope("")
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	chain.SetItemEnvelope("results[].status.code")
	got, err := chain.ItemRefusals(decodeBody(t, `{"status":{"code":"SUCCESS"},"results":[{"status":{"code":""}},{"status":{"code":""}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a backend that never fills an item's code gives nothing to compare an empty one against, got %v", got)
	}
}
