package contract_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func simulatedOrderStates(steps []*chain.Step, until string) map[string]string {
	states := map[string]string{}
	for _, s := range steps {
		if s.ID == until {
			break
		}
		refused := false
		for _, e := range s.Expect {
			if e.Path == "status.code" && e.NotEqual != nil {
				refused = true
			}
		}
		if refused || s.SkipAuth || s.Auth == "invalid" {
			continue
		}
		ref := fmt.Sprint(s.Body["id_order"])
		switch {
		case strings.HasSuffix(s.Call, "/CreateOrder"):
			states["${"+s.ID+".order.id_order}"] = "ORDER_STATUS_PENDING"
		case strings.HasSuffix(s.Call, "/ConfirmOrder"):
			states[ref] = "ORDER_STATUS_CONFIRMED"
		case strings.HasSuffix(s.Call, "/CancelOrder"):
			states[ref] = "ORDER_STATUS_CANCELLED"
		}
	}
	return states
}

func TestAStatusFilteredListExpectsEachFixtureInTheStateTheChainLeftIt(t *testing.T) {
	p, text, _ := shopDemoPlan(t, "CreateOrder", "ConfirmOrder", "CancelOrder", "FetchOrder", "ListOrders")
	filtered := 0
	for _, st := range p.Chain.Steps {
		if !strings.HasPrefix(st.ID, "list_orders_") || st.Body["status"] == nil {
			continue
		}
		want := fmt.Sprint(st.Body["status"])
		states := simulatedOrderStates(p.Chain.Steps, st.ID)
		if want == "ORDER_STATUS_UNSPECIFIED" {
			for _, e := range st.Expect {
				if !strings.HasSuffix(e.Path, ".id_order") || e.Equals == nil {
					continue
				}
				statusPath := strings.TrimSuffix(e.Path, ".id_order") + ".status"
				for _, s := range st.Expect {
					if s.Path == statusPath && fmt.Sprint(s.Equals) != states[fmt.Sprint(e.Equals)] {
						t.Fatalf("%s lists %s as %v, which the chain left %s:\n%s", st.ID, e.Equals, s.Equals, states[fmt.Sprint(e.Equals)], text)
					}
				}
			}
			continue
		}
		filtered++
		for _, e := range st.Expect {
			if !strings.HasPrefix(e.Path, "orders.") || !strings.HasSuffix(e.Path, ".id_order") || e.Equals == nil {
				continue
			}
			ref := fmt.Sprint(e.Equals)
			if got := states[ref]; got != want {
				t.Fatalf("%s filters on %s but expects %s, which the chain left %s at that point:\n%s", st.ID, want, ref, got, text)
			}
		}
	}
	if filtered < 2 {
		t.Fatalf("the plan filters the list on at least two states:\n%s", text)
	}
	if _, ok := p.Chain.Step("list_orders_pending"); !ok {
		t.Fatalf("a fixture the chain did not move is left pending, so the pending filter is still probed:\n%s", text)
	}
}
