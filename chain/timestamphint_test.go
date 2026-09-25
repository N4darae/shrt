package chain_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func stampedChain(t *testing.T, raw string) *chain.Chain {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stamped.yaml")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func timestampHints(t *testing.T, c *chain.Chain) string {
	t.Helper()
	envPath, ok := chain.EnvelopePath(), chain.EnvelopeOK()
	chain.ApplyConventions(nil, "status.code", "SUCCESS")
	t.Cleanup(func() { chain.ApplyConventions(nil, envPath, ok) })
	out := []string{}
	for _, i := range chain.LintWith(c, catalogtest.Stamped(), chain.LintOptions{Hints: true}) {
		if i.Kind == chain.KindUnassertedTimestamp {
			out = append(out, i.Step+" "+i.Message)
		}
	}
	return strings.Join(out, "\n")
}

const stampedFlow = `apiVersion: shrt/v1
name: stamped
steps:
    - id: create_item
      call: ItemService/CreateItem
      body: {name: widget}
      expect:
        - path: status.code
          equals: SUCCESS
        - path: item.expires_at
          gte: ${nowunix}
    - id: get_item
      call: ItemService/GetItem
      body: {id_item: "${create_item.item.id_item}"}
      expect:
        - path: status.code
          equals: SUCCESS
        - path: item.expires_at
          gte: ${nowunix}
        - path: item.updated_at
          gte: ${nowunix-300}
`

func TestTimestampHintSuggestsANowWindowForACreationStamp(t *testing.T) {
	got := timestampHints(t, stampedChain(t, stampedFlow))
	if !strings.Contains(got, `create_item timestamp item.created_at unasserted; expect within: {of: "${nowunix}", by: 300}`) {
		t.Fatalf("a creation stamp is now, not an hour ahead: %q", got)
	}
	if strings.Contains(got, "nowunix+3600") {
		t.Fatalf("no expiry field is unasserted, so no expiry template: %q", got)
	}
}

func TestTimestampHintOnAReadSuggestsTheStampTheCreatorReceived(t *testing.T) {
	got := timestampHints(t, stampedChain(t, stampedFlow))
	if !strings.Contains(got, "get_item timestamp item.created_at unasserted; expect equals: ${create_item.item.created_at}") {
		t.Fatalf("a read returns the stored stamp, so it equals what create_item received: %q", got)
	}
}

func TestTimestampHintSkipsARefusalAssertingTheMessageAbsent(t *testing.T) {
	got := timestampHints(t, stampedChain(t, `apiVersion: shrt/v1
name: refused
steps:
    - id: create_item
      call: ItemService/CreateItem
      body: {name: widget}
      expect:
        - path: status.code
          equals: SUCCESS
        - path: item.created_at
          within: {of: "${nowunix}", by: 300}
        - path: item.updated_at
          within: {of: "${nowunix}", by: 300}
        - path: item.expires_at
          gte: ${nowunix}
    - id: create_item_same_name
      call: ItemService/CreateItem
      body: {name: widget}
      expect:
        - path: status.code
          not_equal: SUCCESS
        - path: item
          exists: false
`))
	if got != "" {
		t.Fatalf("a step expected to be refused, asserting item absent, has no timestamp to assert: %q", got)
	}
}

func TestTimestampHintKeepsTheExpiryTemplateForAnExpiry(t *testing.T) {
	got := timestampHints(t, stampedChain(t, `apiVersion: shrt/v1
name: expiry
steps:
    - id: create_item
      call: ItemService/CreateItem
      body: {name: widget}
      expect:
        - path: status.code
          equals: SUCCESS
        - path: item.created_at
          within: {of: "${nowunix}", by: 300}
        - path: item.updated_at
          within: {of: "${nowunix}", by: 300}
`))
	if !strings.Contains(got, `create_item timestamp item.expires_at unasserted; expect within: {of: "${nowunix+3600}", by: 5}`) {
		t.Fatalf("an expiry keeps the lifetime template: %q", got)
	}
}
