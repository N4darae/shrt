package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/contract"
)

func contractShow(args []string) error {
	fs := flag.NewFlagSet("contract show", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	filter := fs.String("filter", "", "show every rpc whose name contains this substring")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) == 0 && *filter == "" {
		return fmt.Errorf("usage: shrt contract show <rpc>... | -filter <substring>")
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	lib, _, err := e.library()
	if err != nil {
		return err
	}

	methods := []*catalog.Method{}
	if *filter != "" {
		needle := strings.ToLower(*filter)
		for _, m := range e.cat.Methods() {
			if strings.Contains(strings.ToLower(m.FullName), needle) {
				methods = append(methods, m)
			}
		}
		if len(methods) == 0 {
			return fmt.Errorf("no rpc matches %q", *filter)
		}
	}
	for _, ref := range rest {
		m, err := e.cat.Lookup(ref)
		if err != nil {
			return err
		}
		methods = append(methods, m)
	}

	if *asJSON {
		out := make([]any, 0, len(methods))
		for _, m := range methods {
			out = append(out, contractShowJSON(m, lib, e.cat))
		}
		if len(out) == 1 {
			return emitJSON(out[0])
		}
		return emitJSON(map[string]any{"count": len(out), "contracts": out})
	}
	for i, m := range methods {
		if i > 0 {
			fmt.Println(strings.Repeat("-", 72))
		}
		generated := contract.ForCurated(m, lib, e.cat)
		fmt.Print(generated.Text())
		curated, hasCurated := lib.Get(m.FullName)
		if !hasCurated {
			fmt.Printf("\nNO CURATED CONTRACT\n  add one with: shrt contract init %s\n", contract.DomainOf(m))
			continue
		}
		fmt.Print("\n" + curated.Text(lib, m.FullName))
	}
	return nil
}

func contractShowJSON(m *catalog.Method, lib *contract.Library, cat *catalog.Catalog) map[string]any {
	curated, hasCurated := lib.Get(m.FullName)
	return map[string]any{
		"generated":          contract.ForCurated(m, lib, cat),
		"curated":            curated,
		"domain":             lib.Domain(m.FullName),
		"has_contract":       hasCurated,
		"inherited_failures": lib.InheritedFailures(m.FullName),
		"required_by":        lib.RequiredBy(m.FullName),
	}
}

func oneofChoices(lib *contract.Library, rpc string) []string {
	if lib == nil {
		return nil
	}
	c, _ := lib.Get(rpc)
	return contract.ArmedOneofMembers(c, "")
}
