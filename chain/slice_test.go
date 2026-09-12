package chain_test

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

var refFinder = regexp.MustCompile(`\$\{([^}]+)\}`)

func TestIsReadOnlyCallTreatsPreviewAsRead(t *testing.T) {
	for _, call := range []string{"PreviewDealSurcharge", "svc.v1.Service/PreviewMovementFee"} {
		if !chain.IsReadOnlyCall(call) {
			t.Errorf("IsReadOnlyCall(%q) = false, want true", call)
		}
	}
	for _, call := range []string{"CreateDeal", "svc.v1.Service/ApproveOffset"} {
		if chain.IsReadOnlyCall(call) {
			t.Errorf("IsReadOnlyCall(%q) = true, want false", call)
		}
	}
}

func corpus(t *testing.T) []*chain.Chain {
	t.Helper()
	files, err := filepath.Glob("../../.shrt/chains/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no committed chains to slice")
	}
	out := make([]*chain.Chain, 0, len(files))
	for _, p := range files {
		c, err := chain.LoadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(p), err)
		}
		out = append(out, c)
	}
	return out
}

func refsOf(s *chain.Step) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(v any) {
		text, ok := v.(string)
		if !ok {
			return
		}
		for _, m := range refFinder.FindAllStringSubmatch(text, -1) {
			r := strings.TrimSpace(m[1])
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			add(t)
		case map[string]any:
			for _, item := range t {
				walk(item)
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	walk(s.Body)
	for _, v := range s.Headers {
		add(v)
	}
	for _, e := range s.Expect {
		walk(e.Equals)
		walk(e.NotEqual)
		add(e.Contains)
	}
	return out
}

func resolvable(c *chain.Chain, at int, ref string) bool {
	head, rest, hasRest := strings.Cut(ref, ".")
	switch head {
	case "uuid", "now", "nowunix", "env":
		return true
	case "vars":
		name, _, _ := strings.Cut(rest, ".")
		_, ok := c.Vars[name]
		return ok || name == ""
	case "exports":
		name, _, _ := strings.Cut(rest, ".")
		return exportedBefore(c, at, name)
	case "steps":
		id, _, _ := strings.Cut(rest, ".")
		return stepBefore(c, at, id)
	}
	if !hasRest && exportedBefore(c, at, head) {
		return true
	}
	return stepBefore(c, at, head)
}

func exportedBefore(c *chain.Chain, at int, name string) bool {
	for i := 0; i < at && i < len(c.Steps); i++ {
		if _, ok := c.Steps[i].Export[name]; ok {
			return true
		}
	}
	return false
}

func stepBefore(c *chain.Chain, at int, id string) bool {
	for i := 0; i < at && i < len(c.Steps); i++ {
		if c.Steps[i].ID == id {
			return true
		}
	}
	return false
}

func TestSliceOfEveryStepOfEveryChainLeavesNoDanglingReference(t *testing.T) {
	chains := corpus(t)
	slices := 0
	for _, c := range chains {
		for _, target := range c.Steps {
			res, err := chain.Slice(c, target.ID, chain.SliceOptions{})
			if err != nil {
				t.Fatalf("%s/%s: %v", c.Name, target.ID, err)
			}
			slices++
			out := res.Chain
			for at, s := range out.Steps {
				for _, ref := range refsOf(s) {
					if resolvable(out, at, ref) {
						continue
					}
					sourceAt := indexOf(c, s.ID)
					if !resolvable(c, sourceAt, ref) {
						continue
					}
					t.Errorf("%s sliced for %s: step %s references ${%s}, which resolves in the source chain but not in the slice",
						c.Name, target.ID, s.ID, ref)
				}
			}
		}
	}
	if slices == 0 {
		t.Fatal("no slice was computed, so this test proved nothing")
	}
	t.Logf("%d chains, %d slices checked", len(chains), slices)
}

func indexOf(c *chain.Chain, id string) int {
	for i, s := range c.Steps {
		if s.ID == id {
			return i
		}
	}
	return len(c.Steps)
}

func TestSliceKeepsOriginalRelativeOrder(t *testing.T) {
	for _, c := range corpus(t) {
		for _, target := range c.Steps {
			res, err := chain.Slice(c, target.ID, chain.SliceOptions{})
			if err != nil {
				t.Fatalf("%s/%s: %v", c.Name, target.ID, err)
			}
			prev := -1
			for i, k := range res.Kept {
				if k.Index <= prev {
					t.Fatalf("%s sliced for %s: kept index %d follows %d", c.Name, target.ID, k.Index, prev)
				}
				prev = k.Index
				if c.Steps[k.Index].ID != k.ID {
					t.Fatalf("%s sliced for %s: kept %q claims index %d, which is %q", c.Name, target.ID, k.ID, k.Index, c.Steps[k.Index].ID)
				}
				if res.Chain.Steps[i].ID != k.ID {
					t.Fatalf("%s sliced for %s: emitted step %d is %q, report says %q", c.Name, target.ID, i, res.Chain.Steps[i].ID, k.ID)
				}
			}
			if len(res.Kept) == 0 || res.Kept[len(res.Kept)-1].ID != target.ID {
				t.Fatalf("%s sliced for %s: the target must be the last kept step", c.Name, target.ID)
			}
		}
	}
}

func TestSliceIsDeterministic(t *testing.T) {
	for _, c := range corpus(t) {
		for _, target := range c.Steps {
			a, err := chain.Slice(c, target.ID, chain.SliceOptions{})
			if err != nil {
				t.Fatal(err)
			}
			b, err := chain.Slice(c, target.ID, chain.SliceOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(a.Kept) != len(b.Kept) {
				t.Fatalf("%s/%s: %d kept then %d kept", c.Name, target.ID, len(a.Kept), len(b.Kept))
			}
			for i := range a.Kept {
				if a.Kept[i] != b.Kept[i] {
					t.Fatalf("%s/%s: keep %d differs between runs: %+v vs %+v", c.Name, target.ID, i, a.Kept[i], b.Kept[i])
				}
			}
		}
	}
}

func TestSliceWarnsWhenReferenceClosureCollapses(t *testing.T) {
	chains := corpus(t)
	collapsed := 0
	for _, c := range chains {
		if len(c.Steps) < 10 {
			continue
		}
		last := c.Steps[len(c.Steps)-1]
		res, err := chain.Slice(c, last.ID, chain.SliceOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Kept) != 1 {
			continue
		}
		collapsed++
		if len(res.DroppedWrites) == 0 {
			continue
		}
		if !res.UnderIncluded {
			t.Errorf("%s sliced for its last step kept 1 of %d steps and dropped %d write(s) without warning",
				c.Name, res.Total, len(res.DroppedWrites))
		}
	}
	if collapsed == 0 {
		t.Fatal("no chain collapsed, so this test proved nothing about the warning")
	}
	t.Logf("%d chains of %d collapse to a single step under reference closure alone", collapsed, len(chains))
}

func TestSliceWarningOnTheThreeLargestCollapsingChains(t *testing.T) {
	want := map[string]bool{
		"dealing-approve-obligation-guards":           true,
		"dealing-approveobligation-declared-failures": true,
		"readonly-fetch-sweep":                        false,
	}
	seen := map[string]bool{}
	for _, c := range corpus(t) {
		warn, ok := want[c.Name]
		if !ok {
			continue
		}
		seen[c.Name] = true
		last := c.Steps[len(c.Steps)-1]
		res, err := chain.Slice(c, last.ID, chain.SliceOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Kept) != 1 {
			t.Fatalf("%s: reference closure kept %d steps, this test is about the collapse to 1", c.Name, len(res.Kept))
		}
		if res.UnderIncluded != warn {
			t.Errorf("%s: 1 of %d steps kept, %d dropped write(s), warning=%v, want %v",
				c.Name, res.Total, len(res.DroppedWrites), res.UnderIncluded, warn)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("chain %s is missing from the corpus, so the warning was never exercised on it", name)
		}
	}
}

func sliceFixture() *chain.Chain {
	c := &chain.Chain{
		Name: "fixture",
		Vars: map[string]any{"tag": "T1", "unused": "U"},
		Steps: []*chain.Step{
			{ID: "create_book", Call: "BookService/CreateBook", Body: map[string]any{"code": "B-${vars.tag}"},
				Export: map[string]string{"book": "id_book"}},
			{ID: "set_limit", Call: "LimitActionService/SetCounterpartyCreditLimit", Body: map[string]any{"amount": "10"}},
			{ID: "create_deal", Call: "DealService/CreateDeal", Body: map[string]any{"id_book": "${book}"}},
			{ID: "noise", Call: "DealService/CreateDeal", Body: map[string]any{"id_book": "${create_book.id_book}"}},
			{ID: "fetch_deal", Call: "DealService/FetchDeal", Body: map[string]any{"id_deal": "${create_deal.results.0.id_deal}"},
				Expect: []chain.Expectation{{Path: "id_book", Equals: "${exports.book}"}}},
		},
	}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func TestSliceFollowsExportsAndExpectationReferences(t *testing.T) {
	res, err := chain.Slice(sliceFixture(), "fetch_deal", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := keptIDs(res)
	want := []string{"create_book", "create_deal", "fetch_deal"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("kept %v, want %v", got, want)
	}
	if _, ok := res.Chain.Vars["unused"]; ok {
		t.Error("a var no kept step references must be dropped")
	}
	if _, ok := res.Chain.Vars["tag"]; !ok {
		t.Error("a var a kept step references must survive")
	}
	if got, want := res.Kept[2].Reason, "target"; got != want {
		t.Fatalf("target reason %q, want %q", got, want)
	}
	if !strings.HasPrefix(res.Kept[1].Reason, "produces ${create_deal.results.0.id_deal} used by fetch_deal") {
		t.Fatalf("producer reason is %q", res.Kept[1].Reason)
	}
}

func TestSliceIncludesContractPrerequisitesAndReportsUnmetOnes(t *testing.T) {
	prereqs := func(rpc string) []chain.Prereq {
		switch rpc {
		case "DealService/CreateDeal":
			return []chain.Prereq{
				{RPC: "LimitActionService/SetCounterpartyCreditLimit", Edge: "before"},
				{RPC: "AssetService/CreateAsset", Edge: "needs"},
			}
		}
		return nil
	}
	res, err := chain.Slice(sliceFixture(), "fetch_deal", chain.SliceOptions{Prereqs: prereqs})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(keptIDs(res), ",")
	if want := "create_book,set_limit,create_deal,fetch_deal"; got != want {
		t.Fatalf("kept %s, want %s", got, want)
	}
	for _, k := range res.Kept {
		if k.ID == "set_limit" && !strings.HasPrefix(k.Reason, "contract needs LimitActionService/SetCounterpartyCreditLimit") {
			t.Fatalf("set_limit reason is %q", k.Reason)
		}
	}
	if len(res.Unmet) != 1 || res.Unmet[0].RPC != "AssetService/CreateAsset" {
		t.Fatalf("an edge naming an rpc no earlier step calls must be reported, got %+v", res.Unmet)
	}
}

func TestSlicePinDropsValueOnlyProducersAndKeepsSideEffectOnes(t *testing.T) {
	c := sliceFixture()
	values := map[string]any{
		"create_deal.results.0.id_deal": "DEAL-9",
		"exports.book":                  "BOOK-9",
		"book":                          "BOOK-9",
	}
	opts := chain.SliceOptions{
		Mode:  chain.SliceModePin,
		RunID: "20260911T140222Z-8b148b22",
		Value: func(ref string) (any, bool) {
			v, ok := values[ref]
			return v, ok
		},
		Prereqs: func(rpc string) []chain.Prereq {
			if rpc == "DealService/FetchDeal" {
				return []chain.Prereq{{RPC: "LimitActionService/SetCounterpartyCreditLimit", Edge: "before"}}
			}
			return nil
		},
	}
	pinned, err := chain.Slice(c, "fetch_deal", opts)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := chain.Slice(c, "fetch_deal", chain.SliceOptions{Prereqs: opts.Prereqs})
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned.Kept) >= len(closure.Kept) {
		t.Fatalf("pin kept %d steps, closure kept %d: pinning must drop producers whose only contribution is a value",
			len(pinned.Kept), len(closure.Kept))
	}
	if got := strings.Join(keptIDs(pinned), ","); got != "set_limit,fetch_deal" {
		t.Fatalf("kept %s, want set_limit,fetch_deal: a step kept for a side effect can never be pinned away", got)
	}
	if len(pinned.Pins) != 2 {
		t.Fatalf("want two pins, got %+v", pinned.Pins)
	}
	for _, p := range pinned.Pins {
		if _, ok := pinned.Chain.Vars[p.Var]; !ok {
			t.Errorf("pinned value %s is not in vars", p.Var)
		}
	}
	dropped := map[string]bool{"create_book": true, "create_deal": true, "noise": true}
	for at, s := range pinned.Chain.Steps {
		for _, ref := range refsOf(s) {
			head, _, _ := strings.Cut(ref, ".")
			if dropped[head] {
				t.Errorf("step %s still references ${%s}, whose producer was pinned away", s.ID, ref)
			}
			if !resolvable(pinned.Chain, at, ref) {
				t.Errorf("step %s references ${%s}, which resolves to nothing in the pinned slice", s.ID, ref)
			}
		}
	}
	if !strings.Contains(pinned.Chain.Description, "20260911T140222Z-8b148b22") {
		t.Error("a pinned slice must record the run its values came from, in the description")
	}
}

func TestSlicePinRefusesWithoutValues(t *testing.T) {
	_, err := chain.Slice(sliceFixture(), "fetch_deal", chain.SliceOptions{Mode: chain.SliceModePin})
	if err == nil {
		t.Fatal("pin mode without a run record must fail loudly, not degrade to closure")
	}
	if !strings.Contains(err.Error(), "run record") {
		t.Fatalf("the error must say what is missing, got: %v", err)
	}
}

func TestSliceUnknownStepNamesTheValidIDs(t *testing.T) {
	_, err := chain.Slice(sliceFixture(), "no_such_step", chain.SliceOptions{})
	if err == nil {
		t.Fatal("an unknown step id must be an error")
	}
	for _, want := range []string{"create_book", "fetch_deal"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error must list the valid ids, got: %v", err)
		}
	}
}

func TestSliceDescriptionRecordsProvenance(t *testing.T) {
	res, err := chain.Slice(sliceFixture(), "fetch_deal", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fixture", "fetch_deal", "closure", "3 of 5 steps"} {
		if !strings.Contains(res.Chain.Description, want) {
			t.Errorf("description must record %q:\n%s", want, res.Chain.Description)
		}
	}
	if res.Chain.Name != "fixture-slice-fetch_deal" {
		t.Fatalf("default slice name is %q", res.Chain.Name)
	}
}

func TestCompareVerdictsReadsBothTheCodeAndEachExpectation(t *testing.T) {
	source := chain.Verdict{Step: "a", Status: "passed", ErrorCode: "failed_precondition", Expect: []chain.ExpectResult{
		{Path: "error.details.0.app_code", Rule: "equals", Passed: true},
	}}
	same := source
	if diffs := chain.CompareVerdicts(source, same); len(diffs) != 0 {
		t.Fatalf("identical verdicts must reproduce, got %v", diffs)
	}
	other := chain.Verdict{Step: "a", Status: "passed", ErrorCode: "not_found", Expect: source.Expect}
	if diffs := chain.CompareVerdicts(source, other); len(diffs) == 0 {
		t.Fatal("a different error.code is not a reproduction")
	}
	flipped := chain.Verdict{Step: "a", Status: "failed", ErrorCode: "failed_precondition", Expect: []chain.ExpectResult{
		{Path: "error.details.0.app_code", Rule: "equals", Passed: false},
	}}
	diffs := chain.CompareVerdicts(source, flipped)
	if len(diffs) != 2 {
		t.Fatalf("a flipped expectation and a flipped status are two differences, got %v", diffs)
	}
	if !strings.Contains(fmt.Sprint(diffs), "app_code") {
		t.Fatalf("the difference must name the expectation, got %v", diffs)
	}
}

func keptIDs(res *chain.SliceResult) []string {
	out := make([]string, 0, len(res.Kept))
	for _, k := range res.Kept {
		out = append(out, k.ID)
	}
	return out
}
