package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newMintingBackend(t *testing.T, prefix string, refuseAll bool) *httptest.Server {
	t.Helper()
	minted := map[string]bool{}
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			id := prefix + "-" + itoa(next)
			minted[id] = true
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": id})
		case "/shrt.test.v1.ThingService/Fetch":
			id, _ := body["id"].(string)
			if !minted[id] || refuseAll {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "REJECTED", "message": "no thing " + id}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": id})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func chdirToForeignRunWorkspace(t *testing.T, refuseAll bool) (string, string) {
	t.Helper()
	first := newMintingBackend(t, "old", false)
	chdirToFreshCLIWorkspace(t, first.URL)
	writeFile(t, ".shrt/chains/cli-pair.yaml", `apiVersion: shrt/v1
name: cli-pair
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
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
`)
	captureStdout(t, func() {
		if err := runRun(context.Background(), []string{"cli-pair", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
	second := newMintingBackend(t, "new", refuseAll)
	cfg := string(mustRead(t, ".shrt/config.yaml"))
	writeFile(t, ".shrt/config.yaml", strings.Replace(cfg, first.URL, second.URL, 1))
	return first.URL, second.URL
}

func TestCLISlicePinRefusesASourceRunFromAnotherTarget(t *testing.T) {
	was, now := chdirToForeignRunWorkspace(t, false)
	want := "the source run was recorded against " + was + ", this target is " + now
	for _, verify := range []bool{false, true} {
		args := []string{"cli-pair", "-step", "fetch", "-mode", "pin", "-run", "latest"}
		if verify {
			args = append(args, "-verify")
		}
		var err error
		out := captureStdout(t, func() { err = chainSlice(context.Background(), args) })
		if err == nil {
			t.Fatalf("verify=%v: pinning an id minted on another target must be refused:\n%s", verify, out)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("verify=%v: the refusal must name both targets, got %v", verify, err)
		}
		if verify && exitCodeOf(err) != 3 {
			t.Fatalf("under -verify a foreign source run is inconclusive (exit 3), got %d: %v", exitCodeOf(err), err)
		}
		if strings.Contains(out, "NOT REPRODUCED") {
			t.Fatalf("nothing was compared, so there is no NOT REPRODUCED:\n%s", out)
		}
	}
}

func TestCLISliceClosureVerifyAgainstAForeignRunIsInconclusive(t *testing.T) {
	was, now := chdirToForeignRunWorkspace(t, true)
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{"cli-pair", "-step", "fetch", "-run", "latest", "-verify"})
	})
	if !strings.Contains(out, "the source run was recorded against "+was+", this target is "+now) {
		t.Fatalf("a verdict compared across targets must name both:\n%s\n%v", out, err)
	}
	if exitCodeOf(err) != 3 || strings.Contains(out, "NOT REPRODUCED") {
		t.Fatalf("a different verdict on another target is inconclusive, not NOT REPRODUCED (exit %d):\n%s", exitCodeOf(err), out)
	}
}

func TestCLIChainWhichDoesNotOfferARunFromAnotherTarget(t *testing.T) {
	chdirToForeignRunWorkspace(t, false)
	var err error
	out := captureStdout(t, func() { err = chainWhich([]string{"-rpc", "ThingService/Fetch"}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "-mode pin -run") {
		t.Fatalf("the only run was recorded against another target, so which must not offer to pin from it:\n%s", out)
	}
}
