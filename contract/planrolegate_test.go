package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func shopDemoPlanWith(t *testing.T, opts contract.PlanOptions, targets ...string) (*contract.Plan, string, string) {
	t.Helper()
	cat, lib := shopDemo(t)
	p, err := contract.BuildPlanWith(targets, lib, cat, "shopdemo", opts)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.YAML()
	if err != nil {
		t.Fatal(err)
	}
	return p, string(raw), strings.Join(p.Notes, "\n")
}

func TestPlanForARoleGatedWriteCallsItAsTheLowerProfileAndProvesNoEffect(t *testing.T) {
	p, text, notes := shopDemoPlanWith(t, contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}}, "AddStock")
	denied := planStep(t, p, "add_stock_as_clerk")
	if denied.Auth != "clerk" {
		t.Fatalf("the probe runs as the lower profile, got auth %q:\n%s", denied.Auth, text)
	}
	wantExpect(t, denied, "status.details.0.reason", "PermissionDenied")
	wantExpect(t, denied, "status.details.0.app_code", 1603)

	bare := planStep(t, p, "add_stock_without_token")
	if !bare.SkipAuth {
		t.Fatalf("the missing-token probe sends no token:\n%s", text)
	}
	wantExpect(t, bare, "transport.code", "unauthenticated")
	bad := planStep(t, p, "add_stock_with_bad_token")
	if bad.Auth != "invalid" {
		t.Fatalf("the invalid-token probe uses auth: invalid:\n%s", text)
	}
	wantExpect(t, bad, "transport.code", "unauthenticated")
	for _, st := range []string{"add_stock_as_clerk", "add_stock_without_token", "add_stock_with_bad_token"} {
		if strings.HasSuffix(st, "token") && len(expectsOnPrefix(planStep(t, p, st), "status")) > 0 {
			t.Fatalf("a transport refusal carries no body, so %s asserts nothing in it:\n%s", st, text)
		}
	}

	ids := stepIDs(p)
	before, after := idAt(t, ids, "get_product_before_add_stock_denied"), idAt(t, ids, "get_product_after_add_stock_denied")
	for _, st := range []string{"add_stock_as_clerk", "add_stock_without_token", "add_stock_with_bad_token"} {
		if at := idAt(t, ids, st); at < before || at > after {
			t.Fatalf("%s sits between the reads that prove it changed nothing: %s", st, strings.Join(ids, ", "))
		}
	}
	wantExpect(t, planStep(t, p, "get_product_after_add_stock_denied"), "product.qty_on_hand", "${get_product_before_add_stock_denied.product.qty_on_hand}")
	if !strings.Contains(notes, "add_stock_as_clerk") || !strings.Contains(notes, "without_token") {
		t.Fatalf("the plan says what the denial probes are for: %s", notes)
	}
}

func TestPlanProbesTheTokenOncePerPlanNotPerRPC(t *testing.T) {
	p, text, _ := shopDemoPlanWith(t, contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}}, "AddStock", "CreateOrder")
	n := 0
	for _, st := range p.Chain.Steps {
		if st.SkipAuth {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("one missing-token probe per plan keeps the chain short, got %d:\n%s", n, text)
	}
}

func TestPlanWithoutAuthConfiguredAddsNoRoleOrTokenProbe(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "AddStock")
	for _, st := range p.Chain.Steps {
		if st.SkipAuth || st.Auth != "" {
			t.Fatalf("with no auth configured there is no profile to probe with:\n%s", text)
		}
	}
}

func expectsOnPrefix(st *chain.Step, prefix string) []string {
	out := []string{}
	for _, e := range st.Expect {
		if strings.HasPrefix(e.Path, prefix) {
			out = append(out, e.Path)
		}
	}
	return out
}
