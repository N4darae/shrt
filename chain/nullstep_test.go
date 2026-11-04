package chain_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestNullStepsAreRejected(t *testing.T) {
	for _, empty := range []string{"", "null", "~"} {
		t.Run("yaml_"+empty, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "broken.yaml")
			raw := "steps:\n  - call: ThingService/Fetch\n  - " + empty + "\n"
			if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := chain.LoadFile(path)
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "step 2") {
				t.Fatalf("want the file and empty step index, got %v", err)
			}
		})
	}
	for _, steps := range [][]*chain.Step{nil, {nil}, {{Call: "S/Fetch"}, nil}} {
		c := &chain.Chain{Name: "broken", Steps: steps}
		if err := c.Normalize(); err == nil {
			t.Fatal("an empty chain or step must not normalize")
		}
	}
}

func TestNullStepDoesNotHideOtherChains(t *testing.T) {
	dir := t.TempDir()
	for name, raw := range map[string]string{
		"good.yaml":   "steps:\n  - call: ThingService/Fetch\n",
		"broken.yaml": "steps:\n  -\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	chains, broken, err := chain.LoadDirPartial(dir)
	if err != nil || len(chains) != 1 || chains[0].Name != "good" || len(broken) != 1 {
		t.Fatalf("want the good chain and one load error, got %v %v %v", chains, broken, err)
	}
	if _, err := chain.Resolve(dir, "good"); err != nil {
		t.Fatal(err)
	}
}
