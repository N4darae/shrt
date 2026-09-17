package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func TestEnvelopeSuggestionParsesWhenPastedIntoAConfig(t *testing.T) {
	dir := t.TempDir()
	shrtDir := filepath.Join(dir, ".shrt")
	if err := os.MkdirAll(shrtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := "target:\n    base_url: http://127.0.0.1:8080\ndescriptor:\n    file: .shrt/descriptor.binpb\n"
	body := base + EnvelopeSuggestion("status.code")
	path := filepath.Join(shrtDir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("the conventions: block shrt init tells an adopter to paste does not parse when pasted "+
			"verbatim into .shrt/config.yaml, so the first thing an adopter does with it breaks their "+
			"config: %v\n---\n%s", err, body)
	}
	if cfg.Conventions.EnvelopePath != "status.code" {
		t.Fatalf("envelope_path = %q, want status.code — the pasted block parsed but landed somewhere "+
			"the loader does not read, so the adopter gets no error and no effect", cfg.Conventions.EnvelopePath)
	}
}

func TestEnvelopeSuggestionIsNotIndented(t *testing.T) {
	got := EnvelopeSuggestion("status.code")
	first, _, _ := strings.Cut(got, "\n")
	if strings.HasPrefix(first, " ") || strings.HasPrefix(first, "\t") {
		t.Fatalf("the suggested block starts with whitespace (%q), so pasting it at the top level of a "+
			"config.yaml is a yaml error rather than a setting", first)
	}
}
