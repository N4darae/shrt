package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coredistillation "github.com/N4darae/shrt"
	"github.com/N4darae/shrt/agentkit"
	"github.com/N4darae/shrt/config"
)

func adoptedRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg := config.Default()
	cfg.Root = root
	cfg.Descriptor.Source = ""
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := agentkit.Install(root, agentkit.DocAssets(), true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, cfg.Descriptor.File), []byte("descriptor"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"),
		[]byte(strings.Join(cfg.NeverCommit(), "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

func doctorExitCode(t *testing.T, args ...string) int {
	t.Helper()
	var code int
	captureStdout(t, func() {
		err := runDoctor(context.Background(), args)
		if err == nil {
			code = 0
			return
		}
		var coded *exitError
		if !errors.As(err, &coded) {
			t.Fatalf("shrt doctor returned %v, which is not an exitError, so the shell sees 1 with no "+
				"distinction between a broken install and a crash", err)
			return
		}
		code = coded.code
	})
	return code
}

func TestDoctorIsQuietOnAFreshlyInstalledRepo(t *testing.T) {
	adoptedRepo(t)

	if code := doctorExitCode(t); code != 0 {
		t.Fatalf("a repo that shrt init just wrote must pass its own check, got exit %d", code)
	}
}

func TestDoctorExitsNonZeroWhenTheInstalledDocsContradictTheBinary(t *testing.T) {
	root := adoptedRepo(t)
	installed := filepath.Join(root, config.DocsDir, coredistillation.DocNames[0])
	raw, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, append(raw, []byte("\na rule from an older binary\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := doctorExitCode(t); code != 1 {
		t.Fatalf("drifted docs are what send an agent to rules nothing enforces; want exit 1, got %d", code)
	}
}

func TestDoctorPassesWarningsUntilStrictSaysOtherwise(t *testing.T) {
	root := adoptedRepo(t)
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".shrt/tokens.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := doctorExitCode(t); code != 0 {
		t.Fatalf("an unignored run directory is not worth failing an interactive run over, got exit %d", code)
	}
	if code := doctorExitCode(t, "-strict"); code != 1 {
		t.Fatalf("-strict is the flag a CI job uses to refuse warnings, got exit %d", code)
	}
}

func TestDoctorEmitsJSONForTooling(t *testing.T) {
	adoptedRepo(t)

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), []string{"-json"}); err != nil {
			t.Fatal(err)
		}
	})

	for _, want := range []string{`"Findings"`, `"Check"`, `"Level"`, `"Detail"`} {
		if !strings.Contains(out, want) {
			t.Errorf("-json output is missing %s: %s", want, out)
		}
	}
}
