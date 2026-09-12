package agentkit_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	coredistillation "github.com/N4darae/shrt"
	"github.com/N4darae/shrt/agentkit"
)

var markdownRef = regexp.MustCompile(`[A-Za-z0-9_./-]*[A-Za-z0-9_-]\.md`)

func installed(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, a := range append(agentkit.ClaudeAssets(), agentkit.DocAssets()...) {
		out[filepath.ToSlash(a.Dest)] = true
	}
	return out
}

func TestEveryDocReferenceInTheKitIsInstalled(t *testing.T) {
	dests := installed(t)
	for _, a := range agentkit.ClaudeAssets() {
		raw, err := agentkit.ReadAsset(a)
		if err != nil {
			t.Fatal(err)
		}
		for _, ref := range markdownRef.FindAllString(string(raw), -1) {
			if !strings.Contains(ref, "/") {
				continue
			}
			if dests[ref] {
				continue
			}
			t.Errorf("%s points at %q, which `shrt init` does not install\ninstalled: %v",
				filepath.ToSlash(a.Dest), ref, sortedKeys(dests))
		}
	}
}

func TestTheFourWorkingDocsAreRouteTargets(t *testing.T) {
	skill := agentkit.ClaudeAssets()[0]
	raw, err := agentkit.ReadAsset(skill)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agentkit.DocAssets() {
		if !strings.Contains(string(raw), filepath.ToSlash(a.Dest)) {
			t.Errorf("%s is installed but %s routes nothing to it", a.Dest, skill.Dest)
		}
	}
}

func TestKitCarriesNoMachinePath(t *testing.T) {
	banned := []string{"/root/", "/home/", "/Users/", `C:\`}
	assets := append(agentkit.ClaudeAssets(), agentkit.DocAssets()...)
	assets = append(assets,
		agentkit.Asset{Source: "templates/config.yaml"},
		agentkit.Asset{Source: "templates/chain.example.yaml"})
	for _, a := range assets {
		var raw []byte
		var err error
		if a.FS == nil {
			raw, err = agentkit.Read(a.Source)
		} else {
			raw, err = agentkit.ReadAsset(a)
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range banned {
			if strings.Contains(string(raw), b) {
				t.Errorf("%s contains %q — an adopting repo does not have this machine's paths", a.Source, b)
			}
		}
	}
}

func TestDocAssetsShipEveryDistillationDoc(t *testing.T) {
	entries, err := os.ReadDir(distillationDir())
	if err != nil {
		t.Fatal(err)
	}
	onDisk := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			onDisk = append(onDisk, e.Name())
		}
	}
	sort.Strings(onDisk)
	shipped := append([]string{}, coredistillation.DocNames...)
	sort.Strings(shipped)
	if strings.Join(onDisk, ",") != strings.Join(shipped, ",") {
		t.Fatalf("core_distillation holds %v but the kit ships %v\n"+
			"the kit embeds the distillation directly, so a doc added there must be listed in DocNames "+
			"or adopting repos never see it", onDisk, shipped)
	}
	for _, a := range agentkit.DocAssets() {
		want, err := os.ReadFile(filepath.Join(distillationDir(), a.Source))
		if err != nil {
			t.Fatal(err)
		}
		got, err := agentkit.ReadAsset(a)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s does not match core_distillation/%s", a.Dest, a.Source)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func distillationDir() string {
	if st, err := os.Stat("../core_distillation"); err == nil && st.IsDir() {
		return "../core_distillation"
	}
	return ".."
}
