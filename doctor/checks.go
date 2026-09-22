package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
)

const (
	CheckDocs       = "docs"
	CheckDescriptor = "descriptor"
	CheckIgnored    = "gitignore"
	CheckTokens     = "tokens"
	CheckAuth       = "auth"

	CheckConventions = "conventions"
)

func loadCatalog(cfg *config.Config) (*catalog.Catalog, error) {
	return catalog.Load(cfg.Abs(cfg.Descriptor.File))
}

const reinstallDocs = `rm -rf .shrt/docs && shrt init -agents=false -build=false
Not 'init -force-config': that also rewrites .shrt/config.yaml from defaults and
discards the auth profiles, conventions and volatile paths your chains depend on.`

func checkDocs(_ context.Context, cfg *config.Config, opts Options, r *Report) {
	if opts.Docs == nil || len(opts.DocNames) == 0 {
		r.add(CheckDocs, LevelWarn, "this build carries no embedded docs, so the installed copy went unchecked", "")
		return
	}
	missing, drifted := []string{}, []string{}
	for _, name := range opts.DocNames {
		want, err := fs.ReadFile(opts.Docs, name)
		if err != nil {
			r.add(CheckDocs, LevelError, fmt.Sprintf("%s is named by this build but not embedded in it: %v", name, err), "")
			continue
		}
		got, readErr := os.ReadFile(cfg.Abs(filepath.Join(config.DocsDir, name)))
		switch {
		case readErr != nil:
			missing = append(missing, name)
		case !bytes.Equal(got, want):
			drifted = append(drifted, name)
		}
	}
	if len(missing) > 0 {
		r.add(CheckDocs, LevelError,
			fmt.Sprintf("%s/ is missing %s — the installed skill routes every question there",
				config.DocsDir, strings.Join(missing, ", ")),
			reinstallDocs)
	}
	if len(drifted) > 0 {
		r.add(CheckDocs, LevelError,
			fmt.Sprintf("%s/ has drifted from this binary in %s", config.DocsDir, strings.Join(drifted, ", ")),
			"An agent reads the installed copy, and this binary enforces its own. Where they disagree the\n"+
				"agent follows rules nothing checks, and a lint that should be red comes back green.\n"+reinstallDocs)
	}
	if len(missing) == 0 && len(drifted) == 0 {
		r.add(CheckDocs, LevelOK,
			fmt.Sprintf("%d installed doc(s) match the copy embedded in this binary", len(opts.DocNames)), "")
	}
}

const staleDescriptor = `shrt catalog build
A stale descriptor does not fail loudly. A response whose message it does not know is kept
RAW — camelCase, zero values omitted — so an assertion on a zero-valued field reads as "the
field is missing", and an author fixes the chain instead of the descriptor.`

func checkDescriptor(ctx context.Context, cfg *config.Config, opts Options, r *Report) {
	file := cfg.Abs(cfg.Descriptor.File)
	if file == "" {
		r.add(CheckDescriptor, LevelError, "descriptor.file is empty in .shrt/config.yaml", "")
		return
	}
	have, err := os.ReadFile(file)
	if err != nil {
		r.add(CheckDescriptor, LevelError,
			fmt.Sprintf("%s is missing, so every catalog, contract and chain command fails", cfg.Descriptor.File),
			"shrt catalog build")
		return
	}
	fresh, err := opts.Rebuild(ctx, cfg)
	if err != nil {
		r.add(CheckDescriptor, LevelWarn,
			fmt.Sprintf("%s is %d bytes; staleness went UNCHECKED: %v", cfg.Descriptor.File, len(have), err),
			"Not a pass — nothing compared the descriptor to the protos it came from.")
		return
	}
	if !bytes.Equal(have, fresh) {
		r.add(CheckDescriptor, LevelError,
			fmt.Sprintf("%s does not match a rebuild from %q (on disk %d bytes, rebuilt %d)",
				cfg.Descriptor.File, cfg.Descriptor.Source, len(have), len(fresh)),
			staleDescriptor)
		return
	}
	r.add(CheckDescriptor, LevelOK,
		fmt.Sprintf("%s matches a rebuild from %q", cfg.Descriptor.File, cfg.Descriptor.Source), "")
}

func checkIgnored(_ context.Context, cfg *config.Config, opts Options, r *Report) {
	want := cfg.NeverCommit()
	if len(want) == 0 {
		return
	}
	ignored, err := opts.Ignored(cfg.Root, want)
	if err != nil || ignored == nil {
		ignored = ignoredByFile(cfg.Root, want)
	}
	secret := config.DirName + "/" + config.TokensFile
	leaked := []string{}
	for _, p := range want {
		if ignored[p] {
			continue
		}
		if p == secret {
			r.add(CheckIgnored, LevelError,
				fmt.Sprintf("%s is not ignored, and it holds every bearer token this repo has minted", p),
				"Add it to .gitignore, or let 'shrt init' do it. If it has already been committed, the\n"+
					"tokens have to be ROTATED: deleting the file does not remove it from history.")
			continue
		}
		leaked = append(leaked, p)
	}
	if len(leaked) > 0 {
		r.add(CheckIgnored, LevelWarn,
			fmt.Sprintf("not ignored: %s — build output and machine-local receipts", strings.Join(leaked, ", ")),
			"shrt init -build=false -agents=false   # appends only the lines that are missing")
	}
	if len(leaked) == 0 && ignored[secret] {
		r.add(CheckIgnored, LevelOK, fmt.Sprintf("%d path(s) that must never be committed are ignored", len(want)), "")
	}
}

func checkTokenCache(_ context.Context, cfg *config.Config, opts Options, r *Report) {
	cache := cfg.Abs(filepath.Join(config.DirName, config.TokensFile))
	info, err := os.Stat(cache)
	if err != nil {
		r.add(CheckTokens, LevelOK, "no token cache on disk yet", "")
		return
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		r.add(CheckTokens, LevelWarn,
			fmt.Sprintf("%s is mode %04o — every account on this machine can read the tokens in it", config.DirName+"/"+config.TokensFile, mode),
			fmt.Sprintf("chmod 600 %s", config.DirName+"/"+config.TokensFile))
	}
	raw, err := os.ReadFile(cache)
	if err != nil {
		r.add(CheckTokens, LevelWarn, fmt.Sprintf("cannot read the token cache: %v", err), "")
		return
	}
	entries := map[string]struct {
		ExpiresAt time.Time `json:"expires_at"`
	}{}
	if err := json.Unmarshal(raw, &entries); err != nil {
		r.add(CheckTokens, LevelWarn,
			fmt.Sprintf("the token cache is not readable JSON (%v), so every login will re-authenticate", err),
			fmt.Sprintf("rm %s", config.DirName+"/"+config.TokensFile))
		return
	}
	now := opts.Now()
	expired := 0
	for _, e := range entries {
		if !e.ExpiresAt.IsZero() && e.ExpiresAt.Before(now) {
			expired++
		}
	}
	r.add(CheckTokens, LevelOK,
		fmt.Sprintf("%d cached token(s), %d expired by their own expires_at. A token the backend "+
			"has forgotten -- a restart, a revoke -- does not look expired here and cannot: only "+
			"the backend knows. It is dropped when a call comes back unauthenticated, by HTTP "+
			"status or in the response envelope, and the call is retried once against a fresh "+
			"login. 'rm %s' forces that without a call", len(entries), expired,
			config.DirName+"/"+config.TokensFile), "")
}

var envRef = regexp.MustCompile(`\$\{\s*env\.([^}\s]+)\s*\}`)

var secretish = []string{"password", "secret", "token", "api_key", "apikey", "credential", "passphrase"}

func checkAuth(_ context.Context, cfg *config.Config, opts Options, r *Report) {
	if cfg.Auth == nil {
		r.add(CheckAuth, LevelWarn,
			"no auth: block in .shrt/config.yaml, so every authenticated call answers 401",
			"GRAMMAR.md §4 is the key table for writing one, or re-run 'shrt init' and let it guess\n"+
				"from the descriptor. Check the call it picks: a guess is not a fact.")
		return
	}
	profiles := cfg.AuthProfiles()
	names := cfg.AuthProfileNames()
	literals := []string{}
	unset := []string{}
	unresolvable := []string{}
	for _, name := range names {
		p := profiles[name]
		if p == nil {
			continue
		}
		for _, problem := range chain.AuthBodyReferenceProblems(p.Body) {
			unresolvable = append(unresolvable, name+": "+problem)
		}
		for _, field := range literalSecrets(p.Body) {
			literals = append(literals, name+"."+field)
		}
		for _, v := range envRefsIn(p.Body) {
			if opts.Env(v) == "" {
				unset = append(unset, fmt.Sprintf("%s (%s)", v, name))
			}
		}
	}
	if len(unresolvable) > 0 {
		r.add(CheckAuth, LevelError,
			fmt.Sprintf("an auth body reads what it cannot resolve, so every run that needs the profile dies "+
				"at its first step with 'auth body: unresolved reference':\n%s", strings.Join(unresolvable, "\n")),
			"An auth body is resolved before any step runs: it can read ${env.NAME}, ${uuid}, ${now},\n"+
				"${nowunix} and ${today} (with a +/-seconds offset), and nothing else. Export the value and\n"+
				"write ${env.NAME} instead.")
	}
	if len(literals) > 0 {
		r.add(CheckAuth, LevelError,
			fmt.Sprintf("a credential is written into .shrt/config.yaml: %s", strings.Join(literals, ", ")),
			"config.yaml is committed. Replace the value with ${env.NAME} and export it, then treat the\n"+
				"one that was in the file as disclosed and rotate it.")
	}
	if len(unset) > 0 {
		r.add(CheckAuth, LevelWarn,
			fmt.Sprintf("unset in this shell: %s", strings.Join(unset, ", ")),
			"A run dies at step 1 with status error and SENDS NOTHING, which is a fixture problem and\n"+
				"not a backend one. Export them before 'shrt run'.")
	}
	if len(literals) == 0 && len(unset) == 0 && len(unresolvable) == 0 {
		r.add(CheckAuth, LevelOK,
			fmt.Sprintf("%d auth profile(s): %s, every ${env.*} they read is set", len(names), strings.Join(names, ", ")), "")
	}
}

func envRefsIn(v any) []string {
	seen := map[string]bool{}
	out := []string{}
	walkStrings(v, func(s string) {
		for _, m := range envRef.FindAllStringSubmatch(s, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
	})
	sort.Strings(out)
	return out
}

func literalSecrets(body map[string]any) []string {
	out := []string{}
	for key, v := range body {
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" || strings.Contains(s, "${") {
			continue
		}
		lower := strings.ToLower(key)
		for _, hint := range secretish {
			if strings.Contains(lower, hint) {
				out = append(out, key)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func walkStrings(v any, fn func(string)) {
	switch t := v.(type) {
	case string:
		fn(t)
	case map[string]any:
		for _, item := range t {
			walkStrings(item, fn)
		}
	case []any:
		for _, item := range t {
			walkStrings(item, fn)
		}
	}
}

func ignoredByFile(root string, want []string) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return out
	}
	lines := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines[strings.TrimPrefix(line, "/")] = true
	}
	for _, p := range want {
		out[p] = lines[p] || lines[strings.TrimSuffix(p, "/")] || coveredByDir(lines, p)
	}
	return out
}

func coveredByDir(lines map[string]bool, p string) bool {
	for dir := path.Dir(p); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if lines[dir] || lines[dir+"/"] {
			return true
		}
	}
	return false
}

func gitCheckIgnore(root string, paths []string) (map[string]bool, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "check-ignore", "--stdin")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		if code := cmd.ProcessState.ExitCode(); code != 1 {
			return nil, fmt.Errorf("git check-ignore: %w", err)
		}
	}
	ignored := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ignored[line] = true
		}
	}
	return ignored, nil
}

func rebuildDescriptor(ctx context.Context, cfg *config.Config) ([]byte, error) {
	if strings.TrimSpace(cfg.Descriptor.Source) == "" {
		return nil, fmt.Errorf("descriptor.source is unset, so there is nothing to rebuild from")
	}
	tmp, err := os.MkdirTemp("", "shrt-doctor-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, "descriptor.binpb")
	spec := catalog.BuildSpec{Input: cfg.Abs(cfg.Descriptor.Source), Output: out, Binary: cfg.Descriptor.Binary}
	if err := catalog.Build(ctx, spec); err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

func checkConventions(ctx context.Context, cfg *config.Config, opts Options, r *Report) {
	cat, err := opts.Catalog(cfg)
	if err != nil || cat == nil {
		r.add(CheckConventions, LevelWarn,
			"the response messages were not read, so the envelope settings went UNCHECKED",
			"Fix the descriptor finding above and re-run.")
		return
	}
	checkEnvelopePath(cat, cfg, r)
	checkItemEnvelopePath(cat, cfg, r)
}

func checkEnvelopePath(cat *catalog.Catalog, cfg *config.Config, r *Report) {
	configured := strings.TrimSpace(cfg.Conventions.EnvelopePath)
	found := catalog.DetectEnvelope(cat)
	if configured != "" {
		if declaredSomewhere(cat, configured) {
			r.add(CheckConventions, LevelOK,
				fmt.Sprintf("envelope_path %q is a field of at least one response message", configured), "")
			return
		}
		r.add(CheckConventions, LevelError,
			fmt.Sprintf("envelope_path %q is not a field of ANY response message, so every step "+
				"asserting the envelope compares against a path that is never present", configured),
			"Check it against one response you know was refused, then correct conventions.envelope_path.")
		return
	}
	if len(found) == 0 || found[0].Path == chain.DefaultEnvelopePath {
		r.add(CheckConventions, LevelOK,
			fmt.Sprintf("no envelope_path set, and %q is what the response messages carry", chain.DefaultEnvelopePath), "")
		return
	}
	best := found[0]
	r.add(CheckConventions, LevelWarn,
		fmt.Sprintf("no conventions.envelope_path is set, so the default %q is in force — but %d of "+
			"your %d response message(s) carry a verdict at %q instead and none carries the default",
			chain.DefaultEnvelopePath, best.Count, len(cat.Methods()), best.Path),
		"Check it against one response you know was refused, then set it:\n"+
			"    conventions:\n        envelope_path: "+best.Path+"\n        envelope_ok: <the value there meaning success>\n"+
			"shrt cannot guess envelope_ok: it reads field NAMES, and the success value is data.")
}

func checkItemEnvelopePath(cat *catalog.Catalog, cfg *config.Config, r *Report) {
	configured := strings.TrimSpace(cfg.Conventions.ItemEnvelopePath)
	if configured != "" {
		if err := chain.ValidateItemEnvelopeIn(cat, configured); err != nil {
			r.add(CheckConventions, LevelError,
				fmt.Sprintf("item_envelope_path %q: %v", configured, err),
				"Correct conventions.item_envelope_path, or remove it if this backend has no per-item verdict.")
			return
		}
		r.add(CheckConventions, LevelOK,
			fmt.Sprintf("item_envelope_path %q is declared by at least one response message", configured), "")
		return
	}
	envelope := effectiveEnvelopePath(cat, cfg)
	found := catalog.DetectItemEnvelope(cat, envelope)
	if len(found) == 0 {
		return
	}
	best := found[0]
	why := fmt.Sprintf("its elements carry the envelope path %q", envelope)
	if best.SameMessage {
		why = fmt.Sprintf("its elements carry %q as the same message the top-level envelope uses", envelope)
	}
	others := ""
	if len(found) > 1 {
		names := []string{}
		for _, c := range found[1:] {
			names = append(names, c.Path)
		}
		others = "\nOther repeated fields with the same shape: " + strings.Join(names, ", ") + "."
	}
	r.add(CheckConventions, LevelWarn,
		fmt.Sprintf("no conventions.item_envelope_path is set, and %d response message(s) may carry a "+
			"per-item verdict at %q (%s). A batch rpc can answer OK at the top level while refusing every "+
			"line, and a step asserting only the envelope passes having achieved nothing",
			best.Count, best.Path, why),
		"This is a guess from field names and message types, not something shrt observed: verify it "+
			"against one batch response you know refused an item before setting it:\n"+
			"    conventions:\n        item_envelope_path: "+best.Path+"\n"+
			"A path that is really a business field (a list row's state, say) turns every step reading "+
			"that list red. A wrong envelope_path fails loudly, because every assertion misses. A missing "+
			"item_envelope_path fails silently, which is why this is checked here."+others)
}

func effectiveEnvelopePath(cat *catalog.Catalog, cfg *config.Config) string {
	if p := strings.TrimSpace(cfg.Conventions.EnvelopePath); p != "" {
		return p
	}
	if found := catalog.DetectEnvelope(cat); len(found) > 0 {
		return found[0].Path
	}
	return chain.DefaultEnvelopePath
}

func declaredSomewhere(cat *catalog.Catalog, path string) bool {
	segs := chain.SplitPath(path)
	for _, m := range cat.Methods() {
		if catalog.HasPath(catalog.DescribeMessage(m.Output()).Fields, segs) {
			return true
		}
	}
	return false
}
