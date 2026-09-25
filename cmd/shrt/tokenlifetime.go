package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

type earlyRefusal struct {
	step  *runner.StepRecord
	index int
	r     transport.TokenRefusal
}

type tokenLifetime struct {
	first   earlyRefusal
	again   *earlyRefusal
	prevRun string
	prev    *earlyRefusal
	restart string
	relogin []time.Time
}

const reloginWindow = 2 * time.Minute

func earlyRefusals(rec *runner.Record) []earlyRefusal {
	if rec == nil || rec.DryRun {
		return nil
	}
	var out []earlyRefusal
	seen := map[string]bool{}
	for i, st := range rec.Steps {
		if st == nil {
			continue
		}
		for _, r := range st.TokenRefused {
			if !r.Early() || r.FirstUse && !r.Cached || seen[r.Token] {
				continue
			}
			seen[r.Token] = true
			out = append(out, earlyRefusal{step: st, index: i, r: r})
		}
	}
	return out
}

func examineTokenLifetime(e *env, rec *runner.Record) *tokenLifetime {
	all := earlyRefusals(rec)
	if len(all) == 0 {
		return nil
	}
	t := &tokenLifetime{first: all[0]}
	t.restart = sessionRestartEvidence(rec, t.first.index)
	for i := 1; i < len(all); i++ {
		if all[i].step.AuthProfile == t.first.step.AuthProfile {
			again := all[i]
			t.again = &again
			break
		}
	}
	if r := t.first.r; t.restart == "" && t.again == nil && r.Cached && r.FirstUse {
		t.relogin = reloginChain(r)
	}
	if t.restart != "" || t.again != nil || len(t.relogin) > 0 || e == nil {
		return t
	}
	mine := firstInRun(all)
	if mine == nil {
		return t
	}
	prev := previousRecord(e, rec)
	if prev == nil || sessionRestartEvidence(prev, 0) != "" {
		return t
	}
	for _, p := range earlyRefusals(prev) {
		if !p.r.FirstUse && p.step.AuthProfile == mine.step.AuthProfile && sessionRestartEvidence(prev, p.index) == "" {
			t.first = *mine
			t.prev, t.prevRun = &p, prev.RunID
			break
		}
	}
	return t
}

func reloginChain(r transport.TokenRefusal) []time.Time {
	at := r.RefusedAt
	for i := len(r.Relogins) - 1; i >= 0; i-- {
		if at.Sub(r.Relogins[i]) > reloginWindow {
			return r.Relogins[i+1:]
		}
		at = r.Relogins[i]
	}
	return r.Relogins
}

func clock(t time.Time) string {
	return t.UTC().Format("15:04:05Z")
}

func refusedEarly(st *runner.StepRecord) bool {
	for _, r := range st.TokenRefused {
		if r.Early() && !(r.FirstUse && !r.Cached) {
			return true
		}
	}
	return false
}

func firstInRun(all []earlyRefusal) *earlyRefusal {
	for i := range all {
		if !all[i].r.FirstUse {
			return &all[i]
		}
	}
	return nil
}

func previousRecord(e *env, rec *runner.Record) *runner.Record {
	ids, _ := e.store.ListRuns(rec.Chain)
	var best *runner.Record
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] == rec.RunID {
			continue
		}
		if best != nil && runStamp(ids[i]) < runStamp(best.RunID) {
			break
		}
		prev, err := loadRunNamedAs(e, rec, ids[i])
		if err != nil || prev.DryRun || !ranBefore(prev, rec) {
			continue
		}
		if best == nil || ranBefore(best, prev) {
			best = prev
		}
	}
	return best
}

func sessionRestartEvidence(rec *runner.Record, index int) string {
	if strings.Contains(rec.Build, " -> ") {
		return fmt.Sprintf("The server's build changed during the run (%s)", rec.Build)
	}
	for _, st := range rec.Steps[:index] {
		if unansweredCall(st) {
			return fmt.Sprintf("Step %s before it got no answer from the service", st.ID)
		}
	}
	scan := newPriorScan(rec, index)
	for _, st := range rec.Steps[index+1:] {
		if st == nil || st.Status == runner.StatusSkipped || st.HTTPStatus == 0 && len(st.Response) == 0 {
			continue
		}
		if unansweredCall(st) {
			return fmt.Sprintf("Step %s after it got no answer from the service", st.ID)
		}
		if prior := scan.shrunkList(st); prior != "" {
			return fmt.Sprintf("Data created before it was gone after the re-login (step %s lists fewer items than step %s did before the refusal)", st.ID, prior)
		}
		if scan.conflictVanished(st) {
			return fmt.Sprintf("Data created before it was gone after the re-login (step %s expected a refusal over a value created before it and was accepted)", st.ID)
		}
		why := stepRefusalText(st)
		if why == "" || st.Status == runner.StatusPassed || st.Transport != nil && strings.EqualFold(st.Transport.Code, "unauthenticated") {
			continue
		}
		for _, value := range scan.createdValues(st) {
			if strings.Contains(why, value) || notFound(why) {
				return fmt.Sprintf("Data created before it was gone after the re-login (step %s: %s)", st.ID, why)
			}
		}
	}
	return ""
}

func (t *tokenLifetime) finding() bool {
	return t != nil && t.restart == "" && (t.again != nil || t.prev != nil || len(t.relogin) > 1)
}

func (t *tokenLifetime) cachedFirstUse() bool {
	return t != nil && !t.finding() && t.restart == "" && t.first.r.Cached && t.first.r.FirstUse
}

func (t *tokenLifetime) profile(x earlyRefusal) string {
	if x.step.AuthProfile == "" {
		return transport.DefaultProfile
	}
	return x.step.AuthProfile
}

func (t *tokenLifetime) where(x earlyRefusal) string {
	profile := t.profile(x)
	whose := "a token a login in this run issued"
	if x.r.Cached {
		whose = "the cached token, on its first use in this run"
		if !x.r.FirstUse {
			whose = "the cached token, accepted earlier in this run"
		}
	}
	return fmt.Sprintf("auth profile %s, %s, at step %d %s", profile, whose, x.step.Index, x.step.ID)
}

func (t *tokenLifetime) line() string {
	head := fmt.Sprintf("%s (%s)", runner.TokenRefusalPhrase(t.first.r), t.where(t.first))
	switch {
	case t.restart != "":
		return head + fmt.Sprintf(". %s, which a restart explains, so this is not a finding", t.restart)
	case t.again != nil:
		return head + fmt.Sprintf(", and the fresh token the re-login issued was %s (step %d %s): "+
			"a single restart does not end two sessions issued on either side of it, and nothing in this run shows a restart, "+
			"so the backend ends sessions long before the expiry its login states. This is a finding about the backend",
			strings.TrimPrefix(runner.TokenRefusalPhrase(t.again.r), "token "), t.again.step.Index, t.again.step.ID)
	case len(t.relogin) > 1:
		return fmt.Sprintf("%s (auth profile %s, step %s); it was issued by the re-login after the refusal at %s, whose token "+
			"the re-login after the refusal at %s had issued: one restart does not explain three refusals in a row, so the "+
			"backend ends sessions long before the expiry its login states", runner.TokenRefusalPhrase(t.first.r),
			t.profile(t.first), t.first.step.ID, clock(t.relogin[1]), clock(t.relogin[0]))
	case t.prev != nil:
		return head + fmt.Sprintf(", and in run %s the token that run's login issued was %s (step %d %s): "+
			"a restart would have to land inside both runs, and neither shows one, so the backend ends sessions long before the "+
			"expiry its login states. This is a finding about the backend", t.prevRun,
			strings.TrimPrefix(runner.TokenRefusalPhrase(t.prev.r), "token "), t.prev.step.Index, t.prev.step.ID)
	}
	if t.cachedFirstUse() && len(t.relogin) == 1 {
		return fmt.Sprintf("cached %s (auth profile %s, step %s): possibly a second restart since the re-login after the refusal "+
			"at %s issued it; logged in again, and refused early once more it is a FINDING",
			runner.TokenRefusalPhrase(t.first.r), t.profile(t.first), t.first.step.ID, clock(t.relogin[0]))
	}
	if t.cachedFirstUse() {
		return fmt.Sprintf("cached %s (auth profile %s, step %s): possibly a restart since the token was cached; logged in again",
			runner.TokenRefusalPhrase(t.first.r), t.profile(t.first), t.first.step.ID)
	}
	again := " Refused early again, a token this run's login issued or the one the re-login issued, it is reported as a finding"
	return head + ": the backend ends sessions long before the expiry its login states, or it restarted since that login; " +
		"nothing in this run shows a restart." + again
}

func (t *tokenLifetime) label() string {
	if t.finding() {
		return "FINDING: "
	}
	if t.cachedFirstUse() {
		return "note: "
	}
	return "WARNING: "
}
