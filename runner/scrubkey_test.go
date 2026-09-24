package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func TestATokenEchoedAsAResponseObjectKeyIsScrubbed(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.tokenKey = true
	r := scrubRunner(t, srv)
	c := normalized(t, &chain.Chain{Name: "token-key", Steps: []*chain.Step{
		{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "a"}, Expect: okExpect()},
	}})
	rec, err := r.Run(context.Background(), c, runner.Options{Redact: config.DefaultRedact()})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(srv.lastHeader("Authorization"), "Bearer ")
	if token == "" {
		t.Fatal("the fetch carried no bearer token, so the premise is missing")
	}
	if text := recordText(t, rec); strings.Contains(text, token) {
		t.Fatalf("the token %q echoed as an object key reached the run record: %s", token, text)
	}
	if got := string(rec.Steps[0].Response); !strings.Contains(got, "<redacted>") {
		t.Fatalf("the key should be replaced by the redaction marker, got %s", got)
	}
}
