package main

import (
	"context"
	"strings"
	"testing"
)

const freshTagChain = `apiVersion: shrt/v1
name: cli-fresh-flow
vars:
    tag: T1
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget-${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
    - id: by_tag
      call: ThingService/Fetch
      body:
          id: t-${vars.tag}
      expect:
          - path: error.code
            equals: OK
`

func freshTagWorkspace(t *testing.T) {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-fresh-flow.yaml", freshTagChain)
	if err := runRun(context.Background(), []string{"cli-fresh-flow", "-quiet", "-var", "tag=T9"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
}

func freshTagSlice(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), append([]string{"cli-fresh-flow"}, args...))
	})
	return out, err
}

func TestCLISliceVerifyRefusesTheDeclaredDefaultOfAVarAKeptWriteInterpolates(t *testing.T) {
	freshTagWorkspace(t)
	_, err := freshTagSlice(t, "-step", "fetch", "-run", "latest", "-verify")
	if err == nil || !strings.Contains(err.Error(), "-var tag=<fresh>") {
		t.Fatalf("create re-sends widget-${vars.tag}; without -var the slice would send the declared default T1, which a second verify collides with: want a refusal asking for -var tag=<fresh>, got %v", err)
	}
	if _, err := freshTagSlice(t, "-step", "fetch", "-run", "latest", "-verify", "-var", "tag=T2"); err != nil {
		t.Fatalf("a fresh -var tag must let -verify run: %v", err)
	}
}

func TestCLISlicePinVerifyRefusesTheRunsValueOfAVarAKeptWriteInterpolates(t *testing.T) {
	freshTagWorkspace(t)
	_, err := freshTagSlice(t, "-step", "fetch", "-mode", "pin", "-keep", "create", "-run", "latest", "-verify")
	if err == nil || !strings.Contains(err.Error(), "-var tag=<fresh>") {
		t.Fatalf("pin mode keeps create, which re-sends widget-${vars.tag}: want a refusal asking for -var tag=<fresh>, got %v", err)
	}
}

func TestCLISlicePinWritesTheRunsValueOfAVarOnlyAReadUses(t *testing.T) {
	freshTagWorkspace(t)
	out, err := freshTagSlice(t, "-step", "by_tag", "-mode", "pin", "-run", "latest")
	if err != nil {
		t.Fatalf("slice: %v", err)
	}
	yamlPart := out[strings.Index(out, "apiVersion:"):]
	if !strings.Contains(yamlPart, "tag: T9") || strings.Contains(yamlPart, "tag: T1") {
		t.Fatalf("pin mode reproduces run T9; no kept write re-sends tag, so the slice must carry the run's value, not the default:\n%s", yamlPart)
	}
	if _, err := freshTagSlice(t, "-step", "by_tag", "-mode", "pin", "-run", "latest", "-verify"); err != nil && exitCodeOf(err) != 3 {
		t.Fatalf("no kept write interpolates tag, so pin -verify needs no fresh value: %v", err)
	}
}

func TestCLISlicePinNextCommandAsksForAFreshVarTheKeptWritesWillInterpolate(t *testing.T) {
	freshTagWorkspace(t)
	out, _ := freshTagSlice(t, "-step", "fetch", "-mode", "pin", "-run", "latest", "-verify")
	next := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "next: ") {
			next = l
		}
	}
	if !strings.Contains(next, "-keep writes") {
		t.Fatalf("pin mode dropped the write create, so verify must end with a -keep command:\n%s", out)
	}
	if !strings.Contains(next, "-var tag=<fresh>") {
		t.Fatalf("the next: slice keeps create, which re-sends widget-${vars.tag}, so it must ask for a fresh tag: %s", next)
	}
}
