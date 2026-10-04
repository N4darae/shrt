package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func shopDemoMutated(t *testing.T, opts contract.PlanOptions, mutate func(rpcs map[string]*contract.RPCContract), targets ...string) (*contract.Plan, string) {
	t.Helper()
	cat, lib := shopDemo(t)
	for _, o := range lib.Overlays {
		mutate(o.RPCs)
	}
	p, text, _ := planFrom(t, cat, contract.NewLibrary(lib.Overlays), opts, targets...)
	return p, text
}

func failureWhen(rpc, reason, when string) func(map[string]*contract.RPCContract) {
	return func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs[rpc]; c != nil {
			for i := range c.Failures {
				if reason == "" || c.Failures[i].Reason == reason {
					c.Failures[i].When = when
				}
			}
		}
	}
}

func TestARoleProbeExpectsThePermissionFailureNotAShapeErrorWhoseWhenMentionsTheRole(t *testing.T) {
	p, text := shopDemoMutated(t, contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}}, failureWhen("shop.catalog.v1.ProductService/CreateProduct",
		"SkuRequired", "sku is empty or only whitespace; checked before the role and the price"), "CreateProduct")
	wantExpect(t, planStep(t, p, "create_product_as_clerk"), "status.details.0.reason", "PermissionDenied")
	if strings.Contains(text, "refused with SkuRequired") {
		t.Fatalf("a when: that mentions the role in passing does not make a shape error the denial:\n%s", text)
	}
}

func TestAnIdempotencyReplayIsNotExpectedToFailWithAnUnrelatedFailureMentioningTheKey(t *testing.T) {
	p, text := shopDemoMutated(t, contract.PlanOptions{}, failureWhen("shop.orders.v1.OrderService/CreateOrder",
		"CustomerNotFound", "no customer has id_customer, and the idempotency key was not seen before"), "CreateOrder")
	wantExpect(t, planStep(t, p, "create_order_replay_other_body"), "order.id_order", "${create_order.order.id_order}")
	if strings.Contains(text, "is refused with 1301") {
		t.Fatalf("CustomerNotFound is no key conflict:\n%s", text)
	}
}

func TestAStateTransitionWhosePrerequisiteIsNotInThePlanIsLeftOutWithANote(t *testing.T) {
	p, text := shopDemoMutated(t, contract.PlanOptions{}, confirmNeedsBatch, "ListOrders")
	for _, st := range p.Chain.Steps {
		if strings.HasPrefix(st.ID, "confirm_order") {
			t.Fatalf("no stock was added, so confirming a fixture would fail for a reason the plan made:\n%s", text)
		}
	}
	planStep(t, p, "list_orders_cancelled")
	if !strings.Contains(strings.Join(p.Notes, "\n"), "needs AddStockBatch") {
		t.Fatalf("the plan says why no fixture is confirmed: %v", p.Notes)
	}
}

func TestAPrefixExclusionAnchoredOnTheFirstProducerKeepsReadingIt(t *testing.T) {
	p, text := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs["shop.catalog.v1.ProductService/ListProducts"]; c != nil {
			c.Fields["sku_prefix"] = &contract.FieldContract{SameAs: "shop.catalog.v1.ProductService/CreateProduct->sku", Note: "filter by prefix"}
		}
	}, "ListProducts")
	prefix := bodyAt(t, planStep(t, p, "list_products"), "sku_prefix")
	if got := bodyAt(t, planStep(t, p, "create_product_prefix_inside"), "sku"); got != "x-"+prefix || strings.Contains(got, "create_product_prefix_inside") {
		t.Fatalf("the inside fixture carries the list's prefix after x-, not a read of its own sku, got %s:\n%s", got, text)
	}
}
