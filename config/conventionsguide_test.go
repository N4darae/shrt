package config

import (
	"strings"
	"testing"
)

const defaultGuide = `no conventions: block declared, so shrt assumes the defaults. Declare only what
differs, under a top-level conventions: key in .shrt/config.yaml; every key is optional:
    read_only_prefixes: [Fetch, Get, List, Preview, Search, Read, Query, Find, Lookup, Describe, Show, Count, Export, Download, Retrieve]
    envelope_path: error.code                     where a response states its own verdict; MOVE it, "" does not disable it
    envelope_ok: OK                               the value at that path meaning success
    item_envelope_path: results[].error.code      per-item verdict in a batch response; unset = none
    code_fields: [app_code, reason, error_code]   detail field names 'shrt chain which -code' searches
    validate_output: true                         a response the descriptor does not match FAILS the step instead of warning
`

func TestConventionsGuideDefaultIsUnchanged(t *testing.T) {
	if ConventionsGuide != defaultGuide {
		t.Fatalf("the default guide changed:\n%s", ConventionsGuide)
	}
}

func TestConventionsGuideForUsesTheGivenEnvelope(t *testing.T) {
	got := ConventionsGuideFor("status.code")
	for _, want := range []string{
		"    envelope_path: status.code                    where",
		"    item_envelope_path: results[].status.code     per-item",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "error.code") {
		t.Fatalf("the default path leaked into a guide for status.code:\n%s", got)
	}
}

func TestConventionsGuideForADetectedEnvelopeDoesNotCallItTheDefault(t *testing.T) {
	g := ConventionsGuideFor("status.code")
	if strings.Contains(g, "so shrt assumes the defaults") || !strings.Contains(g, "envelope_ok: <success value>") {
		t.Fatalf("a detected envelope is not the default, and its success value cannot be guessed:\n%s", g)
	}
}
