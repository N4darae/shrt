package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

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
}

func loadEnv(withCatalog bool) (*env, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(wd)
	if err != nil {
		return nil, fmt.Errorf("%w\nrun 'shrt init' first", err)
	}
	contract.ApplyConventions(cfg.Conventions.ReadOnlyPrefixes, cfg.Conventions.EnvelopePath, cfg.Conventions.EnvelopeOK)
	chain.ApplyItemEnvelope(cfg.Conventions.ItemEnvelopePath)
	chain.ApplyCodeFields(cfg.Conventions.CodeFields)
	e := &env{
		cfg:   cfg,
		store: store.New(cfg.Abs(cfg.Paths.Runs), cfg.Abs(cfg.Paths.SafeSpots)),
	}
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

func (e *env) chainsDir() string { return e.cfg.Abs(e.cfg.Paths.Chains) }

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
