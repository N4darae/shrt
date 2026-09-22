package chain_test

import (
	"sync"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestConventionsSurviveConcurrentUse(t *testing.T) {
	t.Cleanup(func() {
		chain.ApplyConventions(nil, chain.DefaultEnvelopePath, chain.DefaultEnvelopeOK)
		chain.SetItemEnvelope("")
	})

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				chain.ApplyConventions([]string{"Fetch"}, "error.code", "OK")
				chain.SetItemEnvelope("results[].error.code")
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				_ = chain.EnvelopePath()
				_ = chain.EnvelopeOK()
				_ = chain.EnvelopeField()
				_ = chain.ItemEnvelope()
				_ = chain.IsEnvelopePath("error.code")
				_ = chain.IsReadOnlyCall("acme.v1.Service/FetchThing")
				_ = chain.ReadOnlyPrefixes()
			}
		}()
	}
	wg.Wait()
}

func TestReadOnlyPrefixesHandsBackACopy(t *testing.T) {
	t.Cleanup(func() { chain.SetReadOnlyPrefixes(nil) })
	chain.SetReadOnlyPrefixes([]string{"Fetch", "List"})

	got := chain.ReadOnlyPrefixes()
	got[0] = "Mutated"

	if again := chain.ReadOnlyPrefixes()[0]; again != "Fetch" {
		t.Fatalf("a caller who edits the returned slice must not rewrite the convention for the whole "+
			"process: got %q", again)
	}
}

func TestSetReadOnlyPrefixesWithNothingRestoresTheDefaults(t *testing.T) {
	t.Cleanup(func() { chain.SetReadOnlyPrefixes(nil) })
	chain.SetReadOnlyPrefixes([]string{"Peek"})

	chain.SetReadOnlyPrefixes(nil)

	if !chain.IsReadOnlyCall("acme.v1.Service/FetchThing") {
		t.Fatal("an empty list means 'use the defaults', not 'nothing is read-only'")
	}
}
