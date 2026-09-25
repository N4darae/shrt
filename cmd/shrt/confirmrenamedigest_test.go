package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/store"
)

func TestConfirmRenameFromRefusesEveryChainEditTheRecordCannotShow(t *testing.T) {
	edits := map[string]func(string) string{
		"allow_fail added": func(s string) string {
			return strings.Replace(s, "    - id: fetch\n", "    - id: fetch\n      allow_fail: true\n", 1)
		},
		"export added": func(s string) string {
			return strings.Replace(s, "          thing_id: id\n", "          thing_id: id\n          thing_name: name\n", 1)
		},
		"literal turned into a var": func(s string) string {
			s = strings.Replace(s, "name: cli-renamed\n", "name: cli-renamed\nvars:\n    n: widget\n", 1)
			return strings.Replace(s, "          name: widget\n", "          name: ${vars.n}\n", 1)
		},
		"uuid turned into a fixed var": func(s string) string {
			s = strings.Replace(s, "name: cli-renamed\n", "name: cli-renamed\nvars:\n    k: fixed-key\n", 1)
			return strings.Replace(s, "idempotency_key: ${uuid}", "idempotency_key: ${vars.k}", 1)
		},
		"expectation tolerance widened": func(s string) string {
			return strings.Replace(s, "          - path: name\n            equals: widget\n", "          - path: name\n            not_empty: true\n", 1)
		},
		"description changed": func(s string) string {
			return strings.Replace(s, "name: cli-renamed\n", "name: cli-renamed\ndescription: now different\n", 1)
		},
	}
	for what, edit := range edits {
		t.Run(what, func(t *testing.T) {
			renameThingFlow(t, edit)
			out, err := confirmRename(t, "-by", "bob@example.test")
			if err == nil || !strings.Contains(err.Error(), "is not cli-thing-flow renamed") {
				t.Fatalf("%s is not a pure rename and must be refused, got %v\n%s", what, err, out)
			}
			if _, err := os.Stat(".shrt/safespots/cli-thing-flow.json"); err != nil {
				t.Fatalf("a refused rename leaves the safe spot where it was: %v", err)
			}
		})
	}
}

func TestConfirmRenameFromRefusesASafeSpotWithoutAChainDigest(t *testing.T) {
	renameThingFlow(t, nil)
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	spot, err := e.store.LoadSafeSpot("cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	if spot.ChainDigest == "" {
		t.Fatal("an approved safe spot records the digest of the chain it was confirmed with")
	}
	spot.ChainDigest = ""
	spot.Digest = spot.ComputeDigest()
	raw, _ := json.MarshalIndent(spot, "", "  ")
	writeFile(t, ".shrt/safespots/cli-thing-flow.json", string(raw))
	if spot.DigestKind() != store.DigestCurrent {
		t.Fatalf("an older safe spot without a chain digest is still sealed, got %s", spot.DigestKind())
	}
	out, err := confirmRename(t, "-by", "bob@example.test")
	if err == nil || !strings.Contains(err.Error(), "does not record the chain it was confirmed with") {
		t.Fatalf("without a chain digest nothing proves the rename is pure, so it is refused, got %v\n%s", err, out)
	}
	if strings.Contains(out, "were not compared") {
		t.Fatalf("no partial comparison is offered:\n%s", out)
	}
}
