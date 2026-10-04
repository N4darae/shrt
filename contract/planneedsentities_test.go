package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func stockedBefore(p *contract.Plan, upto int, product string) bool {
	for _, s := range p.Chain.Steps[:upto] {
		if !strings.HasSuffix(s.Call, "/AddStock") || isRefusal(s) {
			continue
		}
		if v, ok := s.Body["id_product"].(string); ok && v == "${"+product+".product.id_product}" {
			return true
		}
	}
	return false
}

func isRefusal(s *chain.Step) bool {
	for _, e := range s.Expect {
		if e.Path == "transport.code" || (e.Path == "status.code" && e.NotEqual != nil) {
			return true
		}
		if e.Path == "status.code" && e.Equals != nil && e.Equals != "SUCCESS" {
			return true
		}
	}
	return false
}

func TestPlanStocksEveryProductOfAnOrderBeforeAnyConfirmWhateverTheTargetOrder(t *testing.T) {
	for _, targets := range [][]string{
		{"ListOrders", "AddStock", "ConfirmOrder", "CancelOrder"},
		{"ListOrders", "AddStock"},
		{"ConfirmOrder"},
	} {
		p, text := confirmNeedsStockPlan(t, contract.PlanOptions{}, targets...)
		for i, st := range p.Chain.Steps {
			if !strings.HasSuffix(st.Call, "/ConfirmOrder") {
				continue
			}
			ref, _ := st.Body["id_order"].(string)
			order := strings.TrimSuffix(strings.TrimPrefix(ref, "${"), ".order.id_order}")
			creator, ok := p.Chain.Step(order)
			if !ok {
				continue
			}
			lines, _ := creator.Body["lines"].([]any)
			for _, l := range lines {
				item, _ := l.(map[string]any)
				prod, _ := item["id_product"].(string)
				prod = strings.TrimSuffix(strings.TrimPrefix(prod, "${"), ".product.id_product}")
				if !stockedBefore(p, i, prod) {
					t.Fatalf("%v: step %s confirms %s, whose line reads %s, but no AddStock for %s runs before it:\n%s",
						targets, st.ID, order, prod, prod, text)
				}
			}
		}
	}
}
