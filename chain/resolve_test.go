package chain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func newScope() *chain.Scope {
	s := chain.NewScope(map[string]any{"book_id": "BOOK-A", "tag": "t", "nested": map[string]any{"n": 7, "a": "b"}})
	s.Env = func(k string) (string, bool) { return "staff", k == "API_USER" }
	s.Now = func() time.Time { return time.Date(2026, 9, 18, 15, 4, 5, 0, time.UTC) }
	s.Record("create", map[string]any{"name": "widget"}, map[string]any{
		"id":    "thing-1",
		"deals": []any{map[string]any{"id": "d1"}, map[string]any{"id": "d2"}},
		"count": float64(2),
	})
	s.Record("create-thing", nil, map[string]any{"id": "x-1"})
	s.Record("p1", nil, map[string]any{"product": map[string]any{"id": "p-1"}, "tags": []any{"a"}})
	s.Record("mk", nil, map[string]any{"results": []any{map[string]any{"id": "a"}}})
	s.Exports["thing_id"] = "thing-1"
	return s
}

func TestResolveReferences(t *testing.T) {
	s := newScope()
	for in, want := range map[string]any{
		"${vars.book_id}":                "BOOK-A",
		"${vars.nested.n}":               7,
		"${env.API_USER}":                "staff",
		"${create.id}":                   "thing-1",
		"${steps.create.response.id}":    "thing-1",
		"${steps.create.request.name}":   "widget",
		"${create.deals.1.id}":           "d2",
		"${exports.thing_id}":            "thing-1",
		"${thing_id}":                    "thing-1",
		"deal-${create.id}-suffix":       "deal-thing-1-suffix",
		"x-${p1.product.id}-${vars.tag}": "x-p-1-t",
		"${create-thing.id}":             "x-1",
		"${nowunix}":                     "1789743845",
		"${nowunix+3600}":                "1789747445",
		"${now+259200}":                  "2026-09-21T15:04:05Z",
		"${today}":                       "1789689600",
		"${today-86400}":                 "1789603200",
		"${today+86400}":                 "1789776000",
		"plain":                          "plain",
	} {
		got, err := s.ResolveValue(in)
		if err != nil || got != want {
			t.Errorf("%s = %#v (%v), want %#v", in, got, err, want)
		}
	}
	for in, says := range map[string]string{
		"${vars.missing}":    "",
		"${env.NOT_SET}":     "",
		"${later.id}":        "",
		"${create.nope}":     "",
		"${today-oneday}":    "seconds",
		"x-${p1.product}":    "",
		"x-${p1.tags} y":     "",
		"v ${vars.nested}":   "",
		"${mk.results.1.id}": "",
	} {
		if got, err := s.ResolveValue(in); err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("%s must not resolve (saying %q), got %v %v", in, says, got, err)
		}
	}
	got, err := s.ResolveValue(map[string]any{"n": "${create.count}", "flag": true})
	if m, _ := got.(map[string]any); err != nil || m["n"] != float64(2) || m["flag"] != true {
		t.Fatalf("a whole-value reference keeps its type: %#v %v", got, err)
	}
	if a, _ := s.ResolveValue("${uuid}"); a == must(s.ResolveValue("${uuid}")) {
		t.Fatal("${uuid} is fresh per reference")
	}
}

func must(v any, _ error) any { return v }

func TestTheClockIsPinnedForTheWholeRun(t *testing.T) {
	ticks := 0
	s := chain.NewScope(nil)
	s.Now = func() time.Time {
		ticks++
		return time.Unix(1789743845+int64(ticks), 0)
	}
	if must(s.ResolveValue("${nowunix}")) != must(s.ResolveValue("${nowunix}")) || must(s.ResolveValue("${today}")) != must(s.ResolveValue("${today}")) || ticks != 1 {
		t.Fatalf("the clock is read once per scope, read %d times", ticks)
	}
}

func TestAMissingPathNamesTheNearestPartTheResponseHad(t *testing.T) {
	s := chain.NewScope(nil)
	s.Record("create_product", nil, map[string]any{"status": map[string]any{"code": "SUCCESS", "details": []any{}}})
	_, err := s.ResolveValue("${create_product.status.details.0.reason}")
	var missing *chain.MissingPathError
	want := `unresolved reference ${create_product.status.details.0.reason}: step "create_product" answered without status.details.0.reason (status.details is [])`
	if !errors.As(err, &missing) || err.Error() != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}

func TestSyntheticLeniencyExcusesOnlyAListIndex(t *testing.T) {
	d := chain.NewScope(nil)
	d.RecordSynthetic("mk", nil, map[string]any{"results": []any{map[string]any{"id": "a"}}})
	if v, err := d.ResolveValue("${mk.results.1.id}"); err != nil || v != "a" {
		t.Fatalf("a synthesized response takes any index: %v %v", v, err)
	}
	if _, err := d.ResolveValue("${mk.results.0.no_such_field}"); err == nil {
		t.Fatal("synthetic leniency must not excuse a missing field")
	}
}

func TestAuthBodyCheckAgreesWithTheAuthBodyResolver(t *testing.T) {
	for ref, resolves := range map[string]bool{
		"${env.WIDGET_USER}": true, "${uuid}": true, "${now}": true, "${nowunix+3600}": true, "${today-86400}": true,
		"prefix-${env.WIDGET_USER}": true, "${vars.x}": false, "${exports.token}": false, "${token}": false,
		"${login.access_token}": false, "${steps.login.response.access_token}": false, "${nowunix+1h}": false,
	} {
		body := map[string]any{"username": ref}
		sc := chain.AuthBodyScope()
		sc.Env = func(string) (string, bool) { return "set", true }
		if _, err := sc.ResolveValue(body); (err == nil) != resolves {
			t.Fatalf("%s: the resolver says %v, the table is wrong", ref, err)
		}
		problems := chain.AuthBodyReferenceProblems(body)
		if resolves != (len(problems) == 0) || (!resolves && !strings.Contains(problems[0], ref[strings.Index(ref, "${"):])) {
			t.Errorf("%s resolves=%v, static check says %v", ref, resolves, problems)
		}
	}
}

func TestCoerceVarsFollowsTheDeclaredType(t *testing.T) {
	for _, tc := range []struct {
		declared, supplied any
		want               any
	}{
		{"1788868800", int64(1790062486), "1790062486"},
		{10, "25", int64(25)},
		{false, "true", true},
		{10, "not a number", "not a number"},
		{nil, int64(7), int64(7)},
	} {
		c := &chain.Chain{Name: "c", Vars: map[string]any{"other": "1"}}
		if tc.declared != nil {
			c.Vars["v"] = tc.declared
		}
		got := c.CoerceVars(map[string]any{"v": tc.supplied})
		if got["v"] != tc.want || len(got) != 1 {
			t.Errorf("declared %#v, supplied %#v: got %#v", tc.declared, tc.supplied, got)
		}
	}
}

func TestVarsTheChainReads(t *testing.T) {
	c := &chain.Chain{Name: "fixtures", Vars: map[string]any{"tag": "SEED"}, Steps: []*chain.Step{{
		ID: "create", Call: "ThingService/Create", Body: map[string]any{"code": "T-${vars.tag}", "note": "${vars.reason}"},
		Expect: []chain.Expectation{okCode, {Path: "name", Equals: "${vars.expected_name}"}},
	}}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	if got := c.UnusedVarNames(map[string]any{"taag": "X", "tag": "a", "reason": "b", "expected_name": "c"}); strings.Join(got, ",") != "taag" {
		t.Errorf("UnusedVarNames = %v, want [taag]", got)
	}
	if got := c.UnusedVarNames(nil); got != nil {
		t.Errorf("UnusedVarNames(nil) = %v", got)
	}
	if got := strings.Join(c.DeclaredVarNames(), ","); got != "expected_name,reason,tag" {
		t.Errorf("DeclaredVarNames = %q", got)
	}
	m := &chain.Chain{Name: "t", Vars: map[string]any{"declared": "x"}, Steps: []*chain.Step{
		{ID: "a", Call: "ThingService/Create", Body: map[string]any{"name": "${vars.declared}-${vars.batch}-${vars.given}-${vars.tag}", "kind": "${vars.kind}"}}}}
	if got := m.MissingVars(map[string]any{"given": "y"}); strings.Join(got, ",") != "batch,kind" {
		t.Errorf("MissingVars = %v, want [batch kind]: an undeclared tag is fresh per run", got)
	}
	in := &chain.Chain{Name: "t", Vars: map[string]any{"declared": "x"}, Steps: []*chain.Step{create(map[string]any{
		"name": "${vars.declared}", "kind": "${vars.undeclared_one}", "idempotency_key": "${vars.undeclared_two}", "note": "${env.B_NAME}-${env.A_KIND}"}, nil)}}
	if err := in.Normalize(); err != nil {
		t.Fatal(err)
	}
	vars, env := chain.ExternalInputs(in)
	if strings.Join(vars, ",") != "undeclared_one,undeclared_two" || strings.Join(env, ",") != "A_KIND,B_NAME" {
		t.Errorf("ExternalInputs = %v %v", vars, env)
	}
}

func TestAMapOrListVarInsideTextIsRefusedUpFront(t *testing.T) {
	vars := map[string]any{"obj": map[string]any{"a": "b"}, "tags": []any{"x"}}
	for _, tc := range []struct {
		name, header string
		refused      bool
	}{
		{"n ${vars.obj}", "", true},
		{"n ${vars.tags} y", "", true},
		{"n", "${vars.obj}", true},
		{"n ${vars.obj.a}", "", false},
		{"${vars.obj}", "", false},
	} {
		c := productNamed(tc.name, nil, vars)
		if tc.header != "" {
			c.Steps[1].Headers = map[string]string{"X-Obj": tc.header}
		}
		p := c.VarStructureProblems(vars)
		if tc.refused != (len(p) > 0) || (tc.header != "" && !strings.Contains(p[0], "header X-Obj")) {
			t.Errorf("%q %q: refused=%v, got %v", tc.name, tc.header, tc.refused, p)
		}
		if tc.refused && tc.header == "" && !lintErrors(c, "${vars.") {
			t.Errorf("%q: lint must error", tc.name)
		}
	}
}

func lintErrors(c *chain.Chain, text string) bool {
	if err := c.Normalize(); err != nil {
		return false
	}
	for _, i := range chain.Lint(c, catalogtest.Shop()) {
		if i.IsError() && strings.Contains(i.Message, text) {
			return true
		}
	}
	return false
}
