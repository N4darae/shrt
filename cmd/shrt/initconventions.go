package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/transport"
	"gopkg.in/yaml.v3"
)

type observedConventions struct {
	envelopePath string
	envelopeOK   string
	itemPath     string
	login        string
}

func declareConventions(ctx context.Context, cfg *config.Config, cfgPath string) (string, error) {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", err
	}
	if mappingValue(doc.Content[0], "conventions") != nil {
		return "", nil
	}
	cat, err := catalog.Load(cfg.Abs(cfg.Descriptor.File))
	if err != nil {
		return "", nil
	}
	found := catalog.DetectEnvelope(cat)
	if len(found) == 0 {
		return "", nil
	}
	seen, err := observeEnvelopeOK(ctx, cfg, cat, found[0].Path)
	if err != nil {
		return err.Error(), nil
	}
	if items := catalog.DetectItemEnvelope(cat, seen.envelopePath); len(items) == 1 && items[0].SameMessage {
		seen.itemPath = items[0].Path
	}
	block := config.Conventions{EnvelopePath: seen.envelopePath, EnvelopeOK: seen.envelopeOK, ItemEnvelopePath: seen.itemPath}
	var value yaml.Node
	if err := value.Encode(block); err != nil {
		return "", err
	}
	top := doc.Content[0]
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "conventions"}
	at := len(top.Content)
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "auth" {
			at = i + 2
		}
	}
	top.Content = append(top.Content[:at], append([]*yaml.Node{key, &value}, top.Content[at:]...)...)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(4)
	if err := enc.Encode(&doc); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	if err := os.WriteFile(cfgPath, buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	cfg.Conventions.EnvelopePath = block.EnvelopePath
	cfg.Conventions.EnvelopeOK = block.EnvelopeOK
	cfg.Conventions.ItemEnvelopePath = block.ItemEnvelopePath
	line := fmt.Sprintf("write %s conventions: envelope_path %s, envelope_ok %s (read from one %s answer)",
		rel(cfg.Root, cfgPath), block.EnvelopePath, block.EnvelopeOK, seen.login)
	if block.ItemEnvelopePath != "" {
		line += ", item_envelope_path " + block.ItemEnvelopePath
	}
	fmt.Println(line)
	return "", nil
}

func observeEnvelopeOK(ctx context.Context, cfg *config.Config, cat *catalog.Catalog, path string) (*observedConventions, error) {
	if cfg.Auth == nil || strings.TrimSpace(cfg.Auth.Call) == "" {
		return nil, errors.New("no login rpc is declared")
	}
	m, err := cat.Lookup(cfg.Auth.Call)
	if err != nil {
		return nil, fmt.Errorf("auth.call: %w", err)
	}
	if !catalog.HasPath(catalog.DescribeMessage(m.Output()).Fields, chain.SplitPath(path)) {
		return nil, fmt.Errorf("the login answer does not carry %s", path)
	}
	resolved, err := chain.ResolveAuthBody(cfg.Auth.Body)
	if err != nil {
		return nil, fmt.Errorf("the login was not sent: %v", err)
	}
	body, err := json.Marshal(resolved)
	if err != nil {
		return nil, err
	}
	timeout := 5 * time.Second
	if d, err := time.ParseDuration(cfg.Target.Timeout); err == nil && d > 0 && d < timeout {
		timeout = d
	}
	client := transport.New(transport.Options{
		BaseURL:      cfg.Target.BaseURL,
		HostOverride: cfg.Target.HostOverride,
		Timeout:      timeout,
		Headers:      cfg.Target.Headers,
	})
	res, err := client.Do(ctx, &transport.Call{Procedure: m.Procedure(), Body: body})
	if err != nil {
		return nil, fmt.Errorf("the login to %s failed: %v", cfg.Target.BaseURL, err)
	}
	if res.Error != nil {
		return nil, fmt.Errorf("the login was refused: %v", res.Error)
	}
	answer := res.Body
	if canonical, err := cat.Canonicalize(m.Output(), answer); err == nil {
		answer = canonical
	}
	if cfg.Auth.TokenPath == "" || transport.EnvelopeCodeReader(cfg.Auth.TokenPath)(answer) == "" {
		return nil, fmt.Errorf("the login answer carried no token at %s", cfg.Auth.TokenPath)
	}
	ok := transport.EnvelopeCodeReader(path)(answer)
	if ok == "" {
		return nil, fmt.Errorf("the login answer carried no value at %s", path)
	}
	return &observedConventions{envelopePath: path, envelopeOK: ok, login: m.Name}, nil
}
