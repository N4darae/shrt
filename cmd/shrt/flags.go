package main

import (
	"flag"
	"fmt"
	"io"
)

func setUsage(fs *flag.FlagSet, usage, tail string) {
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), usage)
		printFlags(fs.Output(), fs)
		fmt.Fprint(fs.Output(), tail)
	}
}

func printFlags(w io.Writer, fs *flag.FlagSet) {
	heads, usages := []string{}, []string{}
	width := 0
	fs.VisitAll(func(f *flag.Flag) {
		name, usage := flag.UnquoteUsage(f)
		head := "-" + f.Name
		if name != "" {
			head += " " + name
		}
		switch f.DefValue {
		case "", "false", "0", "[]":
		default:
			usage += fmt.Sprintf(" (default %s)", f.DefValue)
		}
		heads, usages = append(heads, head), append(usages, usage)
		width = max(width, len(head))
	})
	for i := range heads {
		fmt.Fprintf(w, "  %-*s  %s\n", width, heads[i], usages[i])
	}
}

func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	positional := []string{}
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}
