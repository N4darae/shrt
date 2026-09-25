package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

const sessionHoldCap = 90 * time.Second

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

func checkSession(ctx context.Context, e *env, profile string, at gateEarly, reads map[string]gateRead) (string, bool, bool) {
	read, ok := reads[profile]
	if !ok || at.age <= 0 || at.stated <= 0 || e.cat == nil {
		return "", false, false
	}
	deps, err := runner.Build(ctx, e.cfg, e.cat, nil)
	if err != nil || deps.Sources[profile] == nil {
		return "", false, false
	}
	src := deps.Sources[profile]
	hold := min(at.age, sessionHoldCap) + time.Second
	fmt.Printf("checking session lifetime: holding a fresh token %ds\n", seconds(hold))
	src.Invalidate()
	if _, err := src.Token(ctx); err != nil {
		return "", false, false
	}
	issued := time.Now()
	var held time.Duration
	for try := 1; try <= 2; try++ {
		gateSleep(ctx, time.Until(issued.Add(hold)))
		call := &transport.Call{Procedure: read.Procedure, Body: read.Body, Meta: map[string]any{"auth": profile}}
		held = time.Since(issued)
		_, err := deps.Client.Do(ctx, call)
		if err != nil || ctx.Err() != nil {
			return "", false, false
		}
		if refused, _ := call.Meta[transport.MetaAuthTokenRefused].([]transport.TokenRefusal); len(refused) == 0 {
			return fmt.Sprintf("session check: the early refusal of auth profile %s was a restart: a fresh token held %ds was accepted",
				profile, seconds(held)), false, true
		}
		issued = time.Now()
	}
	return fmt.Sprintf("FINDING: sessions end early: a fresh token was refused after %ds although the login said %ds",
		seconds(held), seconds(at.stated)), true, true
}

func seconds(d time.Duration) int {
	return int(d.Round(time.Second) / time.Second)
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
