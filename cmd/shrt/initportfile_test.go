package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func initBaseURL(t *testing.T, port string, args ...string) (string, string) {
	t.Helper()
	dir := loginWorkspace(t, "")
	if port != "" {
		writeFile(t, filepath.Join(dir, ".port"), port)
	}
	restore := chdir(t, dir)
	defer restore()
	out := captureStdout(t, func() {
		if err := runInit(t.Context(), append([]string{"-build=false", "-agents=false"}, args...)); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Target.BaseURL, out
}

func TestInitDefaultsTheBaseURLToTheRepoPortFile(t *testing.T) {
	got, out := initBaseURL(t, "18570\n")
	if got != "http://127.0.0.1:18570" || !strings.Contains(out, ".port") {
		t.Fatalf("a .port file holding 18570 makes the default base_url http://127.0.0.1:18570, and init says where it came from: %s\n%s", got, out)
	}
	if got, _ := initBaseURL(t, "18570", "-base-url", "http://10.0.0.1:1"); got != "http://10.0.0.1:1" {
		t.Fatalf("-base-url wins over .port: %s", got)
	}
	if got, _ := initBaseURL(t, "not a port"); got != "http://127.0.0.1:8080" {
		t.Fatalf("a .port that is not a number is ignored: %s", got)
	}
}
