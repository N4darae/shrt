package chain_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"gopkg.in/yaml.v3"
)

type scannedStep struct {
	ID     string `yaml:"id"`
	Call   string `yaml:"call"`
	Expect []struct {
		Path   string `yaml:"path"`
		Equals any    `yaml:"equals"`
	} `yaml:"expect"`
}

type scannedChain struct {
	Name  string        `yaml:"name"`
	Steps []scannedStep `yaml:"steps"`
}

type pair struct{ chain, step string }

type scan struct {
	steps   int
	byRPC   map[string]map[pair]bool
	byCode  map[string]map[pair]bool
	rpcs    []string
	codes   []string
	rawPath map[string]bool
}

func scanCorpus(t *testing.T) *scan {
	t.Helper()
	files, err := filepath.Glob("../../.shrt/chains/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no committed chains to scan")
	}
	sort.Strings(files)
	s := &scan{byRPC: map[string]map[pair]bool{}, byCode: map[string]map[pair]bool{}, rawPath: map[string]bool{}}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		doc := scannedChain{}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", filepath.Base(path), err)
		}
		name := doc.Name
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		for i, st := range doc.Steps {
			s.steps++
			if st.ID == "" {
				t.Fatalf("%s step %d has no id: this scan mirrors the corpus only while every step names its own id",
					filepath.Base(path), i+1)
			}
			p := pair{name, st.ID}
			add(s.byRPC, st.Call, p)
			for _, e := range st.Expect {
				s.rawPath[e.Path] = true
				if e.Equals == nil || !scanIsCodePath(e.Path) {
					continue
				}
				add(s.byCode, fmt.Sprintf("%v", e.Equals), p)
			}
		}
	}
	s.rpcs = keysOf(s.byRPC)
	s.codes = keysOf(s.byCode)
	return s
}

func scanIsCodePath(path string) bool {
	segs := []string{}
	for _, seg := range strings.Split(strings.NewReplacer("[", ".", "]", "").Replace(path), ".") {
		if seg != "" {
			segs = append(segs, seg)
		}
	}
	if len(segs) == 0 {
		return false
	}
	last := segs[len(segs)-1]
	if last == "app_code" || last == "reason" {
		return true
	}
	return last == "code" && len(segs) >= 2 && segs[len(segs)-2] == "error"
}

func add(m map[string]map[pair]bool, key string, p pair) {
	if m[key] == nil {
		m[key] = map[pair]bool{}
	}
	m[key][p] = true
}

func keysOf(m map[string]map[pair]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func pairsOf(hits []chain.WhichChain) map[pair]bool {
	out := map[pair]bool{}
	for _, h := range hits {
		for _, m := range h.Matches {
			out[pair{h.Chain, m.Step}] = true
		}
	}
	return out
}

func diffPairs(want, got map[pair]bool) (missing, extra []string) {
	for p := range want {
		if !got[p] {
			missing = append(missing, p.chain+"/"+p.step)
		}
	}
	for p := range got {
		if !want[p] {
			extra = append(extra, p.chain+"/"+p.step)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func TestWhichFindsEveryPairAStraightforwardScanFinds(t *testing.T) {
	chains := corpus(t)
	s := scanCorpus(t)

	loaded := 0
	for _, c := range chains {
		loaded += len(c.Steps)
	}
	if loaded != s.steps {
		t.Fatalf("the scan saw %d steps and the loader saw %d: the scan is no longer looking at the same corpus", s.steps, loaded)
	}
	if len(s.rpcs) == 0 || len(s.codes) == 0 {
		t.Fatalf("the scan found %d rpc(s) and %d code(s): nothing would be compared", len(s.rpcs), len(s.codes))
	}

	for _, rpc := range s.rpcs {
		got := pairsOf(chain.Which(chains, chain.WhichQuery{RPC: rpc}, chain.WhichOptions{}))
		missing, extra := diffPairs(s.byRPC[rpc], got)
		if len(missing) > 0 || len(extra) > 0 {
			t.Errorf("-rpc %s: missing %v, extra %v", rpc, missing, extra)
		}
	}
	for _, code := range s.codes {
		got := pairsOf(chain.Which(chains, chain.WhichQuery{Code: code}, chain.WhichOptions{}))
		missing, extra := diffPairs(s.byCode[code], got)
		if len(missing) > 0 || len(extra) > 0 {
			t.Errorf("-code %s: missing %v, extra %v", code, missing, extra)
		}
	}
}

func TestWhichIntersectsBothSelectorsOverTheCorpus(t *testing.T) {
	chains := corpus(t)
	s := scanCorpus(t)

	tried := 0
	for _, rpc := range s.rpcs {
		for _, code := range s.codes {
			want := map[pair]bool{}
			for p := range s.byRPC[rpc] {
				if s.byCode[code][p] {
					want[p] = true
				}
			}
			if len(want) == 0 {
				continue
			}
			tried++
			got := pairsOf(chain.Which(chains, chain.WhichQuery{RPC: rpc, Code: code}, chain.WhichOptions{}))
			missing, extra := diffPairs(want, got)
			if len(missing) > 0 || len(extra) > 0 {
				t.Errorf("-rpc %s -code %s: missing %v, extra %v", rpc, code, missing, extra)
			}
			if tried > 200 {
				return
			}
		}
	}
	if tried == 0 {
		t.Fatal("no rpc and code intersect in the corpus, so the intersection was never exercised")
	}
}

func TestCodePathsComeFromTheCorpusNotFromOneHardCodedShape(t *testing.T) {
	paths := chain.CodePaths(corpus(t))
	if len(paths) < 2 {
		t.Fatalf("a corpus that asserts codes on one path only would make this command a hard-coded lookup, got %v", paths)
	}
	nested := false
	for _, p := range paths {
		if !chain.IsCodePath(p) {
			t.Errorf("CodePaths returned %q, which IsCodePath rejects", p)
		}
		if strings.Count(p, "app_code") == 1 && !strings.HasPrefix(p, "error.") {
			nested = true
		}
	}
	if !nested {
		t.Log("the corpus currently asserts app_code only under error.*; the derivation still covers deeper shapes")
	}
	for _, bad := range []string{"counterparties.0.code", "id_request", "pagination.total"} {
		if chain.IsCodePath(bad) {
			t.Errorf("%q is not a code path: a plain field named code must not make every chain match", bad)
		}
	}
	for _, good := range []string{"error.code", "error.details.0.app_code", "results.0.error.details.0.app_code", "error.details.1.reason"} {
		if !chain.IsCodePath(good) {
			t.Errorf("%q names a failure code and must be searchable", good)
		}
	}
}

func whichFixture() []*chain.Chain {
	short := &chain.Chain{Name: "short", Steps: []*chain.Step{
		{ID: "seed", Call: "pkg.Svc/Create", Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "boom", Call: "pkg.Svc/Approve", Body: map[string]any{"id": "${seed.id}"}, Expect: []chain.Expectation{
			{Path: "error.code", Equals: "failed_precondition"},
			{Path: "error.details.0.app_code", Equals: 1218},
		}},
	}}
	long := &chain.Chain{Name: "long", Steps: []*chain.Step{
		{ID: "a", Call: "pkg.Svc/Create", Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "b", Call: "pkg.Svc/Create", Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "boom_long", Call: "pkg.Svc/Approve", Body: map[string]any{"id": "${a.id}", "other": "${b.id}"},
			Expect: []chain.Expectation{{Path: "error.details.0.app_code", Equals: 1218}}},
	}}
	for _, c := range []*chain.Chain{short, long} {
		if err := c.Normalize(); err != nil {
			panic(err)
		}
	}
	return []*chain.Chain{long, short}
}

func failureResponse(code any) any {
	return map[string]any{"error": map[string]any{
		"code":    "failed_precondition",
		"details": []any{map[string]any{"app_code": code, "reason": "ObligationNotOpen"}},
	}}
}

func TestAnObservedVerdictOutranksAnAssertionAndChangesTheCommand(t *testing.T) {
	hits := chain.Which(whichFixture(), chain.WhichQuery{Code: "1218"}, chain.WhichOptions{
		Observations: func(name string) []chain.Observation {
			if name != "long" {
				return nil
			}
			return []chain.Observation{
				{Run: "r1", Step: "boom_long", Status: "failed", Reached: true, Response: failureResponse(float64(1204))},
				{Run: "r2", Step: "boom_long", Status: "failed", Reached: true, Response: failureResponse(float64(1218))},
			}
		},
	})
	if len(hits) != 2 {
		t.Fatalf("both chains assert 1218, got %d", len(hits))
	}
	if hits[0].Chain != "long" {
		t.Fatalf("a chain a run record OBSERVED answering 1218 must outrank a chain that only asserts it, got %s first", hits[0].Chain)
	}
	if !hits[0].Observed || hits[1].Observed {
		t.Fatalf("observed flags are wrong: %v then %v", hits[0].Observed, hits[1].Observed)
	}
	ev := hits[0].Matches[0].Observed
	if ev == nil || ev.Run != "r2" || ev.Code != "1218" || ev.Path != "error.details.0.app_code" {
		t.Fatalf("the evidence must name the newest run that actually answered 1218 and where it was read, got %+v", ev)
	}
	if want := "shrt chain slice long -step boom_long -mode pin -run r2"; hits[0].Command != want {
		t.Fatalf("an observed match must reproduce through the cheaper pinned form\nwant %q\ngot  %q", want, hits[0].Command)
	}
	if want := "shrt chain slice short -step boom"; hits[1].Command != want {
		t.Fatalf("without a run record the command must stay a closure slice\nwant %q\ngot  %q", want, hits[1].Command)
	}
	if hits[1].Runs != 0 || hits[1].Matches[0].Observed != nil {
		t.Fatalf("a chain with no local runs must report no evidence rather than failing, got %d run(s)", hits[1].Runs)
	}
}

func TestARunThatNeverReachedTheBackendIsNotEvidence(t *testing.T) {
	hits := chain.Which(whichFixture(), chain.WhichQuery{Code: "1218"}, chain.WhichOptions{
		Observations: func(string) []chain.Observation {
			return []chain.Observation{
				{Run: "r1", Step: "boom", Status: "error", Reached: false, Response: failureResponse(float64(1218))},
				{Run: "r1", Step: "boom_long", Status: "error", Reached: false, Response: failureResponse(float64(1218))},
			}
		},
	})
	for _, h := range hits {
		if h.Observed {
			t.Fatalf("chain %s: a step that never reached the backend says nothing about the code", h.Chain)
		}
		if !strings.HasSuffix(h.Command, h.Best) {
			t.Fatalf("chain %s: without evidence the command must not offer to pin a run: %q", h.Chain, h.Command)
		}
	}
}

func TestWithoutEvidenceTheCheaperSliceComesFirst(t *testing.T) {
	fixture := whichFixture()
	hits := chain.Which(fixture, chain.WhichQuery{Code: "1218"}, chain.WhichOptions{
		SliceOf: func(c *chain.Chain, step string) (int, bool) {
			res, err := chain.Slice(c, step, chain.SliceOptions{})
			if err != nil {
				return 0, false
			}
			return len(res.Kept), true
		},
	})
	if hits[0].Chain != "short" {
		t.Fatalf("with no evidence anywhere the smaller slice must come first, got %s", hits[0].Chain)
	}
	if hits[0].Matches[0].SliceSteps != 2 || hits[1].Matches[0].SliceSteps != 3 {
		t.Fatalf("slice sizes were not carried through: %d and %d",
			hits[0].Matches[0].SliceSteps, hits[1].Matches[0].SliceSteps)
	}
}

func TestNeitherSelectorMatchesNothingRatherThanEverything(t *testing.T) {
	if hits := chain.Which(whichFixture(), chain.WhichQuery{Code: "9999"}, chain.WhichOptions{}); len(hits) != 0 {
		t.Fatalf("a code no chain asserts must match nothing, got %d chain(s)", len(hits))
	}
	if hits := chain.Which(whichFixture(), chain.WhichQuery{RPC: "pkg.Svc/Missing"}, chain.WhichOptions{}); len(hits) != 0 {
		t.Fatalf("an rpc no chain calls must match nothing, got %d chain(s)", len(hits))
	}
	hits := chain.Which(whichFixture(), chain.WhichQuery{RPC: "pkg.Svc/Create", Code: "1218"}, chain.WhichOptions{})
	if len(hits) != 0 {
		t.Fatalf("both selectors intersect, so an rpc that never asserts 1218 must match nothing, got %d", len(hits))
	}
}
