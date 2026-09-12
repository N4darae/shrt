package coredistillation

import "embed"

//go:embed README.md GRAMMAR.md PLAYBOOK.md PITFALLS.md
var Docs embed.FS

var DocNames = []string{"README.md", "GRAMMAR.md", "PLAYBOOK.md", "PITFALLS.md"}
