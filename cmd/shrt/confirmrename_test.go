package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/store"
)

func renameThingFlow(t *testing.T, edit func(string) string) {
	t.Helper()
	approvedThingFlow(t)
	raw := string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml"))
	renamed := strings.Replace(raw, "name: cli-thing-flow\n", "name: cli-renamed\n", 1)
	if edit != nil {
		renamed = edit(renamed)
	}
	writeFile(t, ".shrt/chains/cli-renamed.yaml", renamed)
	if err := os.Remove(".shrt/chains/cli-thing-flow.yaml"); err != nil {
		t.Fatal(err)
	}
}

func confirmRename(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = runConfirm(context.Background(), append([]string{"cli-renamed", "-rename-from", "cli-thing-flow"}, args...))
	})
	return out, err
}

func TestConfirmRenameFromCarriesAnIdenticalChainsSafeSpot(t *testing.T) {
	renameThingFlow(t, nil)
	out, err := confirmRename(t, "-by", "bob@example.test")
	if err != nil {
		t.Fatalf("an identical chain under a new name keeps its safe spot: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no new approval is needed") {
		t.Fatalf("the output must say why no approval is asked for:\n%s", out)
	}
	if _, err := os.Stat(".shrt/safespots/cli-thing-flow.json"); !os.IsNotExist(err) {
		t.Fatalf("the old safe spot file must be moved, got %v", err)
	}
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	spot, err := e.store.LoadSafeSpot("cli-renamed")
	if err != nil {
		t.Fatal(err)
	}
	if spot.Chain != "cli-renamed" || spot.ConfirmedBy != "alice@example.test" || spot.DigestKind() != store.DigestCurrent {
		t.Fatalf("the approval provenance is kept and the digest re-sealed, got %+v", spot)
	}
	if len(spot.Renamed) != 1 || spot.Renamed[0].From != "cli-thing-flow" || spot.Renamed[0].By != "bob@example.test" {
		t.Fatalf("the rename is recorded, got %+v", spot.Renamed)
	}
	var verr error
	vout := captureStdout(t, func() { verr = runVerify(context.Background(), []string{"cli-renamed", "-quiet"}) })
	if verr != nil {
		t.Fatalf("verify of the renamed chain passes against the carried safe spot: %v\n%s", verr, vout)
	}
}

func TestConfirmRenameFromRefusesAnyOtherDifference(t *testing.T) {
	renameThingFlow(t, func(s string) string { return strings.Replace(s, "name: widget\n", "name: gadget\n", 1) })
	_, err := confirmRename(t, "-by", "bob@example.test")
	if err == nil || !strings.Contains(err.Error(), "is not cli-thing-flow renamed") || !strings.Contains(err.Error(), "body name") {
		t.Fatalf("a body change is not a rename and must be refused naming it, got %v", err)
	}
	if _, err := os.Stat(".shrt/safespots/cli-thing-flow.json"); err != nil {
		t.Fatalf("a refused rename leaves the safe spot where it was: %v", err)
	}
}

func TestConfirmRenameFromNeedsBy(t *testing.T) {
	renameThingFlow(t, nil)
	if _, err := confirmRename(t); err == nil || !strings.Contains(err.Error(), "-by") {
		t.Fatalf("a rename records who carried the approval; without -by it is refused, got %v", err)
	}
}

func TestConfirmRenameFromRefusesWhenTheNewChainHasASafeSpot(t *testing.T) {
	renameThingFlow(t, nil)
	writeFile(t, ".shrt/safespots/cli-renamed.json", string(mustRead(t, ".shrt/safespots/cli-thing-flow.json")))
	if _, err := confirmRename(t, "-by", "bob@example.test"); err == nil || !strings.Contains(err.Error(), "already has a safe spot") {
		t.Fatalf("a rename never replaces a safe spot, got %v", err)
	}
}

func TestConfirmRenameFromRefusesWhileTheOldChainExists(t *testing.T) {
	renameThingFlow(t, nil)
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-renamed.yaml")),
		"name: cli-renamed\n", "name: cli-thing-flow\n", 1))
	if _, err := confirmRename(t, "-by", "bob@example.test"); err == nil || !strings.Contains(err.Error(), "still exists") {
		t.Fatalf("a copy is not a rename, got %v", err)
	}
}
