package doctor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/doctor"
)

var embedded = fstest.MapFS{
	"README.md":   {Data: []byte("the loop, and the four rules\n")},
	"GRAMMAR.md":  {Data: []byte("every key shrt accepts\n")},
	"PLAYBOOK.md": {Data: []byte("the procedures\n")},
}

var docNames = []string{"README.md", "GRAMMAR.md", "PLAYBOOK.md"}

func repo(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := config.Default()
	cfg.Root = root
	cfg.Descriptor.Source = ""
	cfg.Auth = &config.Auth{
		Call:      "acme.iam.v1.AuthService/Login",
		Body:      map[string]any{"username": "${env.ACME_USER}", "password": "${env.ACME_PASSWORD}"},
		TokenPath: "access_token",
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Abs(cfg.Descriptor.File), catalogtest.Descriptor(), 0o644); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, ".gitignore"), strings.Join(cfg.NeverCommit(), "\n")+"\n")
	installDocs(t, cfg)
	loaded, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func installDocs(t *testing.T, cfg *config.Config) {
	t.Helper()
	for _, name := range docNames {
		write(t, cfg.Abs(filepath.Join(config.DocsDir, name)), string(embedded[name].Data))
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func options() doctor.Options {
	return doctor.Options{
		Docs:     embedded,
		DocNames: docNames,
		Env:      func(string) string { return "set" },
		Now:      func() time.Time { return time.Unix(1790035200, 0).UTC() },
		Ignored:  nil,
		Rebuild: func(context.Context, *config.Config) ([]byte, error) {
			return catalogtest.Descriptor(), nil
		},
	}
}

func run(t *testing.T, cfg *config.Config, opts doctor.Options) *doctor.Report {
	t.Helper()
	if opts.Ignored == nil {
		opts.Ignored = func(string, []string) (map[string]bool, error) {
			return nil, fmt.Errorf("no git here, fall back to reading .gitignore")
		}
	}
	return doctor.Run(context.Background(), cfg, opts)
}

func find(t *testing.T, r *doctor.Report, check string) doctor.Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Check == check && f.Level != doctor.LevelOK {
			return f
		}
	}
	for _, f := range r.Findings {
		if f.Check == check {
			return f
		}
	}
	t.Fatalf("no finding for %q in:\n%s", check, r.Text())
	return doctor.Finding{}
}

func TestASoundInstallationReportsNothingToFix(t *testing.T) {
	r := run(t, repo(t), options())

	if r.Failed(true) {
		t.Fatalf("a freshly installed repo must be clean even under -strict:\n%s", r.Text())
	}
	if len(r.Checks()) < 5 {
		t.Errorf("every check has to report, including the ones that pass: %v", r.Checks())
	}
}

func TestDriftedDocsFailBecauseTheAgentReadsTheInstalledCopyNotTheBinary(t *testing.T) {
	cfg := repo(t)
	write(t, cfg.Abs(filepath.Join(config.DocsDir, "PLAYBOOK.md")),
		"the procedures\nplus a rule from an older binary that nothing enforces\n")

	r := run(t, cfg, options())

	got := find(t, r, doctor.CheckDocs)
	if got.Level != doctor.LevelError {
		t.Fatalf("installed docs that contradict the binary are how an agent follows rules nothing "+
			"checks; want FAIL, got %s: %s", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "PLAYBOOK.md") {
		t.Errorf("the finding has to name the file that drifted: %q", got.Detail)
	}
}

func TestAMissingDocFailsRatherThanPassingQuietly(t *testing.T) {
	cfg := repo(t)
	if err := os.Remove(cfg.Abs(filepath.Join(config.DocsDir, "GRAMMAR.md"))); err != nil {
		t.Fatal(err)
	}

	got := find(t, run(t, cfg, options()), doctor.CheckDocs)

	if got.Level != doctor.LevelError || !strings.Contains(got.Detail, "GRAMMAR.md") {
		t.Fatalf("want a FAIL naming GRAMMAR.md, got %s: %s", got.Level, got.Detail)
	}
}

func TestAnUnignoredTokenCacheFailsBecauseACommittedTokenHasToBeRotated(t *testing.T) {
	cfg := repo(t)
	write(t, filepath.Join(cfg.Root, ".gitignore"), ".shrt/runs/\n.shrt/descriptor.binpb\n.shrt/docs/\n")

	got := find(t, run(t, cfg, options()), doctor.CheckIgnored)

	if got.Level != doctor.LevelError {
		t.Fatalf("the token cache is the one entry that cannot be fixed after the fact; want FAIL, got %s: %s",
			got.Level, got.Detail)
	}
	if !strings.Contains(got.Remedy, "ROTATED") {
		t.Errorf("the remedy has to say the tokens are burned, not merely deleted: %q", got.Remedy)
	}
}

func TestUnignoredBuildOutputOnlyWarns(t *testing.T) {
	cfg := repo(t)
	write(t, filepath.Join(cfg.Root, ".gitignore"), ".shrt/tokens.json\n")

	got := find(t, run(t, cfg, options()), doctor.CheckIgnored)

	if got.Level != doctor.LevelWarn {
		t.Fatalf("committing a run record is noise, not a credential leak; want WARN, got %s: %s",
			got.Level, got.Detail)
	}
}

func TestADirectoryRuleCoversThePathsUnderneathIt(t *testing.T) {
	cfg := repo(t)
	write(t, filepath.Join(cfg.Root, ".gitignore"), ".shrt/\n")

	r := run(t, cfg, options())

	if got := find(t, r, doctor.CheckIgnored); got.Level != doctor.LevelOK {
		t.Fatalf("ignoring .shrt/ wholesale already covers every path under it: %s: %s", got.Level, got.Detail)
	}
}

func TestALiteralPasswordInConfigIsAFailureNotAStyleNote(t *testing.T) {
	cfg := repo(t)
	cfg.Auth = &config.Auth{
		Call:      "acme.iam.v1.AuthService/Login",
		Body:      map[string]any{"username": "${env.ACME_USER}", "password": "hunter2"},
		TokenPath: "access_token",
	}

	got := find(t, run(t, cfg, options()), doctor.CheckAuth)

	if got.Level != doctor.LevelError {
		t.Fatalf("config.yaml is committed, so a literal credential there is already disclosed; "+
			"want FAIL, got %s: %s", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "password") {
		t.Errorf("the finding has to name the field: %q", got.Detail)
	}
	if !strings.Contains(got.Remedy, "rotate") {
		t.Errorf("the remedy has to say rotate, not just move it to an env var: %q", got.Remedy)
	}
}

func TestAnUnsetAuthEnvVarWarnsAndNamesTheProfileItBelongsTo(t *testing.T) {
	cfg := repo(t)
	cfg.Auth = &config.Auth{
		Call:      "acme.iam.v1.AuthService/Login",
		Body:      map[string]any{"username": "${env.ACME_USER}", "password": "${env.ACME_PASSWORD}"},
		TokenPath: "access_token",
		Profiles: map[string]*config.Auth{
			"partner": {Call: "acme.partner.v1.AuthService/Login", Body: map[string]any{"password": "${env.PARTNER_PASSWORD}"}},
		},
	}
	opts := options()
	opts.Env = func(name string) string {
		if name == "PARTNER_PASSWORD" {
			return ""
		}
		return "set"
	}

	got := find(t, run(t, cfg, opts), doctor.CheckAuth)

	if got.Level != doctor.LevelWarn {
		t.Fatalf("want WARN, got %s: %s", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "PARTNER_PASSWORD") || !strings.Contains(got.Detail, "partner") {
		t.Errorf("a repo with six profiles needs to know WHICH one is unexported: %q", got.Detail)
	}
	if !strings.Contains(got.Remedy, "SENDS NOTHING") {
		t.Errorf("the remedy has to say the run dies before it sends anything, so the reader stops "+
			"blaming the backend: %q", got.Remedy)
	}
}

func TestAnAuthBodyReferenceTheLoginCannotResolveFails(t *testing.T) {
	cfg := repo(t)
	cfg.Auth = &config.Auth{
		Call:      "acme.iam.v1.AuthService/Login",
		Body:      map[string]any{"username": "${env.ACME_USER}", "password": "${env.ACME_PASSWORD}"},
		TokenPath: "access_token",
		Profiles: map[string]*config.Auth{
			"owner": {Call: "acme.iam.v1.AuthService/Login", Body: map[string]any{
				"username": "${vars.owner_user}", "password": "${env.OWNER_PASSWORD}",
			}},
		},
	}

	got := find(t, run(t, cfg, options()), doctor.CheckAuth)

	if got.Level != doctor.LevelError {
		t.Fatalf("an auth body is resolved with no vars, exports or steps, so ${vars.owner_user} kills "+
			"every run that needs the profile, at its first step; want FAIL, got %s: %s", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "${vars.owner_user}") || !strings.Contains(got.Detail, "owner") {
		t.Errorf("the finding must name the reference and the profile: %q", got.Detail)
	}
	if !strings.Contains(got.Remedy, "env.") {
		t.Errorf("the remedy must say what an auth body can read: %q", got.Remedy)
	}
}

func TestNoAuthBlockAtAllIsReported(t *testing.T) {
	cfg := repo(t)
	cfg.Auth = nil

	got := find(t, run(t, cfg, options()), doctor.CheckAuth)

	if got.Level != doctor.LevelWarn || !strings.Contains(got.Detail, "401") {
		t.Fatalf("init says so when it cannot scaffold auth, and doctor has to keep saying it: %s: %s",
			got.Level, got.Detail)
	}
}

func TestAWorldReadableTokenCacheWarns(t *testing.T) {
	cfg := repo(t)
	cache := cfg.Abs(filepath.Join(config.DirName, config.TokensFile))
	write(t, cache, `{"default#abc":{"token":"t","expires_at":"2030-01-01T00:00:00Z"}}`)
	if err := os.Chmod(cache, 0o644); err != nil {
		t.Fatal(err)
	}

	got := find(t, run(t, cfg, options()), doctor.CheckTokens)

	if got.Level != doctor.LevelWarn || !strings.Contains(got.Detail, "0644") {
		t.Fatalf("want a WARN naming the mode, got %s: %s", got.Level, got.Detail)
	}
}

func TestTheTokenCacheCountsExpiredEntriesWithoutCallingThemAProblem(t *testing.T) {
	cfg := repo(t)
	cache := cfg.Abs(filepath.Join(config.DirName, config.TokensFile))
	body, err := json.Marshal(map[string]any{
		"default#a": map[string]any{"token": "t", "expires_at": "2020-01-01T00:00:00Z"},
		"default#b": map[string]any{"token": "t", "expires_at": "2030-01-01T00:00:00Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, cache, string(body))
	if err := os.Chmod(cache, 0o600); err != nil {
		t.Fatal(err)
	}

	got := find(t, run(t, cfg, options()), doctor.CheckTokens)

	if got.Level != doctor.LevelOK {
		t.Fatalf("expired entries are pruned on the next write, so they are a count and not a defect: %s: %s",
			got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "2 cached") || !strings.Contains(got.Detail, "1 expired") {
		t.Errorf("the count is the point: %q", got.Detail)
	}
}

func TestAMissingDescriptorFailsAndSaysWhichCommandBuildsIt(t *testing.T) {
	cfg := repo(t)
	if err := os.Remove(cfg.Abs(cfg.Descriptor.File)); err != nil {
		t.Fatal(err)
	}

	got := find(t, run(t, cfg, options()), doctor.CheckDescriptor)

	if got.Level != doctor.LevelError || !strings.Contains(got.Remedy, "catalog build") {
		t.Fatalf("want a FAIL pointing at 'shrt catalog build', got %s: %s / %s", got.Level, got.Detail, got.Remedy)
	}
}

func TestAStaleDescriptorFailsAndExplainsWhyItIsNotLoud(t *testing.T) {
	cfg := repo(t)
	opts := options()
	opts.Rebuild = func(context.Context, *config.Config) ([]byte, error) {
		return []byte("descriptor bytes, plus the rpc added last week"), nil
	}

	got := find(t, run(t, cfg, opts), doctor.CheckDescriptor)

	if got.Level != doctor.LevelError {
		t.Fatalf("want FAIL, got %s: %s", got.Level, got.Detail)
	}
	if !strings.Contains(got.Remedy, "RAW") {
		t.Errorf("the remedy has to name the silent failure, or the reader treats staleness as cosmetic: %q", got.Remedy)
	}
}

func TestAnUncheckableDescriptorWarnsAndSaysItIsNotAPass(t *testing.T) {
	cfg := repo(t)
	opts := options()
	opts.Rebuild = func(context.Context, *config.Config) ([]byte, error) {
		return nil, fmt.Errorf("%q is not on PATH", "buf")
	}

	got := find(t, run(t, cfg, opts), doctor.CheckDescriptor)

	if got.Level != doctor.LevelWarn {
		t.Fatalf("no buf means staleness went unchecked, which is neither a pass nor a failure; got %s: %s",
			got.Level, got.Detail)
	}
	if !strings.Contains(got.Remedy, "Not a pass") {
		t.Errorf("the report has to say so in words: %q", got.Remedy)
	}
}

func TestStrictPromotesWarningsToAFailingExit(t *testing.T) {
	cfg := repo(t)
	cfg.Auth = nil

	r := run(t, cfg, options())

	if r.Failed(false) {
		t.Errorf("a warning alone must not fail the default run: %s", r.Summary())
	}
	if !r.Failed(true) {
		t.Errorf("-strict is what a CI job uses to refuse warnings: %s", r.Summary())
	}
}

func TestTheReportPrintsARemedyOnlyForWhatIsWrong(t *testing.T) {
	cfg := repo(t)
	write(t, cfg.Abs(filepath.Join(config.DocsDir, "README.md")), "an older loop\n")

	text := run(t, cfg, options()).Text()

	if !strings.Contains(text, "rm -rf .shrt/docs") {
		t.Errorf("a finding without its remedy leaves the reader to guess: %q", text)
	}
	if strings.Count(text, "shrt init -agents=false") > 1 {
		t.Errorf("the same remedy must not repeat for checks that passed: %q", text)
	}
}

func TestTheIgnoreCheckAsksAboutTheRepoRootNotTheCurrentDirectory(t *testing.T) {
	cfg := repo(t)
	opts := options()
	seen := ""
	opts.Ignored = func(root string, _ []string) (map[string]bool, error) {
		seen = root
		return nil, fmt.Errorf("fall back to reading .gitignore")
	}

	run(t, cfg, opts)

	if seen != cfg.Root {
		t.Fatalf("git check-ignore resolves relative paths against its own working directory, so a "+
			"doctor run from a subdirectory would ask about the wrong file: want %q, got %q", cfg.Root, seen)
	}
}
