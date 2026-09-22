package doctor

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/N4darae/shrt/config"
)

const CheckBuild = "build"

type Build struct {
	Version  string
	Revision string
	Time     string
	Modified bool
	Go       string
}

func ReadBuild() Build {
	b := Build{Version: "unknown"}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return b
	}
	b.Go = info.GoVersion
	if v := strings.TrimSpace(info.Main.Version); v != "" {
		b.Version = v
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			b.Revision = s.Value
		case "vcs.time":
			b.Time = s.Value
		case "vcs.modified":
			b.Modified = s.Value == "true"
		}
	}
	return b
}

func (b Build) ShortRevision() string {
	if len(b.Revision) > 12 {
		return b.Revision[:12]
	}
	return b.Revision
}

func (b Build) String() string {
	parts := []string{b.Version}
	if rev := b.ShortRevision(); rev != "" && !strings.Contains(b.Version, rev) {
		parts = append(parts, rev)
	}
	if b.Modified && !strings.Contains(b.Version, "dirty") {
		parts = append(parts, "dirty")
	}
	if b.Time != "" {
		parts = append(parts, "built "+b.Time)
	}
	if b.Go != "" {
		parts = append(parts, b.Go)
	}
	return strings.Join(parts, ", ")
}

func (b Build) Provenance() string {
	switch {
	case b.Revision == "" && b.Version == "unknown":
		return "this binary carries no version and no commit, so nothing can say which rules it enforces"
	case b.Revision == "":
		return "no commit is stamped into this binary, so its version is the only handle on which rules it enforces"
	case b.Modified:
		return "built from a working tree with uncommitted changes, so the commit it names does not describe it"
	default:
		return ""
	}
}

func checkBuild(_ context.Context, _ *config.Config, _ Options, r *Report) {
	b := ReadBuild()
	detail := b.String()
	if note := b.Provenance(); note != "" {
		detail = fmt.Sprintf("%s — %s", detail, note)
	}
	r.add(CheckBuild, LevelOK, detail, "")
}
