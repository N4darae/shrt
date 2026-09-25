package main

import (
	"context"
	"strings"
	"testing"
)

func TestChainWhichFindsTransportCodesAndHTTPStatuses(t *testing.T) {
	srv := newEchoNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	ids, _ := e.store.ListRuns("cli-thing-flow")
	base, err := e.store.LoadRun("cli-thing-flow", ids[len(ids)-1])
	if err != nil {
		t.Fatal(err)
	}
	saveRefusedAtFetch(t, e, base, "20990101T000000Z-refused1")

	out := captureStdout(t, func() { err = chainWhich([]string{"-code", "unauthenticated"}) })
	if err != nil || !strings.Contains(out, "cli-thing-flow") || !strings.Contains(out, "fetch") || strings.Contains(out, "no local run record observed it") {
		t.Fatalf("a run record holds a 401 unauthenticated at fetch, so chain which -code unauthenticated cites it: %v\n%s", err, out)
	}

	writeFile(t, ".shrt/chains/cli-noauth.yaml", `apiVersion: shrt/v1
name: cli-noauth
steps:
    - id: fetch
      call: ThingService/Fetch
      skip_auth: true
      body:
          id: x
      expect:
          - path: transport.http_status
            equals: 401
`)
	out = captureStdout(t, func() { err = chainWhich([]string{"-code", "401"}) })
	if err != nil || !strings.Contains(out, "cli-noauth") {
		t.Fatalf("cli-noauth asserts transport.http_status equals 401, so chain which -code 401 lists it: %v\n%s", err, out)
	}
}
