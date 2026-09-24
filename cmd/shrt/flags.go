package main

import (
	"flag"
	"fmt"
)

func setUsage(fs *flag.FlagSet, usage, tail string) {
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), usage)
		fs.PrintDefaults()
		fmt.Fprint(fs.Output(), tail)
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
