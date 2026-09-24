package main

import (
	"context"
	"strings"
	"testing"
)

func TestProgressColumnsAlignForALongStepID(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	long := "create_the_first_widget_of_many"
	writeFile(t, ".shrt/chains/cli-long-ids.yaml", `apiVersion: shrt/v1
name: cli-long-ids
steps:
    - id: `+long+`
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${`+long+`.id}
      expect:
          - path: error.code
            equals: OK
`)
	out := captureStdout(t, func() { _ = runRun(context.Background(), []string{"cli-long-ids"}) })
	cols := []int{}
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "ThingService/"); i >= 0 && (strings.Contains(line, long) || strings.Contains(line, " fetch ")) {
			cols = append(cols, i)
		}
	}
	if len(cols) != 2 || cols[0] != cols[1] {
		t.Errorf("the call column starts at the same place on every progress line, got %v:\n%s", cols, out)
	}
}
