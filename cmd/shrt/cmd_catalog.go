package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
)

func init() {
	register(&command{name: "catalog", summary: "build, list and describe the RPC surface", run: runCatalog})
}

var catalogGroup = group{
	name: "catalog",
	subs: []subcommand{
		{"build", "rebuild the descriptor after a proto change"},
		{"ls", "list the RPC surface"},
		{"describe", "request and response schemas with proto doc comments"},
	},
}

func runCatalog(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return catalogGroup.missing()
	}
	if isHelpArg(args[0]) {
		catalogGroup.printHelp()
		return nil
	}
	switch args[0] {
	case "build":
		return catalogBuild(ctx, args[1:])
	case "ls", "list":
		return catalogList(args[1:])
	case "describe", "show":
		return catalogDescribe(args[1:])
	default:
		return catalogGroup.unknown(args[0])
	}
}

func catalogBuild(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("catalog build", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := loadEnv(false)
	if err != nil {
		return err
	}
	if err := buildDescriptor(ctx, e.cfg); err != nil {
		return err
	}
	cat, err := catalog.Load(e.cfg.Abs(e.cfg.Descriptor.File))
	if err != nil {
		return err
	}
	fmt.Printf("built %s: %d services, %d rpcs\n",
		e.cfg.Descriptor.File, len(cat.Services()), len(cat.Methods()))
	return nil
}

func catalogList(args []string) error {
	fs := flag.NewFlagSet("catalog ls", flag.ContinueOnError)
	filter := fs.String("filter", "", "case-insensitive substring filter on the rpc name")
	asJSON := fs.Bool("json", false, "emit JSON")
	services := fs.Bool("services", false, "list services only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	if *services {
		names := e.cat.Services()
		if *asJSON {
			return emitJSON(names)
		}
		for _, n := range names {
			fmt.Println(n)
		}
		return nil
	}

	type row struct {
		RPC       string `json:"rpc"`
		Procedure string `json:"procedure"`
		Input     string `json:"input"`
		Output    string `json:"output"`
		Streaming string `json:"streaming,omitempty"`
		Doc       string `json:"doc,omitempty"`
	}
	rows := []row{}
	for _, m := range e.cat.Methods() {
		if *filter != "" && !strings.Contains(strings.ToLower(m.FullName), strings.ToLower(*filter)) {
			continue
		}
		rows = append(rows, row{
			RPC:       m.FullName,
			Procedure: m.Procedure(),
			Input:     string(m.Input().FullName()),
			Output:    string(m.Output().FullName()),
			Streaming: m.StreamKind(),
			Doc:       m.Doc,
		})
	}
	if *asJSON {
		return emitJSON(rows)
	}
	streaming := 0
	for _, r := range rows {
		if r.Streaming == "" {
			fmt.Println(r.RPC)
			continue
		}
		streaming++
		fmt.Printf("%s  [%s — OUT OF SCOPE, shrt is unary-only]\n", r.RPC, r.Streaming)
	}
	fmt.Printf("\n%d rpc(s)", len(rows))
	if streaming > 0 {
		fmt.Printf(", %d of them streaming and not callable from a chain", streaming)
	}
	fmt.Println()
	return nil
}

func catalogDescribe(args []string) error {
	fs := flag.NewFlagSet("catalog describe", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("usage: shrt catalog describe <rpc>")
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	m, err := e.cat.Lookup(rest[0])
	if err != nil {
		return err
	}
	in := catalog.DescribeMessage(m.Input())
	out := catalog.DescribeMessage(m.Output())
	if *asJSON {
		return emitJSON(map[string]any{
			"rpc": m.FullName, "procedure": m.Procedure(), "doc": m.Doc,
			"file": m.File, "streaming": m.StreamKind(), "request": in, "response": out,
		})
	}
	fmt.Printf("%s\n  procedure: %s\n  file: %s\n", m.FullName, m.Procedure(), m.File)
	if m.Streaming() {
		fmt.Printf("  STREAMING: %s\n", m.StreamRefusal())
	}
	if m.Doc != "" {
		fmt.Printf("  doc: %s\n", m.Doc)
	}
	fmt.Printf("\nREQUEST %s", in.Text())
	fmt.Printf("\nRESPONSE %s", out.Text())
	return nil
}
