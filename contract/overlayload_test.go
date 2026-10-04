package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

const realCreateOrder = `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records a pending order
        required: [lines]
        status: draft
`

const impostorCreateOrder = `apiVersion: shrt/contract/v1
domain: impostor
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: IMPOSTOR
        required: [NONE]
        status: draft
`

func writeOverlay(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAnRPCDefinedInTwoOverlaysIsAnErrorNamingBothFiles(t *testing.T) {
	dir := t.TempDir()
	writeOverlay(t, dir, "orders.yaml", realCreateOrder)
	writeOverlay(t, dir, "zz.yaml", impostorCreateOrder)
	_, broken, err := contract.LoadLibraryIn(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(broken) != 1 {
		t.Fatalf("zz.yaml redefines CreateOrder and silently wins; want one error, got %v", broken)
	}
	msg := broken[0].Error()
	for _, want := range []string{"orders.yaml", "zz.yaml", "shop.orders.v1.OrderService/CreateOrder"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the error must name %q: %s", want, msg)
		}
	}
}

func TestASecondDocumentThatDoesNotParseIsAnError(t *testing.T) {
	_, err := contract.LoadOverlayBytes("orders.yaml", []byte(realCreateOrder+"---\nrpcs: [unclosed\n"))
	if err == nil || !strings.Contains(err.Error(), "more than one YAML document") {
		t.Fatalf("a broken document after '---' was silently dropped, got %v", err)
	}
	if _, err := contract.LoadOverlayBytes("orders.yaml", []byte(realCreateOrder+"---\n")); err != nil {
		t.Fatalf("a trailing empty document carries nothing, got %v", err)
	}
}

func TestOverlaysInASubdirectoryAreListedAsNotLoaded(t *testing.T) {
	dir := t.TempDir()
	writeOverlay(t, dir, "orders.yaml", realCreateOrder)
	writeOverlay(t, dir, "sub/broken.yaml", "rpcs: [unclosed\n")
	writeOverlay(t, dir, "sub/notes.txt", "not an overlay\n")
	got, err := contract.IgnoredOverlayFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != filepath.Join("sub", "broken.yaml") {
		t.Fatalf("want sub/broken.yaml listed as not loaded, got %v", got)
	}
}
