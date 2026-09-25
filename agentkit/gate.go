package agentkit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	coredistillation "github.com/N4darae/shrt"
)

const GateScriptPath = ".shrt/ci-gate.sh"

func GateScript() ([]byte, error) {
	readme, err := coredistillation.Docs.ReadFile("README.md")
	if err != nil {
		return nil, err
	}
	_, after, ok := bytes.Cut(readme, []byte("\n### CI gate\n"))
	if !ok {
		return nil, errors.New("README.md has no CI gate section")
	}
	_, body, ok := bytes.Cut(after, []byte("\n```bash\n"))
	if !ok {
		return nil, errors.New("README.md's CI gate section has no bash block")
	}
	script, _, ok := bytes.Cut(body, []byte("\n```\n"))
	if !ok {
		return nil, errors.New("README.md's CI gate bash block is not closed")
	}
	return append([]byte("#!/usr/bin/env bash\n"), append(script, '\n')...), nil
}

func InstallGateScript(root string, force bool) (bool, error) {
	dest := filepath.Join(root, filepath.FromSlash(GateScriptPath))
	if _, err := os.Stat(dest); err == nil && !force {
		return false, nil
	}
	raw, err := GateScript()
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(dest, raw, 0o755); err != nil {
		return false, err
	}
	return true, os.Chmod(dest, 0o755)
}
