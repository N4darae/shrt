package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/contract"
)

func TestDomainOf(t *testing.T) {
	cases := []struct {
		name    string
		service string
		want    string
	}{
		{"four segment package keeps the segment after the org root", "acme.dealing.deal.v1.DealService", "dealing"},
		{"five segment package still keeps the segment after the org root", "acme.admin.pricing.assetmark.v1.AssetMarkActionService", "admin"},
		{"another org-root domain in the same tree", "acme.pricing.dailybook.v1.DailyBookSummaryActionService", "pricing"},
		{"two segment package is its own domain", "inv.v1.InventoryService", "inv"},
		{"two segment package, action service", "inv.v1.InventoryActionService", "inv"},
		{"single segment package with no version", "inv.InventoryService", "inv"},
		{"v2beta1 suffix is a version, not a domain", "inv.v2beta1.InventoryService", "inv"},
		{"v2beta1 suffix under an org root", "acme.billing.invoice.v2beta1.InvoiceService", "billing"},
		{"v1alpha suffix is a version", "acme.billing.v1alpha.InvoiceService", "billing"},
		{"a segment that merely starts with v is not a version", "acme.vault.v1.VaultService", "vault"},
		{"reverse-DNS root is dropped, not treated as the org root", "com.acme.billing.v1.InvoiceService", "billing"},
		{"reverse-DNS root with a deeper tree", "io.acme.admin.pricing.v1.RateService", "admin"},
		{"a two-segment reverse-DNS package is not deep enough to drop the root", "io.fleet.v1.FleetService", "fleet"},
		{"a real org named like a TLD is kept when nothing follows it", "app.v1.AppService", "app"},
		{"no package at all falls back to the service name", "InventoryService", "InventoryService"},
		{"empty service name", "", "default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := contract.DomainOf(&catalog.Method{Service: tc.service})
			if got != tc.want {
				t.Fatalf("DomainOf(%q) = %q, want %q", tc.service, got, tc.want)
			}
		})
	}
}

func TestDomainOfKeepsTheNineCuratedDomains(t *testing.T) {
	want := map[string]string{
		"acme.admin.iam.partnerlogin.v1.PartnerLoginActionService":             "admin",
		"acme.admin.mdm.instrumentoperator.v1.InstrumentOperatorActionService": "admin",
		"acme.audit.operationlineage.v1.OperationLineageService":               "audit",
		"acme.dealing.dealsurcharge.v1.DealSurchargeService":                   "dealing",
		"acme.iam.staffauth.v1.StaffAuthService":                               "iam",
		"acme.mdm.feeschedule.v1.FeeScheduleService":                           "mdm",
		"acme.partner.partnerauth.v1.PartnerAuthService":                       "partner",
		"acme.position.counterpartystatement.v1.CounterpartyStatementService":  "position",
		"acme.pricing.referencerate.v1.ReferenceRateActionService":             "pricing",
		"acme.settlement.settlementsheet.v1.SettlementSheetService":            "settlement",
	}
	for service, domain := range want {
		if got := contract.DomainOf(&catalog.Method{Service: service}); got != domain {
			t.Errorf("DomainOf(%q) = %q, want %q — the curated overlay files would move", service, got, domain)
		}
	}
}
