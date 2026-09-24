package runner_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func echoedHeaderRecord(t *testing.T, header, template string, vars map[string]any) (string, *runner.Record) {
	t.Helper()
	srv := newFakeServer()
	defer srv.Close()
	srv.echoHeader = header
	c := testChain()
	c.Vars = vars
	c.Steps[0].Headers = map[string]string{header: template}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw), rec
}

func TestACredentialHeaderValueEchoedByTheBackendIsScrubbedByValue(t *testing.T) {
	t.Setenv("SHRT_TEST_PARTNER_API_KEY", "pk-live-9f8e7d6c5b4a")
	raw, rec := echoedHeaderRecord(t, "X-Api-Key", "${env.SHRT_TEST_PARTNER_API_KEY}", nil)
	if strings.Contains(raw, "9f8e7d6c5b4a") {
		t.Fatalf("the api key sent in a credential header came back in the error and stayed in clear: %s", raw)
	}
	if !strings.Contains(rec.Failure, "api key <redacted> is not allowed") {
		t.Fatalf("the rest of the refusal should stay readable: %s", rec.Failure)
	}
}

func TestACredentialEnvReadIntoAPlainHeaderIsScrubbedByValue(t *testing.T) {
	t.Setenv("SHRT_TEST_SHOP_TOKEN", "tok-abcdef123456")
	raw, _ := echoedHeaderRecord(t, "X-Tag", "t-${env.SHRT_TEST_SHOP_TOKEN}", nil)
	if strings.Contains(raw, "abcdef123456") {
		t.Fatalf("an env var named like a credential is a secret wherever it is sent: %s", raw)
	}
}
