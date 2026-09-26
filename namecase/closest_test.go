package namecase_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/namecase"
)

func TestClosestSuggestsTheNearestNamesFirstAndNothingFarAway(t *testing.T) {
	got := namecase.Closest("create_custmer", []string{"create_customer", "list_orders", "createCustomers"}, 3)
	if strings.Join(got, ",") != "create_customer,createCustomers" {
		t.Fatalf("want the near names nearest first, got %v", got)
	}
	if got := namecase.Closest("zzz", []string{"create_customer"}, 3); len(got) != 0 {
		t.Fatalf("a far name is not a suggestion, got %v", got)
	}
}
