package main

import (
	"encoding/json"

	"github.com/N4darae/shrt/catalog"
)

func unsentDefault(e *env) func(procedure, path string, v any) bool {
	return protoDefault(e, false)
}

func protoDefault(e *env, input bool) func(procedure, path string, v any) bool {
	cat := e.cat
	if cat == nil && e.cfg != nil {
		loaded, err := catalog.Load(e.cfg.Abs(e.cfg.Descriptor.File))
		if err != nil {
			return nil
		}
		cat = loaded
	}
	if cat == nil {
		return nil
	}
	return func(procedure, path string, v any) bool {
		m, err := cat.Lookup(procedure)
		if err != nil {
			return false
		}
		md := m.Output()
		if input {
			md = m.Input()
			if raw, err := json.Marshal(v); err == nil {
				_ = json.Unmarshal(raw, &v)
			}
		}
		return catalog.UnsentDefault(md, path, v)
	}
}
