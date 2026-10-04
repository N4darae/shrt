package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/contract"
)

const unauthBlock = `failures:
    - connect_code: unauthenticated
      reason: Unauthenticated
      when: the Authorization header is missing, or its token is unknown or expired
`

func sharedUnauthLibrary(t *testing.T) (*catalog.Catalog, *contract.Library) {
	t.Helper()
	return shopDemoEdited(t, func(name, body string) string {
		switch name {
		case "auth.yaml":
			return strings.Replace(body, "rpcs:\n", strings.Replace(unauthBlock, "      when:", "      scope: all\n      when:", 1)+"rpcs:\n", 1)
		case "catalog.yaml", "orders.yaml", "customers.yaml":
			return strings.Replace(body, unauthBlock, "", 1)
		}
		return body
	})
}

func TestADomainFailureWithScopeAllReachesEveryDomain(t *testing.T) {
	cat, lib := sharedUnauthLibrary(t)
	for _, rpc := range []string{"shop.catalog.v1.StockService/AddStock", "shop.orders.v1.OrderService/ListOrders"} {
		found := false
		for _, f := range lib.AllFailures(rpc) {
			found = found || f.ConnectCode == "unauthenticated"
		}
		if !found {
			t.Fatalf("%s inherits the auth domain's scope: all failure, got %+v", rpc, lib.AllFailures(rpc))
		}
	}
	p, err := contract.BuildPlanWith([]string{"AddStock"}, lib, cat, "shared", contract.PlanOptions{Auth: true})
	if err != nil {
		t.Fatal(err)
	}
	notes := strings.Join(p.Notes, "\n")
	if strings.Contains(notes, "which no failure in its contract declares") {
		t.Fatalf("the shared failure is declared for AddStock:\n%s", notes)
	}
	for _, issue := range contract.LintAll(lib, cat, nil) {
		if issue.Severity == "error" {
			t.Fatalf("scope: all on a domain failure lints clean, got %+v", issue)
		}
	}
}
