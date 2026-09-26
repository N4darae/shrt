package main

import (
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func whichCodeAliases(code string, chains []*chain.Chain, observe func(string) []chain.Observation, lib *contract.Library) []string {
	if code == "" {
		return nil
	}
	responses := []any{}
	if observe != nil {
		for _, c := range chains {
			for _, o := range observe(c.Name) {
				if o.Reached && o.Response != nil {
					responses = append(responses, o.Response)
				}
			}
		}
	}
	seen := map[string]bool{strings.ToLower(code): true}
	out := []string{}
	add := func(v string) {
		if v != "" && !seen[strings.ToLower(v)] {
			seen[strings.ToLower(v)] = true
			out = append(out, v)
		}
	}
	for _, a := range chain.CodeAliases(code, responses) {
		add(a)
	}
	if lib != nil {
		for _, rpc := range lib.RPCs() {
			for _, f := range lib.AllFailures(rpc) {
				num := ""
				if f.Code != 0 {
					num = strconv.Itoa(f.Code)
				}
				switch {
				case num != "" && num == code:
					add(f.Reason)
				case f.Reason != "" && strings.EqualFold(f.Reason, code):
					add(num)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}
