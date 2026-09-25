package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/runner"
)

func TestABuildHeaderCarryingAKnownSecretIsScrubbed(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.builds = []string{"v1 dsn=postgres://staff:s3cret-admin@db", "v1 dsn=postgres://staff:s3cret-admin@db", "v2 dsn=postgres://staff:s3cret-admin@db"}
	cfg := testConfig(srv.URL)
	cfg.Target.BuildHeader = "X-Build"
	cfg.Auth.Body = map[string]any{"username": "staff", "password": "s3cret-admin"}
	r, opts, err := runner.NewFromConfig(context.Background(), cfg, catalogtest.New())
	if err != nil {
		t.Fatalf("runner from config: %v", err)
	}

	rec, err := r.Run(context.Background(), testChain(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(rec.Build, "s3cret-admin") {
		t.Fatalf("record build = %q: the login password is stored in clear", rec.Build)
	}
	if !strings.Contains(rec.Build, "v1 dsn=postgres://staff:<redacted>@db") {
		t.Fatalf("record build = %q, want the build kept with only the secret scrubbed", rec.Build)
	}
	for _, sr := range rec.Steps {
		if strings.Contains(sr.Warning, "s3cret-admin") {
			t.Errorf("step %s warning carries the password: %q", sr.ID, sr.Warning)
		}
	}
}
