package chain_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func decodeBody(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestItemRefusalsAreInvisibleUntilTheConventionIsDeclared(t *testing.T) {
	defer chain.SetItemEnvelope("")
	body := decodeBody(t, `{"error":{"code":"OK"},"results":[
		{"error":{"code":"invalid_argument","message":"give_qty must be an integer"}},
		{"error":{"code":"OK"}}]}`)

	chain.SetItemEnvelope("")
	if got, _ := chain.ItemRefusals(body); len(got) != 0 {
		t.Fatalf("with no item envelope declared the runner must behave exactly as before, got %v", got)
	}

	chain.SetItemEnvelope("results[].error.code")
	got, _ := chain.ItemRefusals(body)
	if len(got) != 1 {
		t.Fatalf("want the one refused line reported, got %v — every line of this batch was refused "+
			"while the top-level envelope said OK, which is the shape that reads as success", got)
	}
}

func TestItemEnvelopeIgnoresABatchWhereEveryItemSucceeded(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	body := decodeBody(t, `{"error":{"code":"OK"},"results":[{"error":{"code":"OK"}},{"error":{"code":"OK"}}]}`)
	if got, _ := chain.ItemRefusals(body); len(got) != 0 {
		t.Errorf("a batch whose items all succeeded must not be reported: %v", got)
	}
}

func TestItemEnvelopeIsQuietWhenTheResponseHasNoSuchList(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	body := decodeBody(t, `{"error":{"code":"OK"},"id_deal":"abc"}`)
	if got, _ := chain.ItemRefusals(body); len(got) != 0 {
		t.Errorf("a non-batch response must not be reported just because the convention is set: %v", got)
	}
}

func TestAMisPointedItemEnvelopeFailsLoudlyInsteadOfSilently(t *testing.T) {
	defer chain.SetItemEnvelope("")
	body := decodeBody(t, `{"status":{"code":"OK"},"results":[
		{"outcome":{"code":"REFUSED"}},{"outcome":{"code":"REFUSED"}}]}`)

	chain.SetItemEnvelope("results[].error.code")
	got, err := chain.ItemRefusals(body)
	if err == nil {
		t.Fatalf("a path whose list resolves but whose item field does not is a MISCONFIGURATION, and "+
			"returning %v with no error means every gate goes green while a batch refuses every line — "+
			"the exact failure the setting exists to prevent", got)
	}

	chain.SetItemEnvelope("results.error.code")
	if _, err := chain.ItemRefusals(body); err == nil {
		t.Error("a path with no [] separator cannot check anything and must say so")
	}

	chain.SetItemEnvelope("status[].code")
	if _, err := chain.ItemRefusals(body); err == nil {
		t.Error("a path naming something that is not a list must say so")
	}
}

func TestANonBatchResponseIsNotAMisconfiguration(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("results[].error.code")
	body := decodeBody(t, `{"error":{"code":"OK"},"id_deal":"abc"}`)
	got, err := chain.ItemRefusals(body)
	if err != nil || len(got) != 0 {
		t.Errorf("most rpcs are not batches; a response with no such list must stay silent, got %v / %v", got, err)
	}
}

func TestDeclaresVerdictNeedsAValueOnExactlyThatPath(t *testing.T) {
	yes := true
	cases := []struct {
		name string
		e    chain.Expectation
		want bool
	}{
		{"equals on the path", chain.Expectation{Path: "results.1.error.code", Equals: "invalid_argument"}, true},
		{"not_equal on the path", chain.Expectation{Path: "results.1.error.code", NotEqual: "OK"}, true},
		{"contains on the path", chain.Expectation{Path: "results.1.error.code", Contains: "invalid"}, true},
		{"bracket spelling of the same path", chain.Expectation{Path: "results[1].error.code", Equals: "invalid_argument"}, true},
		{"exists says nothing about the value", chain.Expectation{Path: "results.1.error.code", Exists: &yes}, false},
		{"not_empty says nothing about the value", chain.Expectation{Path: "results.1.error.code", NotEmpty: true}, false},
		{"a different line", chain.Expectation{Path: "results.0.error.code", Equals: "invalid_argument"}, false},
		{"a detail under the verdict is not the verdict", chain.Expectation{Path: "results.1.error.message", Equals: "x"}, false},
	}
	for _, c := range cases {
		if got := chain.DeclaresVerdict([]chain.Expectation{c.e}, "results.1.error.code"); got != c.want {
			t.Errorf("%s: DeclaresVerdict = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestUndeclaredRefusalsKeepsOnlyTheLinesTheStepDidNotPin(t *testing.T) {
	refusals := []chain.ItemRefusal{
		{Path: "results.0.error.code", Code: "not_found"},
		{Path: "results.1.error.code", Code: "invalid_argument"},
	}
	expect := []chain.Expectation{{Path: "results.0.error.code", Equals: "not_found"}}
	got := chain.UndeclaredRefusals(refusals, expect)
	if len(got) != 1 || got[0].Path != "results.1.error.code" {
		t.Fatalf("want only the undeclared line 1, got %v", got)
	}
	if got[0].String() != "results.1.error.code = invalid_argument" {
		t.Errorf("String() = %q", got[0].String())
	}
	if rest := chain.UndeclaredRefusals(refusals, nil); len(rest) != 2 {
		t.Errorf("with nothing declared every refusal is a surprise, got %v", rest)
	}
}

func TestItemEnvelopeDeclaredReadsTheResponseSchema(t *testing.T) {
	defer chain.SetItemEnvelope("")
	verdict := []*catalog.Field{{Name: "code", Kind: "string"}}
	withVerdict := []*catalog.Field{
		{Name: "error", Kind: "message", Fields: verdict},
		{Name: "results", Kind: "message", Repeated: true, Fields: []*catalog.Field{
			{Name: "error", Kind: "message", Fields: verdict},
			{Name: "amount", Kind: "string"},
		}},
	}
	receiptOnly := []*catalog.Field{
		{Name: "error", Kind: "message", Fields: verdict},
		{Name: "results", Kind: "message", Repeated: true, Fields: []*catalog.Field{{Name: "id", Kind: "string"}}},
	}
	notAList := []*catalog.Field{
		{Name: "results", Kind: "message", Fields: []*catalog.Field{{Name: "error", Kind: "message", Fields: verdict}}},
	}
	truncatedList := []*catalog.Field{
		{Name: "results", Kind: "message", Repeated: true, Truncated: true},
	}

	if chain.ItemEnvelopeDeclared(withVerdict) {
		t.Error("with the convention unset nothing is declared")
	}
	chain.SetItemEnvelope("results[].error.code")
	if !chain.ItemEnvelopeDeclared(withVerdict) {
		t.Error("results[] carrying error.code is exactly the declared shape")
	}
	if chain.ItemEnvelopeDeclared(receiptOnly) {
		t.Error("a results[] whose items have no error field is an atomic receipt list, not a per-item envelope")
	}
	if chain.ItemEnvelopeDeclared(notAList) {
		t.Error("a singular results message is not a list, so no per-item verdict exists")
	}
	if !chain.ItemEnvelopeDeclared(truncatedList) {
		t.Error("a list the schema walk could not descend into must be given the benefit of the doubt, as lint does")
	}
}

func TestValidateItemEnvelopeAgainstAWholeDescriptor(t *testing.T) {
	defer chain.SetItemEnvelope("")
	chain.SetItemEnvelope("")
	if err := chain.ValidateItemEnvelope(catalogtest.New()); err != nil {
		t.Fatalf("unset is always valid: %v", err)
	}
	chain.SetItemEnvelope("results[].error.code")
	if err := chain.ValidateItemEnvelope(catalogtest.New()); err == nil {
		t.Error("the base fixture has no response with a results list, so the convention names nothing and must be refused")
	}
	if err := chain.ValidateItemEnvelope(catalogtest.Batch()); err != nil {
		t.Errorf("PreviewResponse in the batch fixture declares it: %v", err)
	}
}
