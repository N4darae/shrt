package agentkit

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"

	coredistillation "github.com/N4darae/shrt"
)

//go:embed skill/SKILL.md agents/*.md templates/*.yaml
var files embed.FS

const DocsDir = ".shrt/docs"

type Asset struct {
	FS     fs.FS
	Source string
	Dest   string
}

func ClaudeAssets() []Asset {
	return []Asset{
		{FS: files, Source: "skill/SKILL.md", Dest: filepath.Join(".claude", "skills", "shrt", "SKILL.md")},
		{FS: files, Source: "agents/shrt-contract-author.md", Dest: filepath.Join(".claude", "agents", "shrt-contract-author.md")},
	}
}

func DocAssets() []Asset {
	out := make([]Asset, 0, len(coredistillation.DocNames))
	for _, name := range coredistillation.DocNames {
		out = append(out, Asset{
			FS:     coredistillation.Docs,
			Source: name,
			Dest:   filepath.Join(filepath.FromSlash(DocsDir), name),
		})
	}
	return out
}

func Read(name string) ([]byte, error) {
	return files.ReadFile(name)
}

func ReadAsset(a Asset) ([]byte, error) {
	return fs.ReadFile(a.FS, a.Source)
}

func Install(root string, assets []Asset, force bool) ([]string, error) {
	written := []string{}
	for _, a := range assets {
		raw, err := ReadAsset(a)
		if err != nil {
			return written, err
		}
		dest := filepath.Join(root, a.Dest)
		if _, err := os.Stat(dest); err == nil && !force {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(dest, raw, 0o644); err != nil {
			return written, err
		}
		written = append(written, filepath.ToSlash(a.Dest))
	}
	return written, nil
}

func Walk(fn func(path string, d fs.DirEntry) error) error {
	return fs.WalkDir(files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return fn(path, d)
	})
}
