package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryCommandThatReadsContractsRefusesAnOverlayThatDoesNotParse(t *testing.T) {
	defer shopStatusWorkspace(t)()
	writeFile(t, filepath.Join(".shrt", "contracts", "customers.yaml"), "apiVersion: shrt/contract/v1\ndomain: customers\nrpcs:\n  bad: [\n")
	writeFile(t, filepath.Join(".shrt", "chains", "orders.yaml"), `apiVersion: shrt/v1
name: orders
steps:
    - id: create
      call: shop.orders.v1.OrderService/CreateOrder
      body: {}
      expect:
          - path: status.code
            equals: SUCCESS
`)
	commands := map[string]func() error{
		"chain lint -strict":           func() error { return chainLint([]string{"-strict"}) },
		"contract plan":                func() error { return contractPlan([]string{"ConfirmOrder"}) },
		"contract lint -domain orders": func() error { return contractLint([]string{"-domain", "orders"}) },
		"contract lint orders -json":   func() error { return contractLint([]string{"-json", "orders"}) },
		"contract show":                func() error { return contractShow([]string{"ConfirmOrder"}) },
		"contract status":              func() error { return contractStatus(nil) },
		"contract quality":             func() error { return contractQuality(nil) },
		"contract init":                func() error { return contractInit([]string{"orders", "-stdout"}) },
		"chain new":                    func() error { return chainNew([]string{"-name", "x", "ConfirmOrder"}) },
		"chain which":                  func() error { return chainWhich([]string{"-rpc", "CreateOrder"}) },
		"chain slice":                  func() error { return chainSlice(t.Context(), []string{"orders", "-step", "create"}) },
	}
	for name, fn := range commands {
		var err error
		out := captureStdout(t, func() { err = fn() })
		if err == nil {
			t.Errorf("%s: an overlay that does not parse must fail the command, got exit 0:\n%s", name, out)
			continue
		}
		if !strings.Contains(err.Error()+out, "customers.yaml") || !strings.Contains(err.Error()+out, "yaml: line") {
			t.Errorf("%s: the refusal must name the file and its parse error, got %v\n%s", name, err, out)
		}
	}
}
