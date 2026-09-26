package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

const stampedOverlay = `apiVersion: shrt/contract/v1
domain: items
rpcs:
    shrt.stamped.v1.ItemService/CreateItem:
        summary: adds an item
        required: [name]
        fields:
            name:
                value: widget
        status: draft
    shrt.stamped.v1.ItemService/GetItem:
        summary: reads an item
        required: [id_item]
        fields:
            id_item:
                from: shrt.stamped.v1.ItemService/CreateItem->item.id_item
        status: draft
`

func TestPlanAssertsAReadStampEqualsTheOneItsCreatorReceived(t *testing.T) {
	path, ok := chain.EnvelopePath(), chain.EnvelopeOK()
	contract.ApplyConventions(nil, "status.code", "SUCCESS")
	t.Cleanup(func() { contract.ApplyConventions(nil, path, ok) })
	p, err := contract.BuildPlan("shrt.stamped.v1.ItemService/GetItem", libraryFrom(t, stampedOverlay), catalogtest.Stamped(), "stamped")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.YAML()
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	get := text[strings.Index(text, "- id: get_item"):]
	if next := strings.Index(get[1:], "- id: "); next >= 0 {
		get = get[:next+1]
	}
	if !strings.Contains(get, "- path: item.created_at\n          equals: ${create_item.item.created_at}") {
		t.Fatalf("a read returns the stored stamp, so the plan asserts it equals create_item's:\n%s", text)
	}
	if strings.Contains(get, "- path: item.created_at\n          within:") {
		t.Fatalf("a read's created_at was not stamped by the read, so no clock window:\n%s", text)
	}
	create := text[strings.Index(text, "- id: create_item"):strings.Index(text, "- id: get_item")]
	if !strings.Contains(create, "- path: item.created_at\n          within:\n            of: ${nowunix}") {
		t.Fatalf("the creating call is asserted against the clock:\n%s", text)
	}
	notes := strings.Join(p.Notes, "\n")
	if strings.Contains(notes, "step get_item: item.created_at is asserted within") {
		t.Fatalf("the read's note must not say it was stamped by this call: %s", notes)
	}
}
