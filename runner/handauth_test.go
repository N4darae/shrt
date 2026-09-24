package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAHandWrittenAuthorizationHeaderIsRefusedBeforeAnythingIsSent(t *testing.T) {
	for name, step := range map[string]*chain.Step{
		"covered by a profile": {ID: "create", Call: "ThingService/Create",
			Headers: map[string]string{"authorization": "Bearer clerk-token"},
			Body:    map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
		"with skip_auth": {ID: "create", Call: "ThingService/Create", SkipAuth: true,
			Headers: map[string]string{"Authorization": "Bearer clerk-token"},
			Body:    map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
	} {
		srv := newFakeServer()
		deps, err := runner.Build(context.Background(), testConfig(srv.URL), catalogtest.New(), nil)
		if err != nil {
			t.Fatal(err)
		}
		r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, Auth: deps.Bindings, AuthRoute: deps.Route}
		c := normalized(t, &chain.Chain{Name: "hand-auth", Steps: []*chain.Step{
			{ID: "first", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: okExpect()},
			step,
		}})
		_, err = r.Run(context.Background(), c, runner.Options{})
		srv.mu.Lock()
		sent := len(srv.calls)
		srv.mu.Unlock()
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), "nothing was sent") || !strings.Contains(err.Error(), "uthorization") {
			t.Errorf("%s: lint rejects a hand-written Authorization header, so run must refuse it too, got %v", name, err)
		}
		if sent != 0 {
			t.Errorf("%s: %d request(s) reached the backend before the refusal", name, sent)
		}
	}
}
