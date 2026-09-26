package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/runner"
)

func TestRunRefusesUpFrontAnEnvelopePathNoResponseDeclares(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:1")
	cfg.Conventions.EnvelopePath = "status.cod"
	_, _, err := runner.NewFromConfig(context.Background(), cfg, catalogtest.New())
	if err == nil || !strings.Contains(err.Error(), "conventions.envelope_path") || !strings.Contains(err.Error(), `"error.code"`) {
		t.Fatalf("a bad envelope_path must refuse the run and point at what the responses carry, got %v", err)
	}
	cfg.Conventions.EnvelopePath = "error.code"
	if _, _, err := runner.NewFromConfig(context.Background(), cfg, catalogtest.New()); err != nil {
		t.Fatalf("a declared envelope_path is accepted, got %v", err)
	}
}
