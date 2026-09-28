package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/store"
)

type env struct {
	cfg   *config.Config
	cat   *catalog.Catalog
	store *store.Store
	lib   *contract.Library
	libOK bool
}

func loadEnv(withCatalog bool) (*env, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(wd)
	if err != nil {
		return nil, configLoadError(wd, err)
	}
	contract.ApplyConventions(cfg.Conventions.ReadOnlyPrefixes, cfg.Conventions.EnvelopePath, cfg.Conventions.EnvelopeOK)
	chain.ApplyItemEnvelope(cfg.Conventions.ItemEnvelopePath)
	chain.ApplyCodeFields(cfg.Conventions.CodeFields)
	e := &env{
		cfg:   cfg,
		store: store.New(cfg.Abs(cfg.Paths.Runs), cfg.Abs(cfg.Paths.SafeSpots)),
	}
	e.store.Notes = os.Stderr
	if !withCatalog {
		return e, nil
	}
	path := cfg.Abs(cfg.Descriptor.File)
	cat, err := catalog.Load(path)
	if err != nil {
		return nil, fmt.Errorf("%w\nrun 'shrt catalog build' to regenerate it", err)
	}
	e.cat = cat
	return e, nil
}

const brokenConfigAdvice = "fix that line in the file. Plain 'shrt init' would not help: it keeps an existing config and " +
	"stops on this same error. 'shrt init -force-config' rebuilds it from defaults, discarding your auth, " +
	"conventions and volatile paths"

func configLoadError(wd string, err error) error {
	root, derr := config.Discover(wd)
	if derr != nil {
		return fmt.Errorf("%w\nrun 'shrt init' first", err)
	}
	return fmt.Errorf("%s exists but does not parse, so nothing was read from it: %w\n"+
		"%s", filepath.Join(root, config.DirName, config.FileName), err, brokenConfigAdvice)
}

func (e *env) effectsOf(call string) contract.Effects {
	if c := e.contractOf(call); c != nil {
		return c.Effects
	}
	return nil
}

func (e *env) contractOf(call string) *contract.RPCContract {
	if e == nil || e.cat == nil || !e.libOK && e.cfg == nil {
		return nil
	}
	if !e.libOK {
		e.libOK = true
		e.lib, _, _ = contract.LoadLibraryIn(e.contractsDir(), e.cat)
	}
	m, err := e.cat.Lookup(call)
	if err != nil {
		return nil
	}
	c, _ := e.lib.Get(m.FullName)
	return c
}

func (e *env) chainsDir() string { return e.cfg.Abs(e.cfg.Paths.Chains) }

func (e *env) targetURL() string {
	return strings.TrimRight(strings.TrimSpace(e.cfg.Target.BaseURL), "/")
}

func (e *env) otherTarget(recorded string) bool {
	now := e.targetURL()
	return recorded != "" && now != "" && !config.SameTarget(recorded, now)
}

func (e *env) knownChain(name string) error {
	if strings.ContainsAny(name, "/\\") || e.store.HasProposal(name) {
		return nil
	}
	known := chain.Names(e.chainsDir())
	for _, n := range known {
		if n == name {
			return nil
		}
	}
	if _, err := chain.Resolve(e.chainsDir(), name); err == nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(e.cfg.Abs(e.cfg.Paths.Runs), name)); err == nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(e.cfg.Abs(e.cfg.Paths.SafeSpots), name+".json")); err == nil {
		return nil
	}
	if entries, err := os.ReadDir(e.cfg.Abs(e.cfg.Paths.Runs)); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				known = append(known, entry.Name())
			}
		}
	}
	return fmt.Errorf("chain %q not found: no chain file in %s and no run, safe spot or proposal recorded for it%s",
		name, e.chainsDir(), chain.DidYouMean(name, known))
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func buildDescriptor(ctx context.Context, cfg *config.Config) error {
	src := cfg.Descriptor.Source
	if src == "" {
		return fmt.Errorf("descriptor.source is not set in %s/%s", config.DirName, config.FileName)
	}
	return catalog.Build(ctx, catalog.BuildSpec{
		Input:  cfg.Abs(src),
		Output: cfg.Abs(cfg.Descriptor.File),
		Binary: cfg.Descriptor.Binary,
	})
}
