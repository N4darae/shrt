package doctor_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/doctor"
)

func conventionsReport(t *testing.T, cfg *config.Config, cat *catalog.Catalog) []doctor.Finding {
	t.Helper()
	if cfg.Root == "" {
		cfg.Root = t.TempDir()
	}
	rep := doctor.Run(context.Background(), cfg, doctor.Options{
		Catalog: func(*config.Config) (*catalog.Catalog, error) { return cat, nil },
	})
	out := []doctor.Finding{}
	for _, f := range rep.Findings {
		if f.Check == doctor.CheckConventions {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		t.Fatal("doctor reported nothing about conventions at all")
	}
	return out
}

func worstOf(findings []doctor.Finding) doctor.Level {
	worst := doctor.LevelOK
	for _, f := range findings {
		if f.Level > worst {
			worst = f.Level
		}
	}
	return worst
}

func TestDoctorReportsAnEnvelopePathNoResponseCarries(t *testing.T) {
	cfg := config.Default()
	cfg.Conventions.EnvelopePath = "status.verdict"
	got := conventionsReport(t, cfg, catalogtest.New())
	if worstOf(got) != doctor.LevelError {
		t.Fatalf("a path no response message declares makes every envelope assertion compare "+
			"against something that is never present — the whole corpus goes red at once and "+
			"nothing says why: %v", got)
	}
}

func TestDoctorReportsAnEnvelopeNobodyConfigured(t *testing.T) {
	cfg := config.Default()
	got := conventionsReport(t, cfg, catalogtest.Foreign())
	found := ""
	for _, f := range got {
		if f.Level == doctor.LevelWarn && strings.Contains(f.Detail, "conventions.envelope_path") {
			found = f.Detail
		}
	}
	if found == "" {
		t.Fatalf("every response here answers at status.code and none carries the default "+
			"error.code, so the whole corpus would assert a path that is never present. init "+
			"prints the block once and cannot write it (it can detect the path but not the "+
			"success value); nothing re-checked that anyone pasted it: %v", got)
	}
	if !strings.Contains(found, "status.code") {
		t.Errorf("the finding should name the path it detected: %q", found)
	}
}

func TestDoctorReportsAPerItemVerdictNobodyConfigured(t *testing.T) {
	cfg := config.Default()
	cfg.Conventions.ItemEnvelopePath = ""
	got := conventionsReport(t, cfg, catalogtest.Batch())
	found := ""
	for _, f := range got {
		if strings.Contains(f.Detail, "per-item verdict") {
			found = f.Detail
		}
	}
	if found == "" {
		t.Fatal("a batch rpc can answer OK at the top level while refusing every line. A wrong " +
			"envelope_path fails loudly because every assertion misses; a missing item_envelope_path " +
			"fails silently, so it is the one doctor has to raise")
	}
	if !strings.Contains(found, "results[].error.code") {
		t.Errorf("the finding should name the path it detected: %q", found)
	}
}

func TestDoctorAcceptsAConfiguredItemEnvelope(t *testing.T) {
	cfg := config.Default()
	cfg.Conventions.ItemEnvelopePath = "results[].error.code"
	for _, f := range conventionsReport(t, cfg, catalogtest.Batch()) {
		if f.Level != doctor.LevelOK {
			t.Fatalf("this path IS declared by the fixture and must pass: %v", f)
		}
	}
}

func itemEnvelopeFinding(t *testing.T, got []doctor.Finding) doctor.Finding {
	t.Helper()
	for _, f := range got {
		if strings.Contains(f.Detail, "per-item verdict") {
			return f
		}
	}
	t.Fatalf("no per-item verdict finding: %v", got)
	return doctor.Finding{}
}

func TestDoctorSuggestsTheEnvelopeRepeatedPerItemNotAFieldNamedLikeAStatus(t *testing.T) {
	cfg := config.Default()
	cfg.Conventions.EnvelopePath = "result.code"
	f := itemEnvelopeFinding(t, conventionsReport(t, cfg, catalogtest.Listing()))
	text := f.Detail + "\n" + f.Remedy
	for _, wrong := range []string{"widgets[].status", "gadgets[].status"} {
		if strings.Contains(text, wrong) {
			t.Errorf("%s is a business field of a list read that happens to be named status; "+
				"configuring it turns every list step red while doctor says ok: %q", wrong, text)
		}
	}
	if !strings.Contains(text, "item_envelope_path: entries[].result.code") {
		t.Errorf("the batch response repeats the envelope message per entry, at "+
			"entries[].result.code, and that is the suggestion: %q", text)
	}
	if !strings.Contains(text, "guess") || !strings.Contains(text, "verify") {
		t.Errorf("the suggestion is read from field names, so it must say it is a guess to verify: %q", text)
	}
}

func TestDoctorSuggestsNothingPerItemWhenOnlyStatusNamedFieldsExist(t *testing.T) {
	cfg := config.Default()
	cfg.Conventions.EnvelopePath = "state.code"
	for _, f := range conventionsReport(t, cfg, catalogtest.Listing()) {
		if strings.Contains(f.Detail, "per-item verdict") {
			t.Fatalf("no list element carries state.code, so there is nothing to suggest: %v", f)
		}
	}
}
