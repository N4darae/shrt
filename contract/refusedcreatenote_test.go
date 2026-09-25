package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestARefusedCreateIsNotToldThatNoReadTakesItsID(t *testing.T) {
	p, _, _ := shopDemoPlanWith(t, contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}}, "CreateProduct")
	notes := strings.Join(p.Notes, "\n")
	for _, id := range []string{"create_product_price_minor_below_min", "create_product_as_clerk"} {
		planStep(t, p, id)
		if strings.Contains(notes, "step "+id+": no read rpc") {
			t.Fatalf("%s is a create expected to be refused: it leaves no id, so there is nothing a read could take:\n%s", id, notes)
		}
	}
	if !strings.Contains(notes, "no id to read back") {
		t.Fatalf("the plan says once why a refused create is not read back:\n%s", notes)
	}
}

func TestAWriteWhoseReaderAnswersOnlyTextIsNotToldNoReadTakesItsID(t *testing.T) {
	p, _, _ := shopDemoPlanWith(t, contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}}, "CreateCustomer")
	notes := strings.Join(p.Notes, "\n")
	if strings.Contains(notes, "no read rpc in the contracts takes the id") {
		t.Fatalf("GetCustomer takes id_customer from CreateCustomer, so the note must not say no read takes it:\n%s", notes)
	}
	if !strings.Contains(notes, "GetCustomer takes the id of what it changes") {
		t.Fatalf("the note names the read that exists and why it cannot compare profiles:\n%s", notes)
	}
}
