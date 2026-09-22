package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestIsCodePathFollowsTheConfiguredEnvelopeAndCodeFields(t *testing.T) {
	t.Cleanup(func() {
		chain.ApplyConventions(nil, "", "")
		chain.ApplyCodeFields(nil)
	})

	chain.ApplyConventions(nil, "", "")
	chain.ApplyCodeFields(nil)
	for _, p := range []string{"error.code", "error.details.0.app_code", "error.details.0.reason", "error.details.0.error_code"} {
		if !chain.IsCodePath(p) {
			t.Errorf("default conventions should recognise %q as a verdict path", p)
		}
	}
	if chain.IsCodePath("status.code") {
		t.Error("with envelope_path error.code, status.code is an ordinary field")
	}

	chain.ApplyConventions(nil, "status.code", "SUCCESS")
	if !chain.IsCodePath("status.code") {
		t.Error("a deployment that moved its envelope to status.code gets no answer from " +
			"'chain which -code', because the envelope leaf was compared against a literal error.code")
	}
	if chain.IsCodePath("error.code") {
		t.Error("error.code is not this deployment's envelope")
	}

	chain.ApplyCodeFields([]string{"failure_id"})
	if !chain.IsCodePath("status.details.0.failure_id") {
		t.Error("conventions.code_fields names the detail fields that carry a backend's own code")
	}
	if chain.IsCodePath("status.details.0.app_code") {
		t.Error("an explicit code_fields list replaces the defaults, it does not extend them")
	}
}
