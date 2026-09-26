package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChainLsMarksAChainKeptRed(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	writeFile(t, ".shrt/chains/cli-red.yaml", `apiVersion: shrt/v1
name: cli-red
description: reach Fetch, composed from the contract dependency graph
kept_red:
    - step: fetch
      path: error.code
steps:
    - id: fetch
      call: ThingService/Fetch
      body:
          id: x
      expect:
          - path: error.code
            equals: OK
`)
	var err error
	out := captureStdout(t, func() { err = chainList(nil) })
	if err != nil {
		t.Fatal(err)
	}
	var red, plain string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, " cli-red ") {
			red = line
		}
		if strings.Contains(line, " cli-thing-flow ") {
			plain = line
		}
	}
	if !strings.HasSuffix(red, " 1 step(s)") || strings.Contains(red, "reach Fetch") {
		t.Fatalf("a line ends at the step count; -long prints the description:\n%s", out)
	}
	if !strings.HasPrefix(red, " R ") || strings.HasPrefix(plain, " R ") {
		t.Fatalf("a chain kept red is marked R, one that is not is not:\n%s", out)
	}
	if !strings.Contains(out, "R = kept red") {
		t.Fatalf("the legend explains the mark:\n%s", out)
	}
	out = captureStdout(t, func() { err = chainList([]string{"-json"}) })
	var rows []map[string]any
	if json.Unmarshal([]byte(out), &rows) != nil {
		t.Fatalf("bad json: %s", out)
	}
	for _, r := range rows {
		if r["name"] == "cli-red" && r["kept_red"] != true {
			t.Fatalf("the JSON row says it is kept red: %v", r)
		}
	}
}
