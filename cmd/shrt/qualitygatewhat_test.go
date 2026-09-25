package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestAWorseQualityScoreNamesTheGapsNotVaguerness(t *testing.T) {
	baseline := filepath.Join(t.TempDir(), "quality-baseline")
	if err := os.WriteFile(baseline, []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := contract.QualityReport{TotalScore: 2, RPCs: []contract.QualityRPC{
		{Domain: "orders", RPC: "shop.orders.v1.OrderService/CreateOrder", UndocumentedFields: []string{"note"}, Score: 2},
	}}
	err := qualityGate(report, baseline)
	if err == nil {
		t.Fatal("a score above the baseline fails the gate")
	}
	if strings.Contains(err.Error(), "see what got vaguer") {
		t.Errorf("a field the descriptor gained is not a contract that got vaguer: %v", err)
	}
	for _, want := range []string{"CreateOrder", "undocumented field(s): note", "descriptor gained"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the gate lacks %q: %v", want, err)
		}
	}
}
