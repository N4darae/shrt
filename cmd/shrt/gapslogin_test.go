package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestContractStatusGapsLeavesTheConfiguredLoginOut(t *testing.T) {
	dir := shopWorkspace(t, shopConfig+`auth:
    call: shop.catalog.v1.ProductService/GetProduct
    body:
        username: ${env.API_USER}
        password: ${env.API_PASSWORD}
    token_path: access_token
`)
	writeFile(t, filepath.Join(dir, ".shrt", "contracts", "auth.yaml"), `apiVersion: shrt/contract/v1
domain: auth
rpcs:
    shop.catalog.v1.ProductService/GetProduct:
        summary: stands in for a login
        required: [NONE]
        status: draft
`)
	writeFile(t, filepath.Join(dir, ".shrt", "contracts", "customers.yaml"), `apiVersion: shrt/contract/v1
domain: customers
rpcs:
    shop.customers.v1.CustomerService/CreateCustomer:
        summary: adds a customer
        required: [NONE]
        status: draft
`)
	defer chdir(t, dir)()
	out := captureStdout(t, func() {
		if err := contractStatus([]string{"-gaps"}); err != nil {
			t.Fatalf("status -gaps: %v", err)
		}
	})
	if strings.Contains(out, "no path to   shop.catalog.v1.ProductService/GetProduct") {
		t.Fatalf("the login the config's auth calls needs no path, so it is not a gap:\n%s", out)
	}
	if !strings.Contains(out, "no path to   shop.customers.v1.CustomerService/CreateCustomer") {
		t.Fatalf("an rpc that is not a login is still listed:\n%s", out)
	}
	if !strings.Contains(out, "login") {
		t.Fatalf("the legend must say the configured login is left out:\n%s", out)
	}
}
