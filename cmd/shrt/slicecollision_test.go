package main

import (
	"context"
	"strings"
	"testing"
)

const customerChain = `apiVersion: shrt/v1
name: %s
vars:
    tag: t
steps:
    - id: create_customer
      call: CustomerService/CreateCustomer
      body:
        email: c-${vars.tag}@example.test
      expect:
        - path: status.code
          equals: SUCCESS
        - path: customer.email
          equals: c-${vars.tag}@example.test
    - id: create_order
      call: OrderService/CreateOrder
      body:
        id_customer: ${create_customer.customer.id_customer}
        idempotency_key: k-${uuid}
      expect:
        - path: status.code
          equals: SUCCESS
`

func TestSliceVerifyDiagnosesAFixtureAnotherChainCreated(t *testing.T) {
	chdirToFakeShop(t, newFakeShop())
	writeFile(t, ".shrt/scratch/other.yaml", strings.Replace(customerChain, "%s", "other", 1))
	writeFile(t, ".shrt/scratch/customers.yaml", strings.Replace(customerChain, "%s", "customers", 1))
	if err := runRun(context.Background(), []string{".shrt/scratch/other.yaml", "-quiet", "-var", "tag=taken"}); err != nil {
		t.Fatalf("run other: %v", err)
	}
	if err := runRun(context.Background(), []string{".shrt/scratch/customers.yaml", "-quiet", "-var", "tag=src"}); err != nil {
		t.Fatalf("run customers: %v", err)
	}
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{".shrt/scratch/customers.yaml", "-step", "create_order", "-run", "latest",
			"-verify", "-var", "tag=taken"})
	})
	if exitCodeOf(err) != 3 {
		t.Fatalf("the create was refused before the target, so the slice did not run it (exit %d):\n%s", exitCodeOf(err), out)
	}
	if !strings.Contains(out, "fixture reused") || !strings.Contains(out, "chain other") {
		t.Fatalf("the refusal is a uniqueness conflict on a value run of chain other already sent; say so as run and verify do:\n%s", out)
	}
	if !strings.Contains(out, "-var tag=") {
		t.Fatalf("give the fresh -var that gets past it:\n%s", out)
	}
}
