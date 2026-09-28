package contract_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func wantTransportRefusal(t *testing.T, st *chain.Step) {
	t.Helper()
	wantExpect(t, st, "transport.code", "invalid_argument")
}

func TestPlanSendsMalformedRequestsTheContractSaysAreInvalid(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "CreateOrder")
	empty := planStep(t, p, "create_order_lines_empty")
	wantTransportRefusal(t, empty)
	if n := len(empty.Body["lines"].([]any)); n != 0 {
		t.Fatalf("create_order_lines_empty sends no lines, got %d:\n%s", n, text)
	}
	zero := planStep(t, p, "create_order_qty_zero")
	wantTransportRefusal(t, zero)
	if got := bodyAt(t, zero, "lines.1.qty"); got != "0" {
		t.Fatalf("the last line asks for 0, got %s:\n%s", got, text)
	}
	if got := bodyAt(t, zero, "lines.0.qty"); got == "0" {
		t.Fatalf("only one line is malformed:\n%s", text)
	}
	wantTransportRefusal(t, planStep(t, p, "create_order_qty_negative"))

	for rpc, want := range map[string]map[string]any{
		"CreateProduct":  {"create_product_sku_empty": "", "create_product_sku_blank": "   ", "create_product_name_empty": "", "create_product_name_blank": "   "},
		"CreateCustomer": {"create_customer_email_no_at": "cust-${vars.tag}.example.test"},
		"GetProduct":     {"get_product_id_product_empty": ""},
		"AddStockBatch":  {"add_stock_batch_lines_empty": "[]"},
	} {
		p, text, _ := shopDemoPlan(t, rpc)
		for id, value := range want {
			st := planStep(t, p, id)
			wantTransportRefusal(t, st)
			found := false
			for _, v := range st.Body {
				if fmt.Sprint(v) == fmt.Sprint(value) {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s sends %q somewhere in its body, got %v:\n%s", id, value, st.Body, text)
			}
		}
	}
}

func TestPlanNotesAnRpcWhoseContractDeclaresNoValidation(t *testing.T) {
	cat, _ := shopDemo(t)
	dir := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("testdata", "shopdemo", "contracts", "customers.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(raw), "        required: [email]\n", "", 1)
	start := strings.Index(text, "            - connect_code: invalid_argument\n              reason: EmailInvalid")
	end := strings.Index(text, "            - code: 1101")
	text = text[:start] + text[end:]
	if err := os.WriteFile(filepath.Join(dir, "customers.yaml"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, broken, err := contract.LoadLibraryIn(dir, cat)
	if err != nil || len(broken) > 0 {
		t.Fatalf("load: %v %v", err, broken)
	}
	p, err := contract.BuildPlanFor([]string{"CreateCustomer"}, lib, cat, "c")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range p.Chain.Steps {
		for _, e := range st.Expect {
			if e.Path == "transport.code" && fmt.Sprint(e.Equals) == "invalid_argument" {
				t.Fatalf("%s guesses a validation nothing declares", st.ID)
			}
		}
	}
	if notes := strings.Join(p.Notes, "\n"); !strings.Contains(notes, "does not guess what the handler validates") {
		t.Fatalf("the plan says it planned no malformed request: %s", notes)
	}
}
