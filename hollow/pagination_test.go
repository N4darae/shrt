package hollow_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

const emptyWithPaging = `{"error":{"code":"OK","details":[],"message":""},"limits":[],` +
	`"pagination":{"current_page":"1","page_size":"10","total":"0","total_pages":"0"}}`

func TestAListRPCThatEchoesPaginationIsStillHollow(t *testing.T) {
	rep := scanSteps(t, step("fetch_position_limit", "/acme.mdm.limit.v1.LimitService/FetchPositionLimit",
		emptyWithPaging, envelopeOK()))
	if rep.Unallowed != 1 {
		t.Fatalf("want 1 hollow step, got %d — the response carries nothing but a pagination echo, "+
			"and a non-zero current_page must not make an empty body look full: that blindness hid "+
			"144 step-records across 9 Fetch* families in this repo's own corpus", rep.Unallowed)
	}
}

func TestAVacuousPaginationAssertionDoesNotBuyAStepOutOfTheHollowPopulation(t *testing.T) {
	vacuous := []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "pagination.current_page", Rule: "not_empty", Got: "1", Passed: true},
	}
	if rep := scanSteps(t, step("fetch_limits", "/acme.mdm.limit.v1.LimitService/FetchPositionLimit",
		emptyWithPaging, vacuous)); rep.Unallowed != 1 {
		t.Errorf("want 1 hollow step, got %d — not_empty on a page counter cannot fail, so counting it "+
			"as a data assertion lets a step assert nothing and leave the population", rep.Unallowed)
	}
}

func TestPinningAPaginationTotalIsARealAssertionAndSparesTheStep(t *testing.T) {
	pinned := []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "pagination.total", Rule: "equals", Want: "0", Got: "0", Passed: true},
	}
	if rep := scanSteps(t, step("fetch_limits", "/acme.mdm.limit.v1.LimitService/FetchPositionLimit",
		emptyWithPaging, pinned)); rep.Unallowed != 0 {
		t.Errorf("want 0 hollow steps, got %d — 'total equals 0' is an author deliberately pinning "+
			"emptiness as the expected result, which is the opposite of a read that found nothing "+
			"by accident", rep.Unallowed)
	}
}

func TestABodyWithRealRowsIsNotHollowEvenWithPagination(t *testing.T) {
	full := `{"error":{"code":"OK"},"limits":[{"id_limit":"abc"}],"pagination":{"current_page":"1","total":"1"}}`
	if rep := scanSteps(t, step("fetch_limits", "/acme.mdm.limit.v1.LimitService/FetchPositionLimit",
		full, envelopeOK())); rep.Unallowed != 0 {
		t.Errorf("want 0 hollow steps, got %d — the read returned a row, so skipping pagination when "+
			"judging emptiness must not make a full body look empty", rep.Unallowed)
	}
}
