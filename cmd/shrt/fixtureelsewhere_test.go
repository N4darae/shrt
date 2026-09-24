package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const otherUniqueChain = `apiVersion: shrt/v1
name: cli-other
vars:
    label: first
steps:
    - id: make
      call: ThingService/Create
      body:
          name: widget ${vars.label}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

func TestAValueAnotherChainCreatedIsAReusedFixture(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	writeFile(t, ".shrt/chains/cli-other.yaml", otherUniqueChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	for _, tag := range []string{"shared1", "shared2"} {
		captureStdout(t, func() {
			if err := runRun(ctx, []string{"cli-other", "-quiet", "-var", "label=" + tag}); err != nil {
				t.Fatalf("shrt run cli-other: %v", err)
			}
		})
		var err error
		out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=" + tag}) })
		var coded *exitError
		if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out, "FINDING") {
			t.Fatalf("chain cli-other created that value, so this is not a finding, exit 3: %v\n%s", err, out)
		}
		if !strings.Contains(err.Error(), "fixture reused") || !strings.Contains(err.Error(), "cli-other") {
			t.Fatalf("the verdict names the other chain whose run used the value: %v", err)
		}
	}
}
