package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

type gateRead struct {
	Procedure string          `json:"procedure"`
	Body      json.RawMessage `json:"body,omitempty"`
}

type gateEarly struct {
	age, stated time.Duration
}

var sessionHoldCap = 30 * time.Second

func sessionReads(e *env, rec *runner.Record) map[string]gateRead {
	if e == nil || e.cat == nil || rec == nil || rec.DryRun {
		return nil
	}
	out := map[string]gateRead{}
	for _, st := range rec.Steps {
		if st == nil || st.Status != runner.StatusPassed || !chain.IsReadOnlyCall(st.Call) || len(st.Headers) > 0 ||
			st.AuthProfile == "" || st.AuthProfile == runner.NoAuthProfile ||
			st.AuthProfile == transport.InvalidTokenProfile || bytes.Contains(st.Request, []byte(pathmask.MaskRedacted)) {
			continue
		}
		if _, ok := out[st.AuthProfile]; ok {
			continue
		}
		if m, err := e.cat.Lookup(st.Call); err != nil || m.Streaming() {
			continue
		}
		out[st.AuthProfile] = gateRead{Procedure: st.Procedure, Body: st.Request}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type sessionCheck struct {
	line    string
	finding bool
}

func checkSessions(ctx context.Context, e *env, profiles []string, at map[string]gateEarly, reads map[string]gateRead, verbose bool) map[string]sessionCheck {
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := map[string]sessionCheck{}
	held, longest := []string{}, time.Duration(0)
	for _, p := range profiles {
		if _, ok := reads[p]; ok && at[p].age > 0 && at[p].stated > 0 && e.cat != nil {
			held, longest = append(held, p), max(longest, sessionHold(at[p]))
		}
	}
	switch len(held) {
	case 0:
	case 1:
		fmt.Printf("checking session lifetime of auth profile %s: holding a fresh token %s (-no-session-check skips this)\n", held[0], ageText(longest))
	default:
		fmt.Printf("checking session lifetime of auth profiles %s: holding fresh tokens up to %s (-no-session-check skips this)\n", strings.Join(held, ", "), ageText(longest))
	}
	for _, p := range profiles {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if line, finding, ok := checkSession(ctx, e, p, at[p], reads, verbose); ok {
				mu.Lock()
				out[p] = sessionCheck{line: line, finding: finding}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

func checkSession(ctx context.Context, e *env, profile string, at gateEarly, reads map[string]gateRead, verbose bool) (string, bool, bool) {
	read, ok := reads[profile]
	if !ok || at.age <= 0 || at.stated <= 0 || e.cat == nil {
		return "", false, false
	}
	hold := sessionHold(at)
	half := hold / 2
	first, ok := heldProbes(ctx, e, profile, read, hold)
	if !ok {
		return "", false, false
	}
	if first[0] {
		of := ""
		if profile != "default" {
			of = " of auth profile " + profile
		}
		line := fmt.Sprintf("session check: early token refusal%s was a restart (fresh token held %s accepted)", of, ageText(hold))
		if hold <= at.age && verbose {
			line += fmt.Sprintf("; sessions between %s and %s are not ruled out, and a repeat in a later gate is reported as a FINDING",
				ageText(hold), ageText(at.age))
		}
		return line, false, true
	}
	second, ok := heldProbes(ctx, e, profile, read, half, hold)
	switch {
	case !ok:
		return "", false, false
	case !second[0]:
		return fmt.Sprintf("FINDING: sessions of auth profile %s end early: fresh tokens were refused at %s and at %s although the login said %s",
			profile, ageText(hold), ageText(half), ageText(at.stated)), true, true
	case !second[1]:
		return fmt.Sprintf("FINDING: sessions of auth profile %s end early: fresh tokens were accepted at %s, refused at %s (twice) although the login said %s",
			profile, ageText(half), ageText(hold), ageText(at.stated)), true, true
	}
	return fmt.Sprintf("session check: the early refusal of auth profile %s was a restart: a fresh token held %s was refused once, "+
		"then fresh ones held %s and %s were accepted", profile, ageText(hold), ageText(half), ageText(hold)), false, true
}

func sessionHold(at gateEarly) time.Duration {
	return min(at.age+time.Second, sessionHoldCap)
}

func heldProbes(ctx context.Context, e *env, profile string, read gateRead, ages ...time.Duration) ([]bool, bool) {
	type held struct {
		deps   *runner.Deps
		issued time.Time
	}
	tokens := make([]held, 0, len(ages))
	for range ages {
		deps, err := runner.Build(ctx, e.cfg, e.cat, nil)
		if err != nil || deps.Sources[profile] == nil {
			return nil, false
		}
		src := deps.Sources[profile]
		src.Invalidate()
		if _, err := src.Token(ctx); err != nil {
			return nil, false
		}
		tokens = append(tokens, held{deps: deps, issued: time.Now()})
	}
	out := make([]bool, 0, len(ages))
	for i, age := range ages {
		gateSleep(ctx, time.Until(tokens[i].issued.Add(age)))
		call := &transport.Call{Procedure: read.Procedure, Body: read.Body, Meta: map[string]any{"auth": profile}}
		if _, err := tokens[i].deps.Client.Do(ctx, call); err != nil || ctx.Err() != nil {
			return nil, false
		}
		refused, _ := call.Meta[transport.MetaAuthTokenRefused].([]transport.TokenRefusal)
		out = append(out, len(refused) == 0)
		if len(refused) > 0 && i < len(ages)-1 {
			for len(out) < len(ages) {
				out = append(out, false)
			}
			break
		}
	}
	return out, true
}

func ageText(d time.Duration) string {
	if d < 10*time.Second {
		return strconv.FormatFloat(d.Round(100*time.Millisecond).Seconds(), 'f', -1, 64) + "s"
	}
	return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
}

func gateCoverage(e *env) string {
	lib, err := e.library()
	if err != nil || e.cat == nil {
		return ""
	}
	if len(lib.Overlays) == 0 {
		n := 0
		for _, m := range e.cat.Methods() {
			if m.StreamRefusal() == "" {
				n++
			}
		}
		if n == 0 {
			return ""
		}
		return fmt.Sprintf("coverage: %d rpc(s) have no contract, so no planned probes (boundaries, other roles, missing tokens, "+
			"read-backs); shrt contract init -all, then shrt contract plan -all -write", n)
	}
	called := map[string]bool{}
	chains, _, _ := chain.LoadDirPartial(e.chainsDir())
	for _, c := range chains {
		for _, s := range c.Steps {
			if m, err := e.cat.Lookup(s.Call); err == nil {
				called[m.FullName] = true
			}
		}
	}
	n := 0
	for _, rpc := range lib.RPCs() {
		if m, err := e.cat.Lookup(rpc); err == nil && m.StreamRefusal() == "" && !called[m.FullName] {
			n++
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("coverage: %d rpc(s) with a contract have no chain calling them, so none of their planned probes run: "+
		"shrt contract plan -all -write", n)
}
