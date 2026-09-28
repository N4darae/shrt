package runner_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func keptRedChain(firstWant, secondWant int, pins ...chain.Pin) *chain.Chain {
	add := func(id string, want int) *chain.Step {
		return step(id, "shop.catalog.v1.StockService/AddStock", map[string]any{"id_product": "p-1", "qty": "1"},
			chain.Expectation{Path: "status.code", Equals: "SUCCESS"}, chain.Expectation{Path: "qty_on_hand", Equals: want})
	}
	return &chain.Chain{Name: "kept-red", KeptRed: pins, Steps: []*chain.Step{add("first", firstWant), add("second", secondWant)}}
}

func threeStepKeptRed(secondWant, thirdWant int, thirdBody map[string]any, pins ...chain.Pin) *chain.Chain {
	c := keptRedChain(6, secondWant, pins...)
	c.Steps = append(c.Steps, step("third", "shop.catalog.v1.StockService/AddStock", thirdBody,
		chain.Expectation{Path: "status.code", Equals: "SUCCESS"}, chain.Expectation{Path: "qty_on_hand", Equals: thirdWant}))
	return c
}

func secondRefused(status int, contentType, body string) http.HandlerFunc {
	var mu sync.Mutex
	calls := 0
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 2 {
			answering(status, contentType, body)(w, r)
			return
		}
		answering(200, "application/json", stock5)(w, r)
	}
}

func TestKeptRedHoldsOnlyWhenTheChainFailsExactlyAsPinned(t *testing.T) {
	plain := map[string]any{"id_product": "p-1", "qty": "1"}
	reads := map[string]any{"id_product": "${first.qty_on_hand}", "qty": "1"}
	pin := func(s, path string) chain.Pin { return chain.Pin{Step: s, Path: path} }
	pinGot := func(s, path, got string) chain.Pin { return chain.Pin{Step: s, Path: path, Got: strp(got)} }
	secondOnly := func(c *chain.Chain) *chain.Chain {
		c.Steps[1].Expect = []chain.Expectation{{Path: "qty_on_hand", Equals: 5}}
		return c
	}
	grouped := &chain.Chain{Name: "kept-red", KeptRed: []chain.Pin{pin("second", "qty_on_hand")}, Steps: []*chain.Step{
		step("first", "shop.catalog.v1.StockService/AddStock", plain, chain.Expectation{Path: "status.code", Equals: "REJECTED"}, chain.Expectation{Path: "qty_on_hand", Equals: 9})}}
	for _, id := range []string{"second", "third", "fourth", "fifth"} {
		grouped.Steps = append(grouped.Steps, step(id, "shop.catalog.v1.StockService/AddStock", reads,
			chain.Expectation{Path: "status.code", Equals: "SUCCESS"}, chain.Expectation{Path: "qty_on_hand", Equals: 1}))
	}
	unevaluated := keptRedChain(5, 5, pin("second", "qty_on_hand"))
	unevaluated.Steps = unevaluated.Steps[1:]
	product := &chain.Chain{Name: "kept-red", KeptRed: []chain.Pin{pin("second", "qty_on_hand")}, Steps: []*chain.Step{
		step("first", "shop.catalog.v1.ProductService/GetProduct", map[string]any{"id_product": "p-1"},
			chain.Expectation{Path: "status.code", Equals: "SUCCESS"}, chain.Expectation{Path: "product.sku", Equals: "s"}),
		step("second", "shop.catalog.v1.StockService/AddStock", map[string]any{"id_product": "${first.product.id_product}", "qty": "1"},
			chain.Expectation{Path: "status.code", Equals: "SUCCESS"}, chain.Expectation{Path: "qty_on_hand", Equals: 7})}}
	productDown := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/GetProduct") {
			answering(400, "application/json", `{"code":"invalid_argument","message":"down"}`)(w, r)
			return
		}
		answering(200, "application/json", stock5)(w, r)
	}
	none := ""
	for _, tc := range []struct {
		name    string
		c       *chain.Chain
		serve   http.HandlerFunc
		opts    runner.Options
		verdict string
		note    []string
		not     []string
		once    string
		fresh   *string
		status  string
		sent    [2]string
	}{
		{name: "as pinned", c: keptRedChain(5, 7, pin("second", "qty_on_hand")), verdict: runner.KeptRedAsPinned, note: []string{"failed exactly"}},
		{name: "as pinned with got", c: keptRedChain(5, 7, pinGot("second", "qty_on_hand", "5")), verdict: runner.KeptRedAsPinned, note: []string{"got=5"}},
		{name: "as pinned with a got read from the run", c: keptRedChain(5, 7, pinGot("second", "qty_on_hand", "${first.qty_on_hand}")), verdict: runner.KeptRedAsPinned, note: []string{"failed exactly"}},
		{name: "a pin path in json case", c: keptRedChain(5, 7, pin("second", "qtyOnHand")), verdict: runner.KeptRedAsPinned},
		{name: "a pin without got on a redacted path", c: keptRedChain(5, 7, pin("second", "qty_on_hand")), opts: runner.Options{Redact: []string{"**.qty_on_hand"}}, verdict: runner.KeptRedAsPinned},
		{name: "every pin is evaluated", c: threeStepKeptRed(7, 5, plain, pin("first", "qty_on_hand"), pin("second", "qty_on_hand")), verdict: runner.KeptRedAsPinned, note: []string{"failed exactly"}},
		{name: "a dry run has no kept_red verdict", c: keptRedChain(5, 7, pin("second", "qty_on_hand")), opts: runner.Options{DryRun: true}},
		{name: "defect gone", c: keptRedChain(5, 5, pin("second", "qty_on_hand")), verdict: runner.KeptRedGone, note: []string{"is gone"}},
		{name: "fails earlier", c: keptRedChain(6, 7, pin("second", "qty_on_hand")), verdict: runner.KeptRedNotAsPinned, note: []string{`step "first" failed where nothing is pinned`}},
		{name: "fails differently", c: keptRedChain(5, 7, pinGot("second", "qty_on_hand", "4")), verdict: runner.KeptRedNotAsPinned, note: []string{"second qty_on_hand: pinned got=4, now got=5"}},
		{name: "a got read from the run differs", c: keptRedChain(5, 7, pinGot("second", "qty_on_hand", "${first.qty_on_hand}0")), verdict: runner.KeptRedNotAsPinned, note: []string{"pinned got=50, now got=5"}},
		{name: "pinned path held", c: keptRedChain(5, 7, pin("second", "status.code")), verdict: runner.KeptRedNotAsPinned, note: []string{"failed where nothing is pinned"}},
		{name: "a pinned path now passes", c: keptRedChain(5, 7, pinGot("first", "qty_on_hand", "4"), pin("second", "qty_on_hand")), verdict: runner.KeptRedNotAsPinned, note: []string{"first qty_on_hand: pinned got=4, now got=5, which passes"}},
		{name: "a pinned step that passed reads with one but", c: keptRedChain(5, 7, pin("first", "qty_on_hand"), pin("second", "qty_on_hand")), verdict: runner.KeptRedNotAsPinned, note: []string{"first qty_on_hand: pinned failing, now passes"}, once: ", but "},
		{name: "a pin that passes after an unpinned failure may be masked", c: threeStepKeptRed(5, 7, plain, pin("second", "qty_on_hand")), verdict: runner.KeptRedNotAsPinned,
			note: []string{`the pins that now pass come after "first" failed, so they may be masked`}},
		{name: "a later regression is seen", c: threeStepKeptRed(5, 7, plain, pin("first", "qty_on_hand")), verdict: runner.KeptRedNotAsPinned, note: []string{`step "third" failed where nothing is pinned`}},
		{name: "a step left unexercised", c: threeStepKeptRed(5, 5, reads, pin("first", "qty_on_hand")), verdict: runner.KeptRedNotAsPinned, note: []string{`step "third" was not sent`}},
		{name: "the pin after an unpinned failure is evaluated", c: threeStepKeptRed(7, 5, plain, pin("second", "qty_on_hand")), verdict: runner.KeptRedNotAsPinned,
			note: []string{`step "first" failed where nothing is pinned`}, not: []string{"never answered"}, sent: [2]string{"second", runner.StatusFailed},
			fresh: strp("NEW FAILURE outside the pinned defect: first qty_on_hand want=6 got=5")},
		{name: "a pinned step behind a failed step is not sent", c: threeStepKeptRed(5, 7, reads, pin("third", "qty_on_hand")), verdict: runner.KeptRedNotAsPinned,
			note: []string{`step "first" failed where nothing is pinned`, `step "third" was not sent`, "its pinned failure was not seen"}, not: []string{"never answered"}, sent: [2]string{"third", runner.StatusSkipped}},
		{name: "two unpinned failures", c: threeStepKeptRed(7, 7, plain, pin("second", "qty_on_hand")), verdict: runner.KeptRedNotAsPinned,
			fresh: strp("NEW FAILURE outside the pinned defect: first qty_on_hand want=6 got=5; third qty_on_hand want=7 got=5")},
		{name: "a pinned path that held is no new failure", c: threeStepKeptRed(5, 5, plain, pin("first", "status.code")), verdict: runner.KeptRedNotAsPinned,
			fresh: strp("NEW FAILURE outside the pinned defect: first qty_on_hand want=6 got=5")},
		{name: "only a pin that differs", c: threeStepKeptRed(5, 5, plain, pinGot("first", "qty_on_hand", "4")), verdict: runner.KeptRedNotAsPinned, fresh: &none},
		{name: "failures and unsent steps are grouped", c: grouped, verdict: runner.KeptRedNotAsPinned,
			note: []string{`step "second" was not sent`, "3 other steps were not sent (third, fourth, fifth)"}, once: `"first"`},
		{name: "an unevaluated pinned expectation", c: unevaluated, serve: answering(200, "application/json", `{"status":{"code":"SUCCESS"},"qtyOnHand":"5","qtyOnHand":"5"}`),
			verdict: runner.KeptRedNotAsPinned, note: []string{"was not evaluated"}},
		{name: "an internal error at the pinned step", c: secondOnly(keptRedChain(5, 5, pin("second", "qty_on_hand"))), serve: secondRefused(500, "application/json", `{"code":"internal","message":"panic: nil map"}`),
			verdict: runner.KeptRedNotAsPinned, note: []string{`step "second": the pinned step was refused at transport`}},
		{name: "not found at the pinned step", c: secondOnly(keptRedChain(5, 5, pin("second", "qty_on_hand"))), serve: secondRefused(404, "application/json", `{"code":"not_found","message":"no such rpc"}`),
			verdict: runner.KeptRedNotAsPinned, note: []string{`step "second": the pinned step was refused at transport`}},
		{name: "permission denied at the pinned step", c: secondOnly(keptRedChain(5, 5, pin("second", "qty_on_hand"))), serve: secondRefused(403, "application/json", `{"code":"permission_denied","message":"nope"}`),
			verdict: runner.KeptRedNotAsPinned, note: []string{`step "second": the pinned step was refused at transport`}},
		{name: "a gateway answer at the pinned step", c: secondOnly(keptRedChain(5, 5, pin("second", "qty_on_hand"))), serve: secondRefused(503, "text/html", `<html>bad gateway</html>`),
			status: runner.StatusError},
		{name: "each not-sent reason only on its step", c: product, serve: productDown, verdict: runner.KeptRedNotAsPinned,
			note: []string{`step "second" was not sent (why is on its line)`}, not: []string{"not sent: ${"},
			fresh: strp(runner.NewFailurePrefix + "first refused at transport: invalid_argument: down")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serve := tc.serve
			if serve == nil {
				serve = answering(200, "application/json", stock5)
			}
			rec := run(t, shopRunner(t, serve, false), normalized(t, tc.c), tc.opts)
			if tc.status != "" {
				if rec.Status != tc.status || rec.KeptRed == runner.KeptRedAsPinned {
					t.Fatalf("status %s kept_red %q, want %s and never as pinned", rec.Status, rec.KeptRed, tc.status)
				}
				return
			}
			if rec.KeptRed != tc.verdict {
				t.Fatalf("kept_red verdict %q, want %q (status %s, note %q)", rec.KeptRed, tc.verdict, rec.Status, rec.KeptRedNote)
			}
			for _, s := range tc.note {
				if !strings.Contains(rec.KeptRedNote, s) {
					t.Errorf("note %q does not say %q", rec.KeptRedNote, s)
				}
			}
			for _, s := range tc.not {
				if strings.Contains(rec.KeptRedNote, s) {
					t.Errorf("note %q must not say %q", rec.KeptRedNote, s)
				}
			}
			if tc.once != "" && strings.Count(rec.KeptRedNote, tc.once) != 1 {
				t.Errorf("note %q must say %q once", rec.KeptRedNote, tc.once)
			}
			if tc.fresh != nil && rec.KeptRedNew != *tc.fresh {
				t.Errorf("finding %q, want %q", rec.KeptRedNew, *tc.fresh)
			}
			if tc.sent[0] != "" {
				if sr, ok := rec.Step(tc.sent[0]); !ok || sr.Status != tc.sent[1] {
					t.Errorf("a kept_red chain runs every step as -keep-going does: step %q want %s, got %+v", tc.sent[0], tc.sent[1], sr)
				}
			}
		})
	}
}

func TestKeptRedPinsAreCheckedAtLoadAndLint(t *testing.T) {
	for _, p := range []chain.Pin{{Step: "nope", Path: "qty_on_hand"}, {Step: "second", Path: "qty"}} {
		if err := keptRedChain(5, 7, p).Normalize(); err == nil || !strings.Contains(err.Error(), "kept_red") {
			t.Fatalf("pin %+v must be refused at load, got %v", p, err)
		}
	}
	c := normalized(t, keptRedChain(5, 7, chain.Pin{Step: "second", Path: "qty_on_hand", Got: strp("5")}))
	found := false
	for _, i := range chain.LintWith(c, catalogtest.Shop(), chain.LintOptions{Redact: []string{"**.qty_on_hand"}}) {
		found = found || (i.IsError() && strings.Contains(i.Message, "kept_red") && strings.Contains(i.Message, "redact"))
	}
	if !found {
		t.Fatal("lint must error on a kept_red got on a redacted path")
	}
	own := keptRedChain(5, 7, chain.Pin{Step: "second", Path: "qty_on_hand", Got: strp("5")})
	own.Redact = []string{"**.qty_on_hand"}
	if err := own.Normalize(); err == nil || !strings.Contains(err.Error(), "redact") {
		t.Fatalf("the chain's own redact covers the pinned path, so load must refuse it; got %v", err)
	}
}

func TestPinChangesNamesAPinWhoseGotMoved(t *testing.T) {
	c := &chain.Chain{Name: "k", Steps: []*chain.Step{{ID: "make"}, {ID: "read"}, {ID: "read_2"}},
		KeptRed: []chain.Pin{{Step: "read", Path: "level", Got: strp("8")}, {Step: "read_2", Path: "state", Got: strp("DONE")}}}
	rec := func(level string, second chain.ExpectResult) *runner.Record {
		return &runner.Record{KeptRed: runner.KeptRedNotAsPinned, Steps: []*runner.StepRecord{
			{ID: "make", Status: runner.StatusFailed, Expect: []chain.ExpectResult{{Path: "total", Rule: "equals", Want: "5", Got: "4"}}},
			{ID: "read", Status: runner.StatusFailed, Expect: []chain.ExpectResult{{Path: "level", Rule: "equals", Want: "10", Got: level}}},
			{ID: "read_2", Status: runner.StatusFailed, Expect: []chain.ExpectResult{second}},
		}}
	}
	unevaluated := chain.ExpectResult{Path: "state", Rule: "unevaluated"}
	if changed, held := runner.PinChanges(c, rec("8", unevaluated)); len(changed) != 0 || !held {
		t.Errorf("a pin unevaluated behind another failure and a pin that held: held, got %v %v", changed, held)
	}
	if changed, held := runner.PinChanges(c, rec("-3", unevaluated)); changed["read level"] != "8" || held {
		t.Errorf("a pinned got that moved is named with its pinned value, got %v %v", changed, held)
	}
	if _, held := runner.PinChanges(c, rec("8", chain.ExpectResult{Path: "other", Rule: "equals", Want: "a", Got: "b"})); held {
		t.Error("a pinned path that no longer fails does not hold")
	}
	passes := &chain.Chain{Name: "k", Steps: []*chain.Step{{ID: "read"}}, KeptRed: []chain.Pin{{Step: "read", Path: "orders.1", Got: strp("true")}}}
	gone := &runner.Record{KeptRed: runner.KeptRedGone, Steps: []*runner.StepRecord{
		{ID: "read", Status: runner.StatusPassed, Expect: []chain.ExpectResult{{Path: "orders.1", Rule: "exists", Want: false, Got: false, Passed: true}}},
	}}
	if changed, held := runner.PinChanges(passes, gone); changed["read orders.1"] != "true" || held {
		t.Errorf("a pin that passes moved from its pinned got, even when every step passed, got %v %v", changed, held)
	}
}
