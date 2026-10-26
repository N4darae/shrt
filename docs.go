package coredistillation

import (
	"embed"
	"io/fs"
)

//go:embed docs/agent/README.md docs/agent/GRAMMAR.md docs/agent/PLAYBOOK.md docs/agent/PITFALLS.md
var embedded embed.FS

var Docs, _ = fs.Sub(embedded, "docs/agent")

var DocNames = []string{"README.md", "GRAMMAR.md", "PLAYBOOK.md", "PITFALLS.md"}
