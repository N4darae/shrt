package main

import "github.com/N4darae/shrt/catalog"

func unsentDefault(cat *catalog.Catalog) func(procedure, path string, v any) bool {
	if cat == nil {
		return nil
	}
	return func(procedure, path string, v any) bool {
		m, err := cat.Lookup(procedure)
		if err != nil {
			return false
		}
		return catalog.UnsentDefault(m.Output(), path, v)
	}
}
