package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	coredistillation "github.com/N4darae/shrt"
	"github.com/N4darae/shrt/chain"
)

type slcCase struct {
	name  string
	setup func(t *testing.T)
	red   bool
	args  []string
	code  int
	want  []string
	not   []string
	check func(t *testing.T, out string)
}

func slcSlice(t *testing.T, red bool, args ...string) (string, int) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		out := captureStderr(t, func() {
			if red {
				err = keptRedSlice(context.Background(), args)
			} else {
				err = chainSlice(context.Background(), args)
			}
		})
		os.Stdout.WriteString(out)
	})
	return out + errText(err), exitCodeOf(err)
}

func slcNext(out string) []string {
	m := regexp.MustCompile(`next: shrt chain slice (.*)`).FindStringSubmatch(out)
	if m == nil {
		return nil
	}
	return strings.Fields(m[1])
}

func slcRunNext(t *testing.T, out string, extra ...string) (string, int) {
	t.Helper()
	next := slcNext(out)
	if next == nil {
		t.Fatalf("no next: command in:\n%s", out)
	}
	return slcSlice(t, false, append(next, extra...)...)
}

func slcPins(t *testing.T, path string) string {
	t.Helper()
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return keptRedPaths(c)
}

func slcSteps(t *testing.T, path string) (*chain.Chain, string) {
	t.Helper()
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, s := range c.Steps {
		ids = append(ids, s.ID)
	}
	return c, strings.Join(ids, ",")
}

func slcMustRun(t *testing.T, args ...string) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), append(args, "-quiet")) })
	if err != nil {
		t.Fatalf("shrt run %v: %v\n%s", args, err, out)
	}
}

func slcRunAny(args ...string) {
	_ = runRun(context.Background(), append(args, "-quiet"))
}

func slcExists(t *testing.T, path string, want bool) {
	t.Helper()
	if _, err := os.Stat(path); (err == nil) != want {
		t.Fatalf("%s exists=%v, want %v", path, err == nil, want)
	}
}

func slcHas(t *testing.T, path string, want ...string) string {
	t.Helper()
	raw := string(mustRead(t, path))
	for _, w := range want {
		if !strings.Contains(raw, w) {
			t.Fatalf("%s lacks %q:\n%s", path, w, raw)
		}
	}
	return raw
}

func slcNoisy(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeNoisyChain(t, "widget")
	slcMustRun(t, "cli-noisy-flow")
}

func slcShop(bug func(*fakeShop), file, text string, runArgs ...string) func(t *testing.T) {
	return func(t *testing.T) {
		shop := newFakeShop()
		if bug != nil {
			bug(shop)
		}
		chdirToFakeShop(t, shop)
		writeFile(t, file, text)
		slcRunAny(append([]string{file}, runArgs...)...)
	}
}

func slcPair(fetch func(string) (int, map[string]any)) func(t *testing.T) {
	return func(t *testing.T) {
		newSliceBackend(t, &sliceBackend{fetch: fetch})
		slcRunAny("cli-pair")
	}
}

func slcRefusing(message *string) func(string) (int, map[string]any) {
	return func(string) (int, map[string]any) {
		return 200, map[string]any{"error": map[string]any{"code": "REJECTED", "message": *message}, "id": "thing-0"}
	}
}

func okFetch(id string) (int, map[string]any) {
	return 200, map[string]any{"error": map[string]any{"code": "OK"}, "id": id, "name": "widget"}
}

func TestSliceCases(t *testing.T) {
	noisy := []string{"cli-noisy-flow", "-step", "fetch", "-verify", "-run", "latest"}
	cancelBug := func(s *fakeShop) { s.cancelConfirmedBug = true }
	orders := ".shrt/scratch/probe-orders.yaml"
	message := "qty must be greater than zero"
	changeMessage := func(t *testing.T) {
		message = "qty must be greater than zero"
		slcPair(slcRefusing(&message))(t)
		message = "caller must hold role ADMIN"
	}
	refuse := false
	total := 2
	var round2Code *string
	cases := []slcCase{
		{name: "a dropped write on a kept entity is a note on a matched verdict, not a reason for INCONCLUSIVE", setup: slcNoisy, args: noisy,
			want: []string{"verify reproduced 3/3: step fetch, source run ", "note: the slice dropped write step(s) on entities the kept steps use and gave the step its verdict without them: fill\n", "slice run not kept"},
			not:  []string{"INCONCLUSIVE", "next:", "WARNING possible under-inclusion", "whether a dropped write caused it"},
			check: func(t *testing.T, out string) {
				slcExists(t, ".shrt/runs/cli-noisy-flow-slice-fetch", false)
				if hollow := captureStdout(t, func() { _ = chainHollow(nil) }); strings.Contains(hollow, "orphan") {
					t.Fatalf("slice -verify left an orphan run:\n%s", hollow)
				}
			}},
		{name: "a matched -write records VERIFIED at the path it names", setup: slcNoisy,
			args: append(noisy, "-write", ".shrt/scratch/noisy-repro.yaml"),
			want: []string{"verify reproduced 3/3"},
			check: func(t *testing.T, _ string) {
				if raw := slcHas(t, ".shrt/scratch/noisy-repro.yaml", "VERIFIED by 'shrt chain slice -verify'"); strings.Contains(raw, "HYPOTHESIS") || strings.Contains(raw, "INCONCLUSIVE") {
					t.Fatalf("the verdict replaces the hypothesis:\n%s", raw)
				}
			}},
		{name: "next keeps the -write path", setup: func(t *testing.T) {
			round2Code = round2Workspace(t)
			*round2Code = "PERMISSION_DENIED"
		}, args: []string{"cli-r2-flow", "-step", "fetch", "-run", "latest", "-var", "batch=T2", "-verify", "-write", ".shrt/scratch/r2-repro.yaml"}, code: 1,
			want: []string{"NOT REPRODUCED", "-verify -write .shrt/scratch/r2-repro.yaml\n"}},
		{name: "an unset env var is did not run", setup: func(t *testing.T) {
			srv := newFakeCLIBackend()
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			writeNoisyChain(t, "${env.SHRT_SLICE_TEST_NAME}")
			t.Setenv("SHRT_SLICE_TEST_NAME", "widget")
			slcMustRun(t, "cli-noisy-flow")
			_ = os.Unsetenv("SHRT_SLICE_TEST_NAME")
		}, args: noisy, code: 3, want: []string{"DID NOT RUN", "SHRT_SLICE_TEST_NAME"}, not: []string{"NOT REPRODUCED"}},

		{name: "unknown step", setup: freshTagWorkspace, args: []string{"cli-fresh-flow", "-step", "fetchh", "-run", "latest"}, code: 1},
		{name: "unknown step under -verify", setup: freshTagWorkspace, args: []string{"cli-fresh-flow", "-step", "fetchh", "-run", "latest", "-verify"}, code: 1},
		{name: "-verify without -run verifies against the latest run", setup: freshTagWorkspace, args: []string{"cli-fresh-flow", "-step", "fetch", "-verify"}, code: 1,
			want: []string{"-var tag=<fresh>"}, not: []string{"-verify needs -run"}},
		{name: "unknown chain", setup: freshTagWorkspace, args: []string{"no-such-chain", "-step", "fetch"}, code: 1},
		{name: "unknown chain under -verify", setup: freshTagWorkspace, args: []string{"no-such-chain", "-step", "fetch", "-run", "latest", "-verify"}, code: 1},
		{name: "a kept write interpolating a var needs a fresh one", setup: freshTagWorkspace,
			args: []string{"cli-fresh-flow", "-step", "fetch", "-run", "latest", "-verify"}, code: 1, want: []string{"-var tag=<fresh>"}},
		{name: "a slice verified with a fresh var carries that value", setup: freshTagWorkspace,
			args: []string{"cli-fresh-flow", "-step", "fetch", "-run", "latest", "-verify", "-var", "tag=T2", "-write", "cli-fresh-slice"},
			check: func(t *testing.T, _ string) {
				c, _ := slcSteps(t, ".shrt/scratch/cli-fresh-slice.yaml")
				if c.Vars["tag"] != "T2" || !strings.Contains(c.Description, "=<fresh>") {
					t.Fatalf("want tag T2 and a fresh-var note: %v\n%s", c.Vars, c.Description)
				}
			}},
		{name: "-write of a bare file name lands in .shrt/scratch, out of the gate", setup: freshTagWorkspace,
			args: []string{"cli-fresh-flow", "-step", "fetch", "-write", "kept.yaml"}, want: []string{"no sweep reads .shrt/scratch/kept.yaml"},
			not: []string{"lint, hollow and the gate run"},
			check: func(t *testing.T, _ string) {
				slcExists(t, "kept.yaml", false)
				slcExists(t, ".shrt/chains/kept.yaml", false)
				if c, _ := slcSteps(t, ".shrt/scratch/kept.yaml"); c.Name != "kept" {
					t.Fatalf("got name %q", c.Name)
				}
			}},
		{name: "-write of a ./ path stays in the current directory", setup: freshTagWorkspace,
			args: []string{"cli-fresh-flow", "-step", "fetch", "-write", "./here.yaml"},
			check: func(t *testing.T, _ string) {
				slcExists(t, "here.yaml", true)
				slcExists(t, ".shrt/chains/here.yaml", false)
			}},
		{name: "-write without a path stays out of the gate and says where it went", setup: freshTagWorkspace,
			args:  []string{"cli-fresh-flow", "-step", "fetch", "-write"},
			want:  []string{"/.shrt/scratch/cli-fresh-flow-slice-fetch.yaml\n", "no sweep reads .shrt/scratch/cli-fresh-flow-slice-fetch.yaml; run it by path"},
			not:   []string{"lint, hollow and the gate run"},
			check: func(t *testing.T, _ string) { slcExists(t, ".shrt/chains/cli-fresh-flow-slice-fetch.yaml", false) }},
		{name: "-write into paths.chains joins the gate", setup: freshTagWorkspace,
			args: []string{"cli-fresh-flow", "-step", "fetch", "-write", ".shrt/chains/kept.yaml"},
			want: []string{"lint, hollow and the gate run", "mv .shrt/chains/kept.yaml .shrt/scratch/"}},
		{name: "-without over the chain itself is no news", setup: freshTagWorkspace,
			args: []string{"cli-fresh-flow", "-without", "by_tag", "-write", ".shrt/chains/cli-fresh-flow.yaml"},
			want: []string{"written: .shrt/chains/cli-fresh-flow.yaml"}, not: []string{"lint, hollow and the gate run"}},
		{name: "-write to a path writes exactly there and joins no sweep", setup: freshTagWorkspace,
			args: []string{"cli-fresh-flow", "-step", "fetch", "-write", ".shrt/scratch/x"},
			want: []string{"/.shrt/scratch/x.yaml\n", "no sweep reads", "shrt run .shrt/scratch/x.yaml"}, not: []string{"part of every sweep", "mv "},
			check: func(t *testing.T, _ string) {
				slcHas(t, ".shrt/scratch/x.yaml", "name: x\n")
				slcExists(t, ".shrt/chains/.shrt", false)
				writeFile(t, ".shrt/scratch/other.yaml", "hand written\n")
				if out, code := slcSlice(t, false, "cli-fresh-flow", "-step", "fetch", "-write", ".shrt/scratch/other.yaml"); code == 0 || !strings.Contains(out, "already exists") {
					t.Fatalf("an existing file is not overwritten:\n%s", out)
				}
				if raw := string(mustRead(t, ".shrt/scratch/other.yaml")); raw != "hand written\n" {
					t.Fatalf("overwritten: %q", raw)
				}
			}},

		{name: "kept red pins the failing expectation", setup: oneDefectWorkspace, red: true,
			args: []string{"cli-one-defect", "-step", "fetch", "-kept-red", "-write", ".shrt/chains/one-defect-red.yaml"},
			check: func(t *testing.T, _ string) {
				if got := slcPins(t, ".shrt/chains/one-defect-red.yaml"); got != "fetch:name" {
					t.Fatalf("pins %s", got)
				}
				if _, ids := slcSteps(t, ".shrt/chains/one-defect-red.yaml"); strings.Contains(ids, "other") {
					t.Fatalf("only what fetch needs: %s", ids)
				}
				slcMustRun(t, "one-defect-red", "-var", "tag=T10")
			}},
		{name: "kept red -verify writes a verified slice", setup: oneDefectWorkspace, red: true,
			args: []string{"cli-one-defect", "-step", "fetch", "-kept-red", "-verify", "-var", "tag=T12", "-write", ".shrt/chains/one-defect-verified.yaml"},
			not:  []string{"hypothesis until run"},
			check: func(t *testing.T, _ string) {
				c, _ := slcSteps(t, ".shrt/chains/one-defect-verified.yaml")
				if keptRedPaths(c) != "fetch:name" || !chain.HasVerifiedVerdict(c.Description) {
					t.Fatalf("%s\n%s", keptRedPaths(c), c.Description)
				}
			}},
		{name: "kept red on a passing read has nothing to pin", setup: oneDefectWorkspace, red: true,
			args: []string{"cli-one-defect", "-step", "other", "-kept-red"}, code: 1, want: []string{"nothing to pin"}},
		{name: "kept red on a passing write has nothing to pin", setup: oneDefectWorkspace, red: true,
			args: []string{"cli-one-defect", "-step", "create", "-kept-red"}, code: 1, want: []string{"nothing to pin"}},
		{name: "-without failed writes the rest that still runs", setup: oneDefectWorkspace, red: true,
			args: []string{"cli-one-defect", "-without", "failed", "-write", ".shrt/chains/one-defect-rest.yaml"},
			want: []string{"reads fetch", "the new chain holds 2 of the 4 steps, the 2 below left out"},
			check: func(t *testing.T, _ string) {
				if _, ids := slcSteps(t, ".shrt/chains/one-defect-rest.yaml"); ids != "create,other" {
					t.Fatalf("got %s", ids)
				}
				slcMustRun(t, "one-defect-rest", "-var", "tag=T11")
				slcExists(t, ".shrt/chains/cli-one-defect.yaml", true)
			}},
		{name: "-without -write without a path lands in .shrt/scratch", setup: oneDefectWorkspace, red: true,
			args: []string{"cli-one-defect", "-without", "failed", "-write"},
			want: []string{"written: .shrt/scratch/cli-one-defect-without-fetch.yaml\n", "no sweep reads .shrt/scratch/cli-one-defect-without-fetch.yaml"},
			check: func(t *testing.T, _ string) {
				slcExists(t, ".shrt/chains/cli-one-defect-without-fetch.yaml", false)
				if _, ids := slcSteps(t, ".shrt/scratch/cli-one-defect-without-fetch.yaml"); ids != "create,other" {
					t.Fatalf("got %s", ids)
				}
			}},
		{name: "-without -write of the chain's own file name replaces it", setup: oneDefectWorkspace, red: true,
			args: []string{"cli-one-defect", "-without", "failed", "-write", "cli-one-defect.yaml"},
			check: func(t *testing.T, _ string) {
				slcExists(t, "cli-one-defect.yaml", false)
				if c, ids := slcSteps(t, ".shrt/chains/cli-one-defect.yaml"); c.Name != "cli-one-defect" || ids != "create,other" {
					t.Fatalf("got %s %s", c.Name, ids)
				}
			}},
		{name: "kept red pins a kept step that failed instead of relaxing it", setup: twoLineWorkspace, red: true,
			args: []string{"cli-two-lines", "-step", "fetch_again", "-kept-red", "-write", ".shrt/chains/two-red.yaml"}, not: []string{"relaxed:"},
			check: func(t *testing.T, _ string) {
				c, _ := slcSteps(t, ".shrt/chains/two-red.yaml")
				if fetch, _ := c.Step("fetch"); keptRedPaths(c) != "fetch:name,fetch_again:name" || len(fetch.Expect) != 2 {
					t.Fatalf("pins %s, fetch expects %+v", keptRedPaths(c), fetch.Expect)
				}
				slcMustRun(t, "two-red", "-var", "tag=T10")
			}},
		{name: "-kept-red=other pins more steps of the same defect", setup: twoLineWorkspace, red: true,
			args: []string{"cli-two-lines", "-step", "fetch_again", "-kept-red=other", "-write", ".shrt/chains/two-more.yaml"},
			check: func(t *testing.T, _ string) {
				if _, ids := slcSteps(t, ".shrt/chains/two-more.yaml"); strings.Contains(ids, "fine") {
					t.Fatalf("fine is not needed: %s", ids)
				}
				if got := slcPins(t, ".shrt/chains/two-more.yaml"); got != "fetch:name,other:name,fetch_again:name" {
					t.Fatalf("pins %s", got)
				}
				slcMustRun(t, "two-more", "-var", "tag=T11")
			}},
		{name: "a repeated -kept-red adds each step", setup: twoLineWorkspace, red: true,
			args: []string{"cli-two-lines", "-step", "fetch_again", "-kept-red=other", "-kept-red", "-json"}, want: []string{`"id": "other"`}},
		{name: "-kept-red of a step that passed is refused", setup: twoLineWorkspace, red: true,
			args: []string{"cli-two-lines", "-step", "fetch_again", "-kept-red=fine"}, code: 1, want: []string{"fine", "nothing to pin"}},
		{name: "kept red -verify pins each failing kept step once", setup: twoLineWorkspace, red: true,
			args: []string{"cli-two-lines", "-step", "fetch_again", "-kept-red", "-verify", "-var", "tag=T12", "-write", ".shrt/chains/two-verified.yaml"},
			check: func(t *testing.T, _ string) {
				if got := slcPins(t, ".shrt/chains/two-verified.yaml"); got != "fetch:name,fetch_again:name" {
					t.Fatalf("pins %s", got)
				}
			}},
		{name: "kept red of the whole chain writes the slice with every pin", setup: func(t *testing.T) { twoDefectWorkspace(t, "name", "gadget") }, red: true,
			args: []string{"cli-two-defects", "-step", "fetch_again", "-kept-red=fetch,fetch_again", "-verify", "-var", "tag=T20", "-write"},
			not:  []string{"written, and", "itself: no "},
			check: func(t *testing.T, _ string) {
				if got := slcPins(t, ".shrt/chains/cli-two-defects-slice-fetch_again.yaml"); got != "fetch:name,fetch_again:name" {
					t.Fatalf("pins %s", got)
				}
				slcMustRun(t, "cli-two-defects-slice-fetch_again", "-var", "tag=T21")
			}},
		{name: "kept red into the source names no without command", setup: func(t *testing.T) { twoDefectWorkspace(t, "name", "gadget") }, red: true,
			args: []string{"cli-two-defects", "-step", "fetch_again", "-kept-red=fetch", "-write", ".shrt/chains/cli-two-defects.yaml"},
			not:  []string{"-without", "mv .shrt/chains/cli-two-defects.yaml"},
			check: func(t *testing.T, _ string) {
				if c, _ := slcSteps(t, ".shrt/chains/cli-two-defects.yaml"); c.Name != "cli-two-defects" || len(c.KeptRed) != 2 {
					t.Fatalf("%+v", c)
				}
			}},
		{name: "kept red -verify pins what the slice run got", setup: func(t *testing.T) {
			twoDefectWorkspace(t, "name", "gadget")
			e, err := loadEnv(true)
			if err != nil {
				t.Fatal(err)
			}
			rec, err := e.store.LatestRun("cli-two-defects")
			if err != nil {
				t.Fatal(err)
			}
			st, _ := rec.Step("fetch_again")
			for i := range st.Expect {
				st.Expect[i].Got = "only-in-the-source-run"
			}
			if _, err := e.store.SaveRun(rec); err != nil {
				t.Fatal(err)
			}
		}, red: true,
			args: []string{"cli-two-defects", "-step", "fetch", "-kept-red=fetch_again", "-verify", "-run", "latest", "-var", "tag=T13", "-write", ".shrt/chains/two-defects-replayed.yaml"},
			check: func(t *testing.T, _ string) {
				c, _ := slcSteps(t, ".shrt/chains/two-defects-replayed.yaml")
				for _, k := range c.KeptRed {
					if k.Got != nil && strings.Contains(*k.Got, "only-in-the-source-run") {
						t.Fatalf("pinned the source run's got: %+v", c.KeptRed)
					}
				}
				if len(c.KeptRed) != 2 {
					t.Fatalf("%+v", c.KeptRed)
				}
				slcMustRun(t, "two-defects-replayed", "-var", "tag=T14")
			}},
		{name: "a value starting with = gets a did-you-mean", setup: func(t *testing.T) { twoDefectWorkspace(t, "name", "gadget") }, red: true,
			args: []string{"cli-two-defects", "-step", "fetch_again", "-write", "=fetch"}, code: 1, want: []string{"did you mean -write=fetch"},
			check: func(t *testing.T, _ string) {
				entries, _ := os.ReadDir(".shrt/chains")
				for _, en := range entries {
					if strings.HasPrefix(en.Name(), "=") {
						t.Fatalf("wrote %s", en.Name())
					}
				}
			}},

		{name: "a read an upstream failure left unevaluated names the upstream step", setup: blockedWorkspace,
			args: []string{"cli-blocked", "-step", "fetch_again", "-run", "latest", "-verify"}, code: 3,
			want: []string{"INCONCLUSIVE", "not evaluated in source run", "it reads step fetch, which failed there", "the slice evaluated it: name equals passed"},
			not:  []string{"-keep writes", "next:"}},

		{name: "next keeps exactly the write on the entity the kept steps use", setup: slcShop(cancelBug, orders, cancelConfirmedChain, "-var", "tag=src"),
			args: []string{orders, "-step", "cancel_confirmed", "-run", "latest", "-verify", "-v", "-var", "tag=s1"}, code: 3,
			want: []string{"confirm_single       OrderService/ConfirmOrder       changes the state of what create_order_single created, which cancel_confirmed reads",
				"next: shrt chain slice .shrt/scratch/probe-orders.yaml "},
			check: func(t *testing.T, out string) {
				next := slcNext(out)
				for i, f := range next {
					if f == "-keep" && next[i+1] != "add_stock" {
						t.Fatalf("want -keep add_stock: %v", next)
					}
				}
				again, code := slcRunNext(t, out, "-var", "tag=s2")
				if code != 0 || !strings.Contains(again, "verify reproduced") || strings.Contains(again, "INCONCLUSIVE") {
					t.Fatalf("exit %d:\n%s", code, again)
				}
			}},
		{name: "an answered write that failed an expectation is kept and relaxed", setup: slcShop(cancelBug, orders, strings.Replace(cancelConfirmedChain,
			"        qty: \"50\"\n      expect:\n        - path: status.code\n          equals: SUCCESS\n",
			"        qty: \"50\"\n      expect:\n        - path: status.code\n          equals: SUCCESS\n        - path: qty_on_hand\n          equals: \"999\"\n", 1), "-keep-going", "-var", "tag=src"),
			args: []string{orders, "-step", "cancel_confirmed", "-run", "latest", "-verify", "-v", "-keep", "confirm_single", "-var", "tag=s1"}, code: 3,
			not: []string{"No -keep command can reproduce"},
			check: func(t *testing.T, out string) {
				if !strings.Contains(strings.Join(slcNext(out), " "), "add_stock") {
					t.Fatalf("next must keep add_stock:\n%s", out)
				}
				again, code := slcRunNext(t, out, "-var", "tag=s2")
				if code != 0 || !strings.Contains(again, "verify reproduced") || !strings.Contains(again, "add_stock qty_on_hand equals") {
					t.Fatalf("exit %d:\n%s", code, again)
				}
			}},
		{name: "a dropped write the contracts say the target never reads does not block the receipt", setup: slcShop(nil, orders, cancelConfirmedChain, "-var", "tag=src"),
			args: []string{orders, "-step", "create_order_single", "-run", "latest", "-verify", "-v", "-var", "tag=s1"},
			want: []string{"verify reproduced", "add_stock            StockService/AddStock           changes the state of what create_product created, which create_order_single reads"},
			check: func(t *testing.T, _ string) {
				args := []string{orders, "-step", "create_order_single", "-run", "latest", "-verify", "-v"}
				writeFile(t, ".shrt/contracts/orders.yaml", "apiVersion: shrt/contract/v1\ndomain: orders\nrpcs:\n    shop.orders.v1.OrderService/CreateOrder:\n        summary: records a pending order without touching stock\n        required: [NONE]\n        status: draft\n")
				if out, code := slcSlice(t, false, append(args, "-var", "tag=s2")...); code != 0 || !strings.Contains(out, "verify reproduced") {
					t.Fatalf("exit %d:\n%s", code, out)
				}
				writeFile(t, ".shrt/contracts/orders.yaml", "apiVersion: shrt/contract/v1\ndomain: orders\nrpcs:\n    shop.orders.v1.OrderService/CreateOrder:\n        summary: records a pending order\n        required: [NONE]\n        needs: [shop.catalog.v1.StockService/AddStock]\n        status: draft\n")
				if out, code := slcSlice(t, false, append(args, "-var", "tag=s3")...); code != 0 || !strings.Contains(out, "contract needs shop.catalog.v1.StockService/AddStock") || strings.Contains(out, "info: dropped write step add_stock") {
					t.Fatalf("exit %d:\n%s", code, out)
				}
			}},
		{name: "a fixture another chain created is diagnosed", setup: func(t *testing.T) {
			chdirToFakeShop(t, newFakeShop())
			writeFile(t, ".shrt/scratch/other.yaml", strings.Replace(customerChain, "%s", "other", 1))
			writeFile(t, ".shrt/scratch/customers.yaml", strings.Replace(customerChain, "%s", "customers", 1))
			slcMustRun(t, ".shrt/scratch/other.yaml", "-var", "tag=taken")
			slcMustRun(t, ".shrt/scratch/customers.yaml", "-var", "tag=src")
		}, args: []string{".shrt/scratch/customers.yaml", "-step", "create_order", "-run", "latest", "-verify", "-var", "tag=taken"}, code: 3,
			want: []string{"fixture reused", "chain other", "-var tag="}},
		{name: "the fixture tag echoed in a failing value is masked", setup: slcShop(func(s *fakeShop) { s.skuEchoBug = true }, ".shrt/chains/sku-echo.yaml", skuEchoChain, "-var", "tag=src1"),
			args: []string{"sku-echo", "-step", "get", "-run", "latest", "-verify", "-var", "tag=sl1"}, want: []string{"verify reproduced 3/3"}},
		{name: "kept steps that failed an unrelated expectation are relaxed, not a stop", setup: slcShop(func(s *fakeShop) { s.priceBug = true }, ".shrt/scratch/order-happy.yaml", pricedOrderChain, "-keep-going", "-var", "tag=src"),
			args: []string{".shrt/scratch/order-happy.yaml", "-step", "confirm_order", "-run", "latest", "-verify", "-var", "tag=s1"}, code: 1,
			want: []string{"relaxed: ", "create_product product.price_minor equals", "create_order order.total_minor equals"}, not: []string{"DID NOT RUN", "kept failing"},
			check: func(t *testing.T, out string) {
				if next := strings.Join(slcNext(out), " "); !strings.Contains(next, "-keep add_stock ") && !strings.Contains(next, "-keep writes ") {
					t.Fatalf("next must keep add_stock:\n%s", out)
				}
				if again, code := slcRunNext(t, out, "-var", "tag=s2"); code != 0 || !strings.Contains(again, "verify reproduced") {
					t.Fatalf("exit %d:\n%s", code, again)
				}
			}},
		{name: "a kept write failing on the field the target fails keeps that expectation", setup: slcShop(func(s *fakeShop) { s.priceBug = true }, ".shrt/scratch/price-read.yaml", priceReadChain, "-keep-going", "-var", "tag=src"),
			args: []string{".shrt/scratch/price-read.yaml", "-step", "get_product", "-run", "latest", "-verify", "-var", "tag=s1", "-write", ".shrt/scratch/price-repro.yaml"},
			want: []string{"kept failing: kept writes failed in run ", "  create_product product.price_minor equals (want 1250, got 1249)\n", "verify reproduced 3/3"},
			not:  []string{"relaxed:"},
			check: func(t *testing.T, _ string) {
				c, _ := slcSteps(t, ".shrt/scratch/price-repro.yaml")
				if st, _ := c.Step("create_product"); len(st.Expect) != 2 || !strings.Contains(c.Description, "Kept as failed in run ") {
					t.Fatalf("the write's own failing expectation must stay:\n%+v\n%s", st.Expect, c.Description)
				}
			}},
		{name: "a kept step that passed in the source and fails in the slice is inconclusive", setup: func(t *testing.T) {
			slcShop(nil, ".shrt/scratch/probe-confirm.yaml", confirmThenFetchChain)(t)
			writeFile(t, ".shrt/contracts/orders.yaml", "apiVersion: shrt/contract/v1\ndomain: orders\nrpcs:\n    shop.orders.v1.OrderService/ConfirmOrder:\n        summary: confirms the order\n        required: [NONE]\n        status: draft\n    shop.orders.v1.OrderService/FetchOrder:\n        summary: reads the order\n        required: [NONE]\n        status: draft\n")
		}, args: []string{".shrt/scratch/probe-confirm.yaml", "-step", "fetch_order", "-run", "latest", "-verify"}, code: 3,
			want: []string{"kept step(s) confirm_order passed in source run"}, not: []string{"verify reproduced"}},
		{name: "a contract prerequisite the source chain never met stays out of the repro's description", setup: func(t *testing.T) {
			slcShop(nil, ".shrt/scratch/probe-confirm.yaml", confirmThenFetchChain)(t)
			writeFile(t, ".shrt/contracts/orders.yaml", "apiVersion: shrt/contract/v1\ndomain: orders\nrpcs:\n    shop.orders.v1.OrderService/FetchOrder:\n        summary: reads the order\n        required: [NONE]\n        needs: [shop.orders.v1.OrderService/CancelOrder]\n        status: draft\n")
		}, args: []string{".shrt/scratch/probe-confirm.yaml", "-step", "fetch_order", "-v", "-write", ".shrt/scratch/probe-fetch.yaml"},
			want: []string{"contract prerequisites the source chain did not meet before these steps either", "needs shop.orders.v1.OrderService/CancelOrder (declared for fetch_order)"},
			not:  []string{"unmet prerequisites"},
			check: func(t *testing.T, _ string) {
				if raw := slcHas(t, ".shrt/scratch/probe-fetch.yaml", "Slice of probe-confirm"); strings.Contains(raw, "CancelOrder") || strings.Contains(raw, "nmet") {
					t.Fatalf("the description lists a prerequisite the source run did not meet either:\n%s", raw)
				}
				if out, _ := slcSlice(t, false, ".shrt/scratch/probe-confirm.yaml", "-step", "fetch_order"); strings.Contains(out, "CancelOrder") {
					t.Fatalf("without -v the prerequisite is not printed:\n%s", out)
				}
			}},
		{name: "a partial match over the repeats is intermittent", setup: slcShop(func(s *fakeShop) { s.getProductFailAt = map[int]bool{2: true, 3: true, 4: true, 5: true} }, ".shrt/scratch/probe-get.yaml", flakyGetChain, "-keep-going"),
			args: []string{".shrt/scratch/probe-get.yaml", "-step", "get_b", "-run", "latest", "-verify"}, code: 1,
			want: []string{"intermittent: reproduced 1/3"}, not: []string{"verify reproduced", "verify NOT REPRODUCED"}},
		{name: "a repeat a kept step's own failure spoiled is not counted", setup: slcShop(func(s *fakeShop) { s.skuEchoBug, s.addStockFailAt = true, map[int]int{3: 503} }, ".shrt/scratch/stocked-echo.yaml", stockedEchoChain),
			args: []string{".shrt/scratch/stocked-echo.yaml", "-step", "get", "-run", "latest", "-verify", "-var", "tag=rep1"},
			want: []string{"repeat 2 of 3: not counted", "verify reproduced 2/2 (repeat 2 not counted: kept step add_stock failed, unavailable)"},
			not:  []string{"intermittent", "INCONCLUSIVE"}},
		{name: "a repeat that did not run is not counted", setup: slcShop(func(s *fakeShop) { s.skuEchoBug, s.addStockFailAt = true, map[int]int{3: 500} }, ".shrt/scratch/stocked-echo.yaml", stockedEchoChain),
			args: []string{".shrt/scratch/stocked-echo.yaml", "-step", "get", "-run", "latest", "-verify", "-var", "tag=rep1"},
			want: []string{"repeat 2 of 3: not counted", "verify reproduced 2/2 (repeat 2 not counted: kept step add_stock failed, internal)"}, not: []string{"intermittent", "DID NOT RUN"}},
		{name: "every repeat reproduced", setup: slcShop(func(s *fakeShop) { s.getProductFailN = 1 }, ".shrt/scratch/probe-get.yaml", flakyGetChain, "-keep-going"),
			args: []string{".shrt/scratch/probe-get.yaml", "-step", "get_b", "-run", "latest", "-verify"}, want: []string{"verify reproduced 3/3"}},
		{name: "a slice of a scratch chain lands next to it", setup: minimalScratch,
			args: []string{".shrt/scratch/min-stock.yaml", "-step", "get_after", "-keep", "writes", "-write"},
			check: func(t *testing.T, _ string) {
				slcExists(t, ".shrt/chains/min-stock-slice-get_after.yaml", false)
				slcExists(t, ".shrt/scratch/min-stock-slice-get_after.yaml", true)
			}},
		{name: "-write naming the source refuses to drop a step of it, and points at run -repeat", setup: minimalScratch,
			args: []string{".shrt/scratch/min-stock.yaml", "-step", "get_after", "-run", "latest", "-keep", "writes", "-verify", "-var", "tag=v1", "-write", ".shrt/scratch/min-stock.yaml"},
			code: 1, want: []string{"-write .shrt/scratch/min-stock.yaml is min-stock itself, and writing the slice there would drop 1 of its 4 steps (peek)",
				"Nothing was sent or written", "shrt run .shrt/scratch/min-stock.yaml -repeat 3", "-write alone for .shrt/scratch/min-stock-slice-get_after.yaml"},
			not: []string{"verify reproduced"},
			check: func(t *testing.T, _ string) {
				if raw := string(mustRead(t, ".shrt/scratch/min-stock.yaml")); raw != minimalScratchChain {
					t.Fatalf("the source changed:\n%s", raw)
				}
				if out, code := slcSlice(t, false, ".shrt/scratch/min-stock.yaml", "-step", "get_after", "-keep", "writes", "-write", "min-stock"); code != 1 || !strings.Contains(out, "is min-stock itself") {
					t.Fatalf("a bare name that resolves to the source is the source too: exit %d\n%s", code, out)
				}
			}},
		{name: "-write naming the source of a slice that keeps every step as written records only the verdict", setup: minimalScratch,
			args: []string{".shrt/scratch/min-stock.yaml", "-step", "get_after", "-run", "latest", "-keep", "writes,peek", "-verify", "-var", "tag=v1", "-write", ".shrt/scratch/min-stock.yaml"},
			want: []string{"the slice keeps all 4 steps of min-stock, so it is min-stock itself: nothing written", "verify reproduced 3/3"},
			check: func(t *testing.T, _ string) {
				c, _ := slcSteps(t, ".shrt/scratch/min-stock.yaml")
				orig := filepath.Join(t.TempDir(), "min-stock.yaml")
				writeFile(t, orig, minimalScratchChain)
				was, _ := slcSteps(t, orig)
				verified := c.Description
				c.Description, c.SourcePath, was.SourcePath = "", "", ""
				got, _ := c.Marshal()
				want, _ := was.Marshal()
				if !strings.Contains(verified, "VERIFIED by 'shrt chain slice -verify'") || string(got) != string(want) {
					t.Fatalf("only the description gains the verdict:\n%s\n%s", verified, got)
				}
			}},
		{name: "a slice of the source that drops a failing expectation is refused over the source", setup: slcShop(func(s *fakeShop) { s.priceBug = true }, ".shrt/scratch/order-happy.yaml", pricedOrderChain, "-keep-going", "-var", "tag=src"),
			args: []string{".shrt/scratch/order-happy.yaml", "-step", "confirm_order", "-run", "latest", "-keep", "writes", "-var", "tag=s1", "-write", ".shrt/scratch/order-happy.yaml"},
			code: 1, want: []string{"is order-happy itself, and writing the slice there would drop the expectations a kept step failed: create_product product.price_minor equals"},
			check: func(t *testing.T, _ string) {
				if raw := string(mustRead(t, ".shrt/scratch/order-happy.yaml")); raw != pricedOrderChain {
					t.Fatalf("the source changed:\n%s", raw)
				}
			}},
		{name: "a write refusal under -verify exits 1", setup: func(t *testing.T) {
			minimalScratch(t)
			writeFile(t, ".shrt/scratch/other.yaml", "apiVersion: shrt/v1\nname: other\nsteps: []\n")
		}, args: []string{".shrt/scratch/min-stock.yaml", "-step", "get_after", "-run", "latest", "-keep", "writes", "-verify", "-var", "tag=v2", "-write", ".shrt/scratch/other.yaml"},
			code: 1, want: []string{"already exists and is not a slice"},
			check: func(t *testing.T, _ string) {
				if _, code := slcSlice(t, false, ".shrt/scratch/min-stock.yaml", "-step", "get_after", "-keep", "writes", "-write", ".shrt/scratch/other.yaml"); code != 1 {
					t.Fatalf("exit %d", code)
				}
			}},

		{name: "a fresh id in a failure both runs share is masked", setup: slcPair(okFetch), args: []string{"cli-pair", "-step", "fetch", "-verify", "-run", "latest"},
			want: []string{"verify reproduced"}},
		{name: "a refusal for another reason is not reproduced", setup: changeMessage, args: []string{"cli-pair", "-step", "fetch", "-verify", "-run", "latest"},
			code: 1, want: []string{"NOT REPRODUCED", "caller must hold role ADMIN"}},
		{name: "-write records not reproduced instead of the hypothesis", setup: changeMessage,
			args: []string{"cli-pair", "-step", "fetch", "-run", "latest", "-verify", "-write", "probe"}, code: 1,
			check: func(t *testing.T, _ string) {
				if raw := slcHas(t, ".shrt/scratch/probe.yaml", "NOT REPRODUCED by 'shrt chain slice -verify'"); strings.Contains(raw, "HYPOTHESIS") {
					t.Fatalf("%s", raw)
				}
			}},
		{name: "a transport refusal is not reproduced, and reproduced once the source got it too", setup: func(t *testing.T) {
			refuse = false
			slcPair(func(id string) (int, map[string]any) {
				if refuse {
					return http.StatusForbidden, map[string]any{"code": "permission_denied", "message": "no"}
				}
				return okFetch(id)
			})(t)
			refuse = true
		}, args: []string{"cli-pair", "-step", "fetch", "-verify", "-run", "latest"}, code: 1,
			want: []string{"NOT REPRODUCED", "403"}, not: []string{"DID NOT RUN", "never reached the backend"},
			check: func(t *testing.T, _ string) {
				slcRunAny("cli-pair")
				if out, code := slcSlice(t, false, "cli-pair", "-step", "fetch", "-verify", "-run", "latest"); code != 0 || !strings.Contains(out, "verify reproduced") {
					t.Fatalf("exit %d:\n%s", code, out)
				}
			}},
		{name: "a slice of every step records its verdict in the chain itself", setup: slcPair(okFetch),
			args: []string{"cli-pair", "-step", "fetch", "-run", "latest", "-verify", "-write"},
			check: func(t *testing.T, _ string) {
				slcExists(t, ".shrt/chains/cli-pair-slice-fetch.yaml", false)
				c, ids := slcSteps(t, ".shrt/chains/cli-pair.yaml")
				if ids != "create,fetch" || !strings.Contains(c.Description, "VERIFIED by 'shrt chain slice -verify'") || !strings.Contains(c.Description, "own run") || strings.Contains(c.Description, "in source run") {
					t.Fatalf("%s\n%s", ids, c.Description)
				}
			}},
		{name: "re-verifying a slice keeps its verdict against the source", setup: func(t *testing.T) {
			newSliceBackend(t, &sliceBackend{fetch: okFetch})
			writeFile(t, ".shrt/chains/cli-trio.yaml", slcTrioChain)
			slcRunAny("cli-trio")
		}, args: []string{"cli-trio", "-step", "fetch", "-run", "latest", "-verify", "-write"},
			check: func(t *testing.T, _ string) {
				first := string(mustRead(t, ".shrt/scratch/cli-trio-slice-fetch.yaml"))
				verified := regexp.MustCompile(`VERIFIED by [^\n]*source run [^\n]*`).FindString(first)
				if verified == "" {
					t.Fatalf("%s", first)
				}
				if out, code := slcSlice(t, false, ".shrt/scratch/cli-trio-slice-fetch.yaml", "-step", "fetch", "-run", "latest", "-verify", "-write"); code != 0 {
					t.Fatalf("%s", out)
				}
				slcHas(t, ".shrt/scratch/cli-trio-slice-fetch.yaml", verified, "own run")
			}},
		{name: "the same failure with another value is not reproduced", setup: func(t *testing.T) {
			calls := 0
			newSliceBackend(t, &sliceBackend{fetch: func(id string) (int, map[string]any) {
				name := []string{"widget", "gadget"}[min(calls, 1)]
				calls++
				return 200, map[string]any{"error": map[string]any{"code": "OK"}, "id": id, "name": name}
			}})
			writeFile(t, ".shrt/chains/cli-got-flow.yaml", slcGotChain)
			slcRunAny("cli-got-flow")
		}, args: []string{"cli-got-flow", "-step", "fetch", "-run", "latest", "-verify"}, code: 1,
			want: []string{"NOT REPRODUCED", "source got widget", "slice got gadget"}},

		{name: "refused dropped writes are not counted and the verdict is recorded", setup: func(t *testing.T) { round2Workspace(t) },
			args: []string{"cli-r2-flow", "-step", "fetch", "-run", "latest", "-keep", "other", "-var", "batch=T2", "-verify", "-v", "-write"},
			want: []string{"verify reproduced", "\ndropped write steps:\n", "refused: error.code = INTERNAL in run", "refused: transport invalid_argument in run", ", not counted as possible under-inclusion\n"},
			not:  []string{"WARNING possible under-inclusion"},
			check: func(t *testing.T, _ string) {
				c, _ := slcSteps(t, ".shrt/scratch/cli-r2-flow-slice-fetch.yaml")
				if strings.Contains(c.Description, "HYPOTHESIS") || !strings.Contains(c.Description, "VERIFIED") || c.Vars["batch"] != "T2" {
					t.Fatalf("%v\n%s", c.Vars, c.Description)
				}
			}},
		{name: "not reproduced names the command that keeps the writes", setup: func(t *testing.T) {
			round2Code = round2Workspace(t)
			*round2Code = "PERMISSION_DENIED"
		}, args: []string{"cli-r2-flow", "-step", "fetch", "-run", "latest", "-var", "batch=T2", "-verify"}, code: 1, want: []string{"NOT REPRODUCED"},
			check: func(t *testing.T, out string) {
				next := strings.Join(slcNext(out), " ")
				if !strings.Contains(next, "-keep writes") || !strings.Contains(next, "-var batch=<fresh>") || strings.Contains(next, "fill") || strings.Contains(next, "blank") {
					t.Fatalf("next %q", next)
				}
			}},
		{name: "an undeclared var a kept write interpolates needs a fresh value", setup: func(t *testing.T) { round2Workspace(t) },
			args: []string{"cli-r2-flow", "-step", "fetch", "-run", "latest", "-verify"}, code: 1, want: []string{"-var batch=<fresh>"}},
		{name: "-write never overwrites another chain", setup: func(t *testing.T) {
			round2Workspace(t)
			writeFile(t, ".shrt/scratch/probe.yaml", slcOtherChain)
		}, args: []string{"cli-r2-flow", "-step", "fetch", "-write", "probe"}, code: 1, want: []string{"name another file"},
			check: func(t *testing.T, _ string) {
				if raw := string(mustRead(t, ".shrt/scratch/probe.yaml")); raw != slcOtherChain {
					t.Fatalf("overwritten:\n%s", raw)
				}
				_ = os.Remove(".shrt/scratch/probe.yaml")
				for _, c := range []struct {
					args []string
					ok   bool
				}{{[]string{"-step", "fetch"}, true}, {[]string{"-step", "fetch", "-keep", "other"}, true}, {[]string{"-step", "blank"}, false}} {
					if out, code := slcSlice(t, false, append(append([]string{"cli-r2-flow"}, c.args...), "-write", "probe")...); (code == 0) != c.ok {
						t.Fatalf("%v: exit %d\n%s", c.args, code, out)
					}
				}
			}},

		{name: "-without names the failures the left-out write caused", setup: func(t *testing.T) { stockWorkspace(t, 0) },
			args: []string{"stock", "-without", "stray_add", "-verify"}, code: 1,
			want: []string{"1 of 2 step(s) that failed in source run", "pass without it: fetch_total\n", "still fail as they did, so another cause: fetch_name"}, not: []string{"not proof"}},
		{name: "-without is inconclusive when the steps still failing read what the left-out write writes", setup: func(t *testing.T) {
			srv := stockBackend(0)
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			writeFile(t, ".shrt/chains/stock.yaml", strings.NewReplacer("equals: 6", "equals: 99", "equals: gadget", "equals: widget", "trace_id: ${make.id}", "trace_id: ${make.id}\n              source: lost").Replace(stockChain))
			slcRunAny("stock", "-keep-going")
		}, args: []string{"stock", "-without", "stray_add", "-verify"}, code: 3,
			want: []string{"verify INCONCLUSIVE without stray_add: the 1 step(s) that failed", "still fail, but they read what the left-out steps write: fetch_total"}, not: []string{"NOT REPRODUCED"}},
		{name: "-without says the failures that persist are not the left-out step's", setup: func(t *testing.T) {
			srv := stockBackend(1)
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			writeFile(t, ".shrt/chains/stock.yaml", strings.NewReplacer("equals: DENIED", "equals: OK", "equals: 6", "equals: 15", "qty: 9", "qty: 0").Replace(stockChain))
			slcRunAny("stock", "-keep-going")
		}, args: []string{"stock", "-without", "stray_add", "-verify"}, code: 1,
			want: []string{"verify STILL FAILS without stray_add: the 2 step(s) that failed in source run ", " still fail exactly as they did (fetch_total, fetch_name), so stray_add is not their cause\n", "STILL FAILS without stray_add"},
			not:  []string{"NOT REPRODUCED", "FAILS DIFFERENTLY"}},
		{name: "-without says a read whose got flips fails differently, never that the write is not its cause", setup: func(t *testing.T) {
			srv := stockBackend(0)
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			writeFile(t, ".shrt/chains/stock.yaml", strings.NewReplacer("equals: DENIED", "equals: OK", "qty: 6", "qty: 3", "qty: 9", "qty: -6", "equals: 6", "equals: 0", "equals: gadget", "equals: widget").Replace(stockChain))
			slcRunAny("stock", "-keep-going")
		}, args: []string{"stock", "-without", "stray_add", "-verify"}, code: 3,
			want: []string{"verify FAILS DIFFERENTLY without stray_add: the 1 step(s) that failed in source run ", "still fail, but not as they did (fetch_total), so stray_add is involved",
				"fetch_total expectation 1 (total equals): failed in both, differently: source got -3, without it got 3\n", "next: shrt chain slice stock -step fetch_total -verify -run "},
			not: []string{"STILL FAILS", "not their cause"}},
		{name: "-without lists the steps that fail differently apart from those failing as they did", setup: func(t *testing.T) {
			srv := stockBackend(1)
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			writeFile(t, ".shrt/chains/stock.yaml", strings.NewReplacer("equals: DENIED", "equals: OK", "equals: 6", "equals: 15").Replace(stockChain))
			slcRunAny("stock", "-keep-going")
		}, args: []string{"stock", "-without", "stray_add", "-verify", "-json"}, code: 3,
			check: func(t *testing.T, out string) {
				var payload struct {
					Verify withoutVerdict `json:"verify"`
				}
				_ = json.NewDecoder(strings.NewReader(out)).Decode(&payload)
				v := payload.Verify
				if len(v.Cleared) != 0 || strings.Join(v.Changed, ",") != "fetch_total" || strings.Join(v.StillFail, ",") != "fetch_name" ||
					len(v.Changes) != 1 || !strings.Contains(v.Changes[0], "source got 16, without it got 7") {
					t.Fatalf("%+v\n%s", v, out)
				}
			}},
		{name: "-without names the steps that need the left-out step", setup: func(t *testing.T) {
			b := lifecycleWorkspace(t)
			b.cancelBug = true
			slcRunAny("life", "-keep-going")
		}, args: []string{"life", "-without", "confirm", "-verify"},
			want: []string{"1 of 1 step(s) that failed in source run", "pass without it: cancel (it acts on the record confirm changed and may need that state: not proof confirm is the cause)\n", "fail only without it: fetch_confirmed"}},

		{name: "an identical re-write keeps a verified slice's verdict", setup: slcVerifiedProbe,
			args: []string{"cli-thing-flow", "-step", "fetch", "-write", "probe"},
			check: func(t *testing.T, _ string) {
				if raw := slcHas(t, ".shrt/scratch/probe.yaml", "VERIFIED by"); strings.Contains(raw, "HYPOTHESIS") {
					t.Fatalf("%s", raw)
				}
			}},
		{name: "a different slice never replaces a verified one", setup: func(t *testing.T) {
			slcVerifiedProbe(t)
			writeFile(t, ".shrt/chains/cli-thing-flow.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml")), "equals: widget", "equals: gadget", 1))
		}, args: []string{"cli-thing-flow", "-step", "fetch", "-write", "probe"}, code: 1, want: []string{"VERIFIED"},
			check: func(t *testing.T, _ string) { slcHas(t, ".shrt/scratch/probe.yaml", "VERIFIED by") }},
		{name: "the configured login is no dropped write", setup: func(t *testing.T) {
			chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
			appendAuth(t, "${env.WIDGET_USER}")
			writeFile(t, ".shrt/chains/cli-login-flow.yaml", slcLoginChain)
		}, args: []string{"cli-login-flow", "-step", "fetch", "-v"},
			check: func(t *testing.T, out string) {
				dropped := out[strings.Index(out, "dropped write steps:"):]
				if strings.Contains(dropped, " login ") || strings.Contains(out, "1  login") || !strings.Contains(dropped, "partner_login") {
					t.Fatalf("%s", out)
				}
			}},
		{name: "-run latest slices the newest drifted replay", setup: func(t *testing.T) {
			total = 2
			srv := newTotallingBackend(&total)
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			fixApprove(t, "cli-thing-flow")
			total = 1
			for range 2 {
				captureStdout(t, func() { _ = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
			}
		}, args: []string{"cli-thing-flow", "-step", "fetch", "-run", "latest", "-verify"},
			want: []string{"(a shrt verify replay)", "note: -run latest: verify replay ", "drifted: total source got=1, slice got=1", "verify reproduced"},
			check: func(t *testing.T, _ string) {
				total = 2
				if out, code := slcSlice(t, false, "cli-thing-flow", "-step", "fetch", "-run", "latest", "-verify"); code != 1 || !strings.Contains(out, "NOT REPRODUCED") || !strings.Contains(out, "total (") || !strings.Contains(out, "the slice run did not") {
					t.Fatalf("exit %d:\n%s", code, out)
				}
			}},
		{name: "a passing target that drifts in the slice only is not reproduced", setup: func(t *testing.T) {
			total = 2
			srv := newTotallingBackend(&total)
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			fixApprove(t, "cli-thing-flow")
			slcMustRun(t, "cli-thing-flow")
			total = 1
		}, args: []string{"cli-thing-flow", "-step", "fetch", "-run", "latest", "-verify"}, code: 1,
			want: []string{"NOT REPRODUCED", "the slice run changed total (changed)"},
			check: func(t *testing.T, _ string) {
				var spot struct {
					RunID string `json:"run_id"`
				}
				if err := json.Unmarshal(mustRead(t, ".shrt/safespots/cli-thing-flow.json"), &spot); err != nil || spot.RunID == "" {
					t.Fatalf("safe spot run id: %v", err)
				}
				if err := os.Remove(filepath.Join(".shrt/runs/cli-thing-flow", spot.RunID+".json")); err != nil {
					t.Fatal(err)
				}
				if out, code := slcSlice(t, false, "cli-thing-flow", "-step", "fetch", "-run", "latest", "-verify"); code != 3 || !strings.Contains(out, "verify INCONCLUSIVE") || !strings.Contains(out, "cannot be loaded") {
					t.Fatalf("exit %d:\n%s", code, out)
				}
			}},
		{name: "a step refused for its token's age is reached and says why it differs", setup: func(t *testing.T) {
			shortSessionWorkspace(t, &shortSessionBackend{uses: 1, short: true}, lifetimeWrites)
			slcRunAny("cli-thing-flow")
		}, args: []string{"cli-thing-flow", "-step", "create_again", "-run", "latest", "-verify"}, code: 1,
			want: []string{"NOT REPRODUCED", "the slice's younger token was accepted"}, not: []string{"reached step", "did not reach"}},
		{name: "a kept write refused but acting on the target's entity is kept", setup: slcLeaky("REJECTED"),
			args: []string{"cli-leaky", "-step", "fetch", "-run", "latest", "-verify", "-v"}, want: []string{"verify reproduced", "take"}, not: []string{"act on no entity"}},
		{name: "-keep writes keeps a refused write", setup: slcLeaky("REJECTED"),
			args: []string{"cli-leaky", "-step", "fetch", "-run", "latest", "-verify", "-v", "-keep", "writes"}, want: []string{"take", "kept by -keep writes"}},
		{name: "a kept write refused against its expectation is relaxed", setup: slcLeaky("OK"),
			args: []string{"cli-leaky", "-step", "fetch", "-run", "latest", "-verify", "-v"}, want: []string{"verify reproduced", "take error.code equals"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.setup(t)
			out, code := slcSlice(t, c.red, c.args...)
			if code != c.code {
				t.Fatalf("exit %d, want %d:\n%s", code, c.code, out)
			}
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Fatalf("want %q in:\n%s", w, out)
				}
			}
			for _, n := range c.not {
				if strings.Contains(out, n) {
					t.Fatalf("unwanted %q in:\n%s", n, out)
				}
			}
			if c.check != nil {
				c.check(t, out)
			}
		})
	}
}

func slcLeaky(takeExpects string) func(t *testing.T) {
	return func(t *testing.T) {
		srv := newLeakyRefusalBackend()
		t.Cleanup(srv.Close)
		chdirToFreshCLIWorkspace(t, srv.URL)
		writeFile(t, ".shrt/chains/cli-leaky.yaml", leakyRefusalChain(takeExpects))
		slcRunAny("cli-leaky", "-keep-going")
	}
}

func slcVerifiedProbe(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	slcMustRun(t, "cli-thing-flow")
	if out, code := slcSlice(t, false, "cli-thing-flow", "-step", "fetch", "-run", "latest", "-verify", "-write", "probe"); code != 0 {
		t.Fatalf("%s", out)
	}
	slcHas(t, ".shrt/scratch/probe.yaml", "VERIFIED by")
}

func TestSliceExitCodesAreDocumented(t *testing.T) {
	if !strings.Contains(sliceExitCodes, "  3  ") || !strings.Contains(sliceExitCodes, "DID NOT RUN") || strings.Contains(sliceExitCodes, "  2  ") {
		t.Errorf("chain slice -h must state the 0/1/3 exit codes:\n%s", sliceExitCodes)
	}
	raw, _ := coredistillation.Docs.ReadFile("README.md")
	if _, row, _ := strings.Cut(string(raw), "| `shrt chain slice <c> -step <id>` |"); !strings.Contains(row, "DID NOT RUN") {
		t.Error("README's command table must give chain slice its exit codes")
	}
}

func TestCLISliceLatestRefusesWhenTheNewestRunDidNotReachTheStep(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeEnvFetchChain(t)
	t.Setenv("SHRT_LAB5_NAME", "thing-1")
	slcMustRun(t, "cli-env-flow")
	reaching := runIDsOf(t, "cli-env-flow")[0]
	writeFile(t, ".shrt/chains/cli-env-flow.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-env-flow.yaml")), "equals: OK", "equals: NOT_THIS", 1))
	slcRunAny("cli-env-flow")
	writeEnvFetchChain(t)
	ids := runIDsOf(t, "cli-env-flow")
	stopped := ids[0]
	if stopped == reaching {
		stopped = ids[1]
	}
	out, code := slcSlice(t, false, "cli-env-flow", "-step", "fetch", "-run", "latest", "-verify")
	if code != 3 {
		t.Fatalf("-run latest never falls back to an older run (exit %d):\n%s", code, out)
	}
	for _, want := range []string{stopped, "step fetch", "-step create -run " + stopped, "-run " + reaching} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal must name %q:\n%s", want, out)
		}
	}
	if out, code := slcSlice(t, false, "cli-env-flow", "-step", "fetch", "-run", stopped, "-verify"); code == 0 || !strings.Contains(out, "-run "+reaching) {
		t.Fatalf("an explicit run that never reached the step is refused, naming one that did:\n%s", out)
	}
}

func TestWhichNamesTheUpstreamStepAndTheSliceThatEvaluatesIt(t *testing.T) {
	blockedWorkspace(t)
	out := whichOut(t, "-rpc", "ThingService/Fetch")
	for _, want := range []string{"not evaluated: it reads step fetch, which failed in run", "shrt chain slice cli-blocked -step fetch_again -run ", " -verify"} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q in:\n%s", want, out)
		}
	}
}

func TestCLIWhichSeesTransportRefusedStepsAndPrintsEvidenceOnItsOwnLine(t *testing.T) {
	round2Workspace(t)
	out := whichOut(t, "-code", "invalid_argument")
	if !strings.Contains(whichLine(t, out, "blank"), "got invalid_argument, step passed") {
		t.Fatalf("-code must find transport.code and the refused run did reach the step:\n%s", out)
	}
	out = whichOut(t, "-rpc", "ThingService/Create")
	if strings.Contains(out, "no local run reached it") || !strings.Contains(out, "-var batch=<fresh>") {
		t.Fatalf("a transport-refused step that passed was reached, and its reproduce line asks for a fresh batch:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, " got ") && !strings.HasPrefix(l, "    run ") {
			t.Errorf("run evidence goes on its own line: %q", l)
		}
	}
}

const slcTrioChain = `apiVersion: shrt/v1
name: cli-trio
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: look
      call: ThingService/Fetch
      body:
          id: thing-0
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: id
            equals: thing-0
`

const slcGotChain = `apiVersion: shrt/v1
name: cli-got-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: thing-999
`

const slcOtherChain = "apiVersion: shrt/v1\nname: probe\nsteps:\n    - id: only\n      call: ThingService/Fetch\n      body:\n          id: x\n"

const slcLoginChain = `apiVersion: shrt/v1
name: cli-login-flow
steps:
    - id: login
      call: shrt.test.v1.AuthService/Login
      skip_auth: true
      body:
          username: someone
          password: secret
    - id: partner_login
      call: PartnerAuthService/Login
      skip_auth: true
      body:
          username: someone
          password: secret
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
`

func TestOwnsFieldNeedsTheFieldOnTheEntityTheReaderUses(t *testing.T) {
	decode := func(raw string) any {
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	batch := decode(`{"status":{"code":"SUCCESS"},"results":[{"status":{"code":"SUCCESS"},"id_product":"p1","qty_on_hand":4}]}`)
	order := decode(`{"status":{"code":"SUCCESS"},"order":{"id_order":"o1","id_customer":"c1","total_minor":2500,"note":""}}`)
	for _, c := range []struct {
		v     any
		field string
		ids   map[string]bool
		want  bool
	}{
		{batch, "qty_on_hand", map[string]bool{"p1": true}, true},
		{batch, "qty_on_hand", map[string]bool{"p2": true}, false},
		{order, "total_minor", map[string]bool{"c1": true}, false},
		{order, "total_minor", map[string]bool{"o1": true}, true},
		{order, "note", nil, false},
		{order, "total_minor", nil, true},
	} {
		if got := ownsField(c.v, "", c.field, c.ids); got != c.want {
			t.Errorf("%s with %v: got %v, want %v", c.field, c.ids, got, c.want)
		}
	}
}

func TestSliceHelpSaysTheWordWritesCombinesWithIds(t *testing.T) {
	if out := helpOf(t, "chain", "slice"); !strings.Contains(out, "the word writes keeps every earlier write and combines with ids: -keep writes,<id>") {
		t.Fatalf("slice -h:\n%s", out)
	}
}
