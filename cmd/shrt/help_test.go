package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	saved := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 0, 4096)
		chunk := make([]byte, 1024)
		for {
			n, err := r.Read(chunk)
			buf = append(buf, chunk[:n]...)
			if err != nil {
				break
			}
		}
		done <- string(buf)
	}()
	fn()
	_ = w.Close()
	os.Stdout = saved
	return <-done
}

func TestEveryCommandAnswersDashHWithoutFailing(t *testing.T) {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no commands registered")
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			for _, arg := range []string{"-h", "--help"} {
				err := commands[name].run(context.Background(), []string{arg})
				if err != nil && !errors.Is(err, flag.ErrHelp) {
					t.Fatalf("shrt %s %s returned %v; the top-level usage promises this prints flags", name, arg, err)
				}
			}
		})
	}
}

func TestGroupCommandsAlsoAcceptBareHelp(t *testing.T) {
	for _, g := range []group{catalogGroup, chainGroup, contractGroup} {
		t.Run(g.name, func(t *testing.T) {
			err := commands[g.name].run(context.Background(), []string{"help"})
			if err != nil && !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("shrt %s help returned %v", g.name, err)
			}
		})
	}
}

func TestGroupHelpListsEverySubcommandItDispatches(t *testing.T) {
	for _, g := range []group{catalogGroup, chainGroup, contractGroup} {
		t.Run(g.name, func(t *testing.T) {
			out := captureStdout(t, g.printHelp)
			for _, s := range g.subs {
				if !strings.Contains(out, s.name) {
					t.Errorf("shrt %s -h does not mention subcommand %q", g.name, s.name)
				}
				if s.summary == "" {
					t.Errorf("subcommand %s %s has no summary", g.name, s.name)
				}
				if !strings.Contains(out, s.summary) {
					t.Errorf("shrt %s -h does not print the summary for %q", g.name, s.name)
				}
			}
			if !strings.Contains(out, g.usage()) {
				t.Errorf("shrt %s -h does not print its usage line", g.name)
			}
		})
	}
}

func TestGroupMisuseExitsTwoAndNamesTheSubcommands(t *testing.T) {
	for _, g := range []group{catalogGroup, chainGroup, contractGroup} {
		t.Run(g.name, func(t *testing.T) {
			var coded *exitError
			for _, err := range []error{g.missing(), g.unknown("frobnicate")} {
				if !errors.As(err, &coded) {
					t.Fatalf("%v is not an exitError, so shrt would exit 1 for misuse", err)
				}
				if coded.code != 2 {
					t.Errorf("misuse exits %d, want 2", coded.code)
				}
				if !strings.Contains(err.Error(), g.names()) {
					t.Errorf("misuse message does not list the subcommands: %v", err)
				}
			}
		})
	}
}

var readmeCandidates = []string{"../../readme.md", "../../core_distillation/README.md", "../../README.md"}

func readmes(t *testing.T) map[string]string {
	t.Helper()
	found := map[string]string{}
	for _, p := range readmeCandidates {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		found[filepath.Base(filepath.Dir(p))+"/"+filepath.Base(p)] = string(raw)
	}
	if len(found) == 0 {
		t.Fatalf("no command table found: none of %v exists", readmeCandidates)
	}
	return found
}

func TestReadmeListsEveryTopLevelCommand(t *testing.T) {
	for file, readme := range readmes(t) {
		for name := range commands {
			if !strings.Contains(readme, "shrt "+name) {
				t.Errorf("%s never mentions `shrt %s` — the command table is the front door, and a "+
					"command missing from it is one nobody finds", file, name)
			}
		}
	}
}

func TestReadmeListsEverySubcommandTheCLIDispatches(t *testing.T) {
	for file, readme := range readmes(t) {
		for _, g := range []group{catalogGroup, chainGroup, contractGroup} {
			for _, s := range g.subs {
				if !strings.Contains(readme, "shrt "+g.name+" "+s.name) {
					t.Errorf("%s's command table does not mention `shrt %s %s` — the table is the "+
						"front door, and a subcommand missing from it is one nobody finds", file, g.name, s.name)
				}
			}
		}
	}
}
