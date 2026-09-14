package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestEntityIDDetectionIsNotTiedToOneRepoSNamingConvention(t *testing.T) {
	yes := []string{
		"id", "uuid",
		"id_customer", "id_deal", "id_customers",
		"customer_id", "order_id",
		"customerId", "orderID",
		"customer_uuid",
		"customer_ids", "orderIDs", "customerIDs",
		"body.id_invoice", "request.order_id",
	}
	no := []string{
		"identity", "idempotency_key", "valid", "paid",
		"amount_minor", "currency", "name", "email",
		"idle_seconds", "invalid",
		"request_id", "correlation_id", "trace_id", "span_id", "session_id", "idempotency_id",
		"id_token", "id_number", "id_type", "id_card_number", "id_document",
	}
	for _, n := range yes {
		if !contract.IsEntityIDField(n) {
			t.Errorf("IsEntityIDField(%q) = false: a repo naming its ids this way gets no producer edge "+
				"scaffolded and no unwired-id quality term", n)
		}
	}
	for _, n := range no {
		if contract.IsEntityIDField(n) {
			t.Errorf("IsEntityIDField(%q) = true: it would be wired to an unrelated producer, and charged "+
				"2 quality points for not being wired to one", n)
		}
	}
}

func TestPagingAndVerdictFieldsAreNotWhatASuccessStepShouldAssert(t *testing.T) {
	for _, n := range []string{"next_page_token", "page_token", "next_cursor", "cursor", "pagination"} {
		if !contract.IsPagingFieldName(n) {
			t.Errorf("IsPagingFieldName(%q) = false: asserting not_empty on it fails on the last page, "+
				"which is a correct response", n)
		}
	}
	for _, n := range []string{"total_count", "id_deal", "status", "balance"} {
		if contract.IsPagingFieldName(n) {
			t.Errorf("IsPagingFieldName(%q) = true: it would be skipped as pagination when it carries the payload", n)
		}
	}
	if !contract.IsVerdictFieldName("error") {
		t.Error("a field literally named error is never the success payload, whatever the envelope is set to")
	}
}
