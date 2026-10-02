package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/config"
)

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() { _ = os.Chdir(prev) }
}

func readGitignore(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func loginWorkspace(t *testing.T, readme string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".shrt", "descriptor.binpb"), string(catalogtest.Descriptor()))
	if readme != "" {
		writeFile(t, filepath.Join(dir, "README.md"), readme)
	}
	return dir
}

func loginServer(t *testing.T, answer string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func initAllowingIncomplete(t *testing.T, args []string) error {
	err := runInit(t.Context(), args)
	var coded *exitError
	if errors.As(err, &coded) && coded.code == 3 {
		return nil
	}
	return err
}

func cliInit(t *testing.T, args ...string) string {
	t.Helper()
	return captureStdout(t, func() {
		if err := initAllowingIncomplete(t, append([]string{"-build=false", "-agents=false"}, args...)); err != nil {
			t.Fatalf("init %v: %v", args, err)
		}
	})
}

const accountsReadme = "# backend\n\n| username | password | role |\n|---|---|---|\n| admin | s3cret | ADMIN |\n| clerk | s3cret | CLERK |\n"

func TestInitWritesACommentFreeConfigTheGateScriptAndARerunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	defer chdir(t, dir)()
	out := cliInit(t)
	raw, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Fatalf("the written config carries a comment line %q:\n%s", line, raw)
		}
	}
	for _, key := range []string{"envelope_path", "envelope_ok", "item_envelope_path", "read_only_prefixes", "code_fields", "validate_output", ".shrt/contracts/", "write .shrt/ci-gate.sh"} {
		if !strings.Contains(out, key) {
			t.Errorf("init prints %s:\n%s", key, out)
		}
	}
	gate, err := os.ReadFile(".shrt/ci-gate.sh")
	if err != nil || !strings.Contains(string(gate), "shrt chain lint -strict\n") || !strings.HasSuffix(string(gate), "exec shrt gate\n") {
		t.Fatalf("init writes the README's CI gate as a file: %v\n%s", err, gate)
	}
	cfg, err := config.Load(dir)
	if err != nil || cfg.Latency == nil || !cfg.Latency.Fail {
		t.Fatalf("a new config loads and fails a confirmed slowdown: %v\n%s", err, raw)
	}
	again := cliInit(t)
	if strings.Contains(again, "write .shrt/chains") || strings.Contains(again, "write .shrt/{chains") ||
		strings.Contains(again, "shrt contract init -all") || !strings.Contains(again, "shrt doctor") {
		t.Fatalf("a rerun claims no writes, drops the first-time next: list and still points at doctor:\n%s", again)
	}
	writeFile(t, ".shrt/config.yaml", "target:\n    base_url: http://127.0.0.1:1\ndescriptor:\n    file: .shrt/descriptor.binpb\n")
	cliInit(t)
	if raw, _ := os.ReadFile(".shrt/config.yaml"); strings.Contains(string(raw), "latency") {
		t.Fatalf("an existing config is the user's: init must not add latency to it:\n%s", raw)
	}
}

func TestInitForceRefreshesTheKitAndOnlyForceConfigRebuildsTheConfig(t *testing.T) {
	dir := t.TempDir()
	defer chdir(t, dir)()
	declared := "target:\n    base_url: https://backend.example\ndescriptor:\n    file: .shrt/descriptor.binpb\n" +
		"conventions:\n    item_envelope_path: results[].error.code\n" +
		"paths:\n    chains: .shrt/chains\n    runs: .shrt/runs\n    safespots: .shrt/safespots\n"
	writeFile(t, ".shrt/config.yaml", declared)
	cliInit(t, "-force")
	if got, _ := os.ReadFile(".shrt/config.yaml"); !strings.Contains(string(got), "item_envelope_path") || !strings.Contains(string(got), "backend.example") {
		t.Fatalf("init -force keeps the adopter's config:\n%s", got)
	}
	cliInit(t, "-force-config", "-base-url", "https://new.example")
	if got, _ := os.ReadFile(".shrt/config.yaml"); !strings.Contains(string(got), "new.example") {
		t.Fatalf("-force-config rebuilds the config:\n%s", got)
	}
}

func TestInitWritesAGitignoreEntryOnlyWhereOneIsMissing(t *testing.T) {
	complete := strings.Join(initGitignore(config.Default()), "\n") + "\n"
	for _, c := range []struct {
		name, before, prefix string
		added                bool
	}{
		{"no gitignore", "", "", true},
		{"some lines present", "/bin\n.shrt/runs/\n", "/bin\n.shrt/runs/\n", true},
		{"no final newline", "/bin", "/bin\n", true},
		{"complete", complete, complete, false},
	} {
		root := t.TempDir()
		if c.before != "" {
			writeFile(t, filepath.Join(root, ".gitignore"), c.before)
		}
		added, err := ensureGitignore(root, initGitignore(config.Default()))
		if err != nil || added != c.added {
			t.Errorf("%s: added %v, want %v (%v)", c.name, added, c.added, err)
		}
		body := readGitignore(t, root)
		if !strings.HasPrefix(body, c.prefix) || strings.Count(body, ".shrt/runs/") != 1 ||
			!strings.Contains(body, ".shrt/tokens.json") || (c.added && !strings.Contains(body, ".shrt/scratch/\n")) {
			t.Errorf("%s: the adopter's lines stay first, nothing doubles, tokens, runs and scratch are ignored: %q", c.name, body)
		}
		if !c.added && body != complete {
			t.Errorf("%s: a complete file is left alone: %q", c.name, body)
		}
	}
}

func TestInitAndCatalogBuildNameTheMissingToolAndExitTwo(t *testing.T) {
	dir := t.TempDir()
	defer chdir(t, dir)()
	t.Setenv("PATH", t.TempDir())
	err := runInit(context.Background(), []string{"-agents=false"})
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 2 || !strings.Contains(err.Error(), "shrt catalog build") {
		t.Fatalf("init with no descriptor built exits 2 naming the command that fixes it: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".shrt", "config.yaml")); statErr != nil {
		t.Errorf("init still writes the config it got as far as: %v", statErr)
	}
	err = runCatalog(context.Background(), []string{"build"})
	if err == nil || !strings.Contains(err.Error(), "not on PATH") || !strings.Contains(err.Error(), "buf") {
		t.Fatalf("catalog build names buf missing from PATH: %v", err)
	}
}

func TestInitBaseURLComesFromTheFlagThePortFileOrTheDefault(t *testing.T) {
	baseURL := func(port string, args ...string) (string, string) {
		dir := loginWorkspace(t, "")
		if port != "" {
			writeFile(t, filepath.Join(dir, ".port"), port)
		}
		defer chdir(t, dir)()
		out := cliInit(t, args...)
		cfg, err := config.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Target.BaseURL, out
	}
	if got, out := baseURL("18570\n"); got != "http://127.0.0.1:18570" || !strings.Contains(out, ".port") {
		t.Fatalf("a .port file sets the default base_url and init says so: %s\n%s", got, out)
	}
	if got, _ := baseURL("18570", "-base-url", "http://10.0.0.1:1"); got != "http://10.0.0.1:1" {
		t.Fatalf("-base-url wins over .port: %s", got)
	}
	if got, _ := baseURL("not a port"); got != "http://127.0.0.1:8080" {
		t.Fatalf("a .port that is not a number is ignored: %s", got)
	}

	dir := shopWorkspace(t, "")
	defer chdir(t, dir)()
	out := cliInit(t, "-base-url", "http://backend.test:9000")
	if strings.Contains(out, "set it") || !strings.Contains(out, "write .shrt/config.yaml: target.base_url http://backend.test:9000\n") {
		t.Fatalf("with -base-url, init names the target it wrote and does not ask to set it:\n%s", out)
	}
	out = cliInit(t, "-base-url", "http://other.test:1")
	if !strings.Contains(out, "-base-url http://other.test:1 was NOT applied") || !strings.Contains(out, "http://backend.test:9000") {
		t.Fatalf("a -base-url ignored because the config exists is said so:\n%s", out)
	}
	fresh := shopWorkspace(t, "")
	defer chdir(t, fresh)()
	out = cliInit(t)
	if !strings.Contains(out, "target.base_url http://127.0.0.1:8080, the default: set it to your backend") {
		t.Fatalf("without -base-url init says to set it:\n%s", out)
	}
	if !strings.Contains(out, "    envelope_path: status.code ") || !strings.Contains(out, "    item_envelope_path: results[].status.code ") ||
		strings.Contains(out, "envelope_path: error.code") || strings.Contains(out, "results[].error.code") ||
		strings.Count(out, "no conventions: block declared") != 1 || strings.Contains(out, "paste this at the TOP LEVEL") {
		t.Fatalf("the conventions guide, printed once, uses the detected status.code envelope:\n%s", out)
	}
}

func TestInitRerunRewritesTheUntouchedExampleWithTheObservedEnvelope(t *testing.T) {
	dir := shopWorkspace(t, "")
	defer chdir(t, dir)()
	cliInit(t)
	example := filepath.Join(dir, ".shrt", "chains", "example.yaml.template")
	cfg, _ := os.ReadFile(".shrt/config.yaml")
	writeFile(t, ".shrt/config.yaml", string(cfg)+"conventions:\n    envelope_path: status.code\n    envelope_ok: SUCCESS\n")
	out := cliInit(t)
	raw, _ := os.ReadFile(example)
	if strings.Contains(string(raw), exampleOKPlaceholder) || strings.Count(string(raw), "- path: status.code\n        equals: SUCCESS") != 2 ||
		!strings.Contains(out, "write .shrt/chains/example.yaml.template") {
		t.Fatalf("a re-run that finds envelope_ok rewrites the untouched scaffold and says so:\n%s\n%s", raw, out)
	}
	edited := strings.Replace(string(raw), "SUCCESS", exampleOKPlaceholder, 1) + "# mine\n"
	writeFile(t, example, edited)
	cliInit(t)
	if got, _ := os.ReadFile(example); string(got) != edited {
		t.Fatalf("a template the user edited is kept:\n%s", got)
	}
}

func TestInitObservesTheEnvelopeInALoginAnswerOrExitsThreeSayingWhatToExport(t *testing.T) {
	srv := loginServer(t, `{"error":{"code":"DONE"},"access_token":"tok","expires_at":"0"}`)
	dir := loginWorkspace(t, "")
	restore := chdir(t, dir)
	t.Setenv("API_USER", "u")
	t.Setenv("API_PASSWORD", "p")
	out := cliInit(t, "-base-url", srv.URL)
	cfg, err := config.Load(dir)
	if err != nil || cfg.Conventions.EnvelopePath != "error.code" || cfg.Conventions.EnvelopeOK != "DONE" {
		t.Fatalf("init logged in and saw error.code DONE, so it writes that: %v %+v\n%s", err, cfg.Conventions, out)
	}
	if strings.Contains(out, "no conventions: block declared") || strings.Count(out, "envelope_ok DONE") != 1 {
		t.Fatalf("an observed envelope is said in one line, with no paste advice:\n%s", out)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, ".shrt", "chains", "example.yaml.template")); !strings.Contains(string(raw), "equals: DONE") {
		t.Fatalf("the example chain asserts the observed success value:\n%s", raw)
	}
	restore()

	kept := loginWorkspace(t, "")
	body := "target:\n    base_url: " + srv.URL + "\ndescriptor:\n    file: .shrt/descriptor.binpb\n" +
		"auth:\n    call: shrt.test.v1.AuthService/Login\n    body:\n        username: ${env.API_USER}\n        password: ${env.API_PASSWORD}\n    token_path: access_token\n" +
		"conventions:\n    read_only_prefixes: [Fetch]\n"
	writeFile(t, filepath.Join(kept, ".shrt", "config.yaml"), body)
	restore = chdir(t, kept)
	out = cliInit(t)
	if raw, _ := os.ReadFile(".shrt/config.yaml"); string(raw) != body || strings.Contains(out, "conventions") {
		t.Fatalf("a conventions: block the user wrote is neither rewritten nor advised on:\n%s\n---\n%s", raw, out)
	}
	restore()

	t.Setenv("API_USER", "")
	t.Setenv("API_PASSWORD", "")
	os.Unsetenv("API_USER")
	os.Unsetenv("API_PASSWORD")
	for _, verbose := range []bool{false, true} {
		restore := chdir(t, loginWorkspace(t, ""))
		args := []string{"-build=false", "-agents=false", "-base-url", srv.URL}
		if verbose {
			args = append(args, "-v")
		}
		var err error
		out := captureStdout(t, func() { err = runInit(t.Context(), args) })
		restore()
		var coded *exitError
		if !errors.As(err, &coded) || coded.code != 3 ||
			err.Error() != "init incomplete: conventions not observed; export API_PASSWORD, API_USER and re-run shrt init" {
			t.Fatalf("-v %v: exits 3 naming what to export, got %v:\n%s", verbose, err, out)
		}
		if strings.Contains(out, "no conventions: block declared") != verbose || strings.Contains(out, "next:") ||
			strings.Contains(out, "credentials not exported") || !strings.Contains(out, "write .shrt/chains/example.yaml.template") {
			t.Errorf("-v %v: init writes what it can and prints the block only under -v:\n%s", verbose, out)
		}
	}
	declared := loginWorkspace(t, "")
	defer chdir(t, declared)()
	writeFile(t, ".shrt/config.yaml", "target:\n    base_url: "+srv.URL+"\nauth:\n    call: shop.auth.v1.AuthService/Login\n    body:\n        username: ${env.API_USER}\n        password: ${env.API_PASSWORD}\n    token_path: access_token\nconventions:\n    envelope_path: error.code\n    envelope_ok: DONE\n")
	if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
		t.Fatalf("conventions already declared, so init is complete without the credentials: %v", err)
	}
}

func TestInitAddsAProfilePerRoleTheReadmeNamesOnceItsCredentialsAreExported(t *testing.T) {
	dir := loginWorkspace(t, accountsReadme)
	defer chdir(t, dir)()
	t.Setenv("CLERK_USER", "")
	t.Setenv("CLERK_PASSWORD", "")
	t.Setenv("DB_USER", "postgres")
	t.Setenv("DB_PASSWORD", "x")
	out := cliInit(t)
	if !strings.Contains(out, "auth.profiles.clerk") || !strings.Contains(out, "CLERK_USER") {
		t.Fatalf("the README names clerk and no clerk credentials are exported, so init says how to add one:\n%s", out)
	}
	if unset := cliInit(t); !strings.Contains(unset, "CLERK_USER") || !strings.Contains(unset, "re-run shrt init") {
		t.Fatalf("a re-run still says how to get the profile:\n%s", unset)
	}
	raw, _ := os.ReadFile(".shrt/config.yaml")
	edited := strings.Replace(string(raw), "target:\n    base_url: http://127.0.0.1:8080", "target:\n    base_url: http://127.0.0.1:9999", 1)
	edited = strings.Replace(edited, "    - '**.created_at'\n", "    - '**.created_at'\n    - '**.kept_by_hand'\n", 1)
	writeFile(t, ".shrt/config.yaml", edited)
	t.Setenv("CLERK_USER", "clerk")
	t.Setenv("CLERK_PASSWORD", "x")
	out = cliInit(t)
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	clerk := cfg.Auth.Profiles["clerk"]
	if clerk == nil || clerk.Call != cfg.Auth.Call || clerk.Body["username"] != "${env.CLERK_USER}" || clerk.Body["password"] != "${env.CLERK_PASSWORD}" ||
		!strings.Contains(out, "profile clerk") {
		t.Fatalf("init adds the clerk profile on the same login and names it:\n%s", out)
	}
	if _, bogus := cfg.Auth.Profiles["db"]; bogus {
		t.Fatal("DB_USER is set but the README names no db role")
	}
	if cfg.Target.BaseURL != "http://127.0.0.1:9999" || !strings.Contains(strings.Join(cfg.Volatile, ","), "**.kept_by_hand") {
		t.Fatalf("adding a profile keeps the rest of the config: %s %v", cfg.Target.BaseURL, cfg.Volatile)
	}
	if raw, _ := os.ReadFile(".shrt/config.yaml"); strings.Contains(string(raw), "#") {
		t.Fatalf("the config carries no comments:\n%s", raw)
	}
	if again := cliInit(t); strings.Contains(again, "profile clerk") {
		t.Fatalf("a third init adds nothing:\n%s", again)
	}
	defer chdir(t, loginWorkspace(t, accountsReadme))()
	if out := cliInit(t); !strings.Contains(out, "profile clerk") {
		t.Fatalf("with the clerk credentials exported, a first init scaffolds the profile:\n%s", out)
	}
	if cfg, err := config.Load("."); err != nil || cfg.Auth == nil || cfg.Auth.Profiles["clerk"] == nil {
		t.Fatalf("the first init writes auth with the clerk profile: %v", err)
	}
}

func TestABrokenConfigsAdviceMatchesWhatInitDoes(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	writeFile(t, ".shrt/config.yaml", "target:\n    base_url: http://127.0.0.1:1\n    timeuot: 5s\ndescriptor:\n    file: .shrt/descriptor.binpb\n")
	_, err := loadEnv(false)
	if err == nil || strings.Contains(err.Error(), "writes a fresh config") || !strings.Contains(err.Error(), "-force-config") ||
		!strings.Contains(err.Error(), `unknown key "timeuot" at line 3 (did you mean "timeout"?)`) {
		t.Fatalf("name the key, the line, and the init flag that rebuilds the config: %v", err)
	}
	var initErr error
	captureStdout(t, func() { initErr = runInit(context.Background(), nil) })
	if initErr == nil || !strings.Contains(initErr.Error(), `unknown key "timeuot"`) {
		t.Fatalf("init stops on the same parse error, got %v", initErr)
	}
}

func TestInitPrintsWhatItWroteWhatToCheckAndTheNextCommand(t *testing.T) {
	srv := loginServer(t, `{"error":{"code":"DONE"},"access_token":"tok","expires_at":"0"}`)
	dir := loginWorkspace(t, "")
	defer chdir(t, dir)()
	t.Setenv("API_USER", "u")
	t.Setenv("API_PASSWORD", "p")
	out := cliInit(t, "-base-url", srv.URL)
	for _, want := range []string{"write .shrt/config.yaml", srv.URL, "write .shrt/chains/, .shrt/runs/, .shrt/safespots/, .shrt/contracts/\n",
		"GUESSED", "envelope_ok DONE", "write .shrt/ci-gate.sh\n", "next: shrt doctor, then .shrt/docs/README.md \"Quickstart\"\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	for _, not := range []string{"subagent", "latency: {fail: true}", "copy it to <name>.yaml", "shrt contract quality -phase happy", "\n\n\n"} {
		if strings.Contains(out, not) {
			t.Errorf("want no %q in:\n%s", not, out)
		}
	}
	if n := strings.Count(out, "\n"); n > 12 {
		t.Errorf("init prints what it wrote, what to check and the next command, %d lines:\n%s", n, out)
	}
}
