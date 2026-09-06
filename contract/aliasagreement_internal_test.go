package contract

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/N4darae/shrt/catalog/catalogtest"
)

func libraryFrom(t *testing.T, raw string) *Library {
	t.Helper()
	o := &Overlay{}
	if err := yaml.Unmarshal([]byte(raw), o); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	o.Domain = "demo"
	return NewLibrary([]*Overlay{o})
}

func aliasWarnings(issues []Issue) []Issue {
	out := []Issue{}
	for _, i := range issues {
		if strings.Contains(i.Message, "one chain, two instances") {
			out = append(out, i)
		}
	}
	return out
}

const disagreeingInstances = `apiVersion: shrt/contract/v1
domain: demo
rpcs:
    shrt.test.v1.ThingService/Create:
        summary: creates a thing
        required: []
        aliases:
            a: {note: one instance}
            b: {note: another instance}
        status: draft
    shrt.test.v1.ThingService/Fetch:
        summary: reads it back
        required: []
        fields:
            id:
                from: shrt.test.v1.ThingService/Create@a->id
        status: draft
    shrt.test.v1.PartnerService/FetchMine:
        summary: reads it back from the other side
        required: []
        needs: [shrt.test.v1.ThingService/Fetch]
        fields:
            id:
                from: shrt.test.v1.ThingService/Create@b->id
        status: draft
`

func TestLintFlagsTwoInstancesOfOneProducerAcrossADependencyEdge(t *testing.T) {
	cat := catalogtest.New()
	got := aliasWarnings(LintLibrary(libraryFrom(t, disagreeingInstances), cat))
	if len(got) != 1 {
		t.Fatalf("got %d alias-agreement warnings, want 1: %+v", len(got), got)
	}
	if got[0].RPC != "shrt.test.v1.PartnerService/FetchMine" {
		t.Fatalf("warned on %s, want the dependent rpc", got[0].RPC)
	}
}

func TestLintAcceptsTwoInstancesWithNoDependencyBetweenThem(t *testing.T) {
	cat := catalogtest.New()
	raw := strings.Replace(disagreeingInstances,
		"        needs: [shrt.test.v1.ThingService/Fetch]\n", "", 1)
	if got := aliasWarnings(LintLibrary(libraryFrom(t, raw), cat)); len(got) != 0 {
		t.Fatalf("warned on unrelated rpcs that merely share a producer: %+v", got)
	}
}

func TestLintAcceptsTwoInstancesAgreeingOnOne(t *testing.T) {
	cat := catalogtest.New()
	raw := strings.Replace(disagreeingInstances, "Create@b->id", "Create@a->id", 1)
	if got := aliasWarnings(LintLibrary(libraryFrom(t, raw), cat)); len(got) != 0 {
		t.Fatalf("warned when both sides name the same instance: %+v", got)
	}
}
