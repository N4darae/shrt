package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func sameEmailChain(t *testing.T) *chain.Chain {
	ok := []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}
	taken := []chain.Expectation{{Path: "status.details.0.reason", Equals: "EmailTaken"}}
	c := &chain.Chain{Name: "customers", Vars: map[string]any{"tag": "t"}, Steps: []*chain.Step{
		{ID: "create_customer", Call: "shop.v1.CustomerService/CreateCustomer", Body: map[string]any{"email": "cus-${vars.tag}-a@example.test", "name": "Customer ${vars.tag}"}, Expect: ok},
		{ID: "create_customer_same_email", Call: "shop.v1.CustomerService/CreateCustomer", Body: map[string]any{"email": "${steps.create_customer.request.email}"}, Expect: taken},
		{ID: "create_customer_other", Call: "shop.v1.CustomerService/CreateCustomer", Body: map[string]any{"email": "cus-${vars.tag}-b@example.test", "name": "Customer ${vars.tag}"}, Expect: ok},
		{ID: "create_customer_fixed", Call: "shop.v1.CustomerService/CreateCustomer", Body: map[string]any{"email": "CUS-A@EXAMPLE.TEST"}, Expect: ok},
		{ID: "create_customer_same_email_case", Call: "shop.v1.CustomerService/CreateCustomer", Body: map[string]any{"email": "CUS-${vars.tag}-A@EXAMPLE.TEST", "name": "Customer ${vars.tag}"}, Expect: taken},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func keptByID(res *chain.SliceResult) map[string]chain.Keep {
	kept := map[string]chain.Keep{}
	for _, k := range res.Kept {
		kept[k.ID] = k
	}
	return kept
}

func TestSliceKeepsAnEarlierWriteSendingTheUniqueValueTheTargetSendsAgain(t *testing.T) {
	c := sameEmailChain(t)
	emailOnly := func(rpc, field string) (bool, bool) { return field == "email", true }
	res, err := chain.Slice(c, "create_customer_same_email_case", chain.SliceOptions{KeyField: emailOnly})
	if err != nil {
		t.Fatal(err)
	}
	kept := keptByID(res)
	if k, in := kept["create_customer"]; !in || !strings.Contains(k.Reason, "email") {
		t.Fatalf("the create whose email the target repeats in another case is what makes it a duplicate: %+v", res.Kept)
	}
	for _, id := range []string{"create_customer_other", "create_customer_fixed", "create_customer_same_email"} {
		if _, in := kept[id]; in {
			t.Fatalf("%s sends no run-unique value the target repeats: %+v", id, res.Kept)
		}
	}
	res, err = chain.Slice(c, "create_customer_same_email_case", chain.SliceOptions{KeyField: func(string, string) (bool, bool) { return false, true }})
	if err != nil {
		t.Fatal(err)
	}
	if _, in := keptByID(res)["create_customer"]; in {
		t.Fatalf("a contract declaring no key field ties nothing by value: %+v", res.Kept)
	}
	res, err = chain.Slice(c, "create_customer_same_email_case", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	kept = keptByID(res)
	if _, in := kept["create_customer"]; !in {
		t.Fatalf("without contracts the same var-built value on the same field ties the writes: %+v", res.Kept)
	}
	if _, in := kept["create_customer_other"]; !in {
		t.Fatalf("without contracts the shared name ties create_customer_other too: %+v", res.Kept)
	}
}
