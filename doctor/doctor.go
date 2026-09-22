package doctor

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/config"
)

type Level int

const (
	LevelOK Level = iota
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelError:
		return "FAIL"
	case LevelWarn:
		return "WARN"
	default:
		return "ok"
	}
}

type Finding struct {
	Check  string
	Level  Level
	Detail string
	Remedy string
}

type Report struct {
	Root     string
	Findings []Finding
}

type Options struct {
	Docs     fs.FS
	DocNames []string
	Env      func(string) string
	Now      func() time.Time
	Ignored  func(root string, paths []string) (map[string]bool, error)
	Rebuild  func(ctx context.Context, cfg *config.Config) ([]byte, error)
	Catalog  func(cfg *config.Config) (*catalog.Catalog, error)
}

func (o Options) withDefaults() Options {
	if o.Env == nil {
		o.Env = os.Getenv
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Ignored == nil {
		o.Ignored = gitCheckIgnore
	}
	if o.Rebuild == nil {
		o.Rebuild = rebuildDescriptor
	}
	if o.Catalog == nil {
		o.Catalog = loadCatalog
	}
	return o
}

var checks = []func(context.Context, *config.Config, Options, *Report){
	checkBuild,
	checkDocs,
	checkDescriptor,
	checkIgnored,
	checkTokenCache,
	checkAuth,
	checkConventions,
}

func Run(ctx context.Context, cfg *config.Config, opts Options) *Report {
	opts = opts.withDefaults()
	r := &Report{Root: cfg.Root}
	for _, check := range checks {
		check(ctx, cfg, opts, r)
	}
	return r
}

func (r *Report) add(check string, level Level, detail, remedy string) {
	r.Findings = append(r.Findings, Finding{Check: check, Level: level, Detail: detail, Remedy: remedy})
}

func (r *Report) Worst() Level {
	worst := LevelOK
	for _, f := range r.Findings {
		if f.Level > worst {
			worst = f.Level
		}
	}
	return worst
}

func (r *Report) Count(level Level) int {
	n := 0
	for _, f := range r.Findings {
		if f.Level == level {
			n++
		}
	}
	return n
}

func (r *Report) Failed(strict bool) bool {
	if r.Worst() == LevelError {
		return true
	}
	return strict && r.Worst() == LevelWarn
}

func (r *Report) Checks() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, f := range r.Findings {
		if !seen[f.Check] {
			seen[f.Check] = true
			out = append(out, f.Check)
		}
	}
	sort.Strings(out)
	return out
}

const remedyIndent = "       "

func (r *Report) Text() string {
	var b strings.Builder
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "%-4s %s: %s\n", f.Level, f.Check, f.Detail)
		if f.Level == LevelOK || f.Remedy == "" {
			continue
		}
		for _, line := range strings.Split(strings.TrimRight(f.Remedy, "\n"), "\n") {
			fmt.Fprintf(&b, "%s%s\n", remedyIndent, line)
		}
	}
	return b.String()
}

func (r *Report) Summary() string {
	fails, warns := r.Count(LevelError), r.Count(LevelWarn)
	switch {
	case fails > 0:
		return fmt.Sprintf("%d failing, %d warning, %d ok", fails, warns, r.Count(LevelOK))
	case warns > 0:
		return fmt.Sprintf("%d warning, %d ok", warns, r.Count(LevelOK))
	default:
		return fmt.Sprintf("%d ok", r.Count(LevelOK))
	}
}
