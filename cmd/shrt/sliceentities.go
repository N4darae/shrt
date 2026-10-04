package main

import (
	"encoding/json"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func relaxableIn(rec *runner.Record) func(string) bool {
	refused := refusedIn(rec)
	return func(id string) bool {
		sr, ok := rec.Step(id)
		if !ok || sr.Status != runner.StatusFailed || sr.Transport != nil {
			return false
		}
		if sr.HTTPStatus == 0 && len(sr.Response) == 0 {
			return false
		}
		if _, wroteNothing := refused(id); wroteNothing && chain.IsReadOnlyCall(sr.Call) {
			return false
		}
		failed := 0
		for _, e := range sr.Expect {
			if e.Passed {
				continue
			}
			if strings.HasPrefix(e.Path, "transport.") || e.Rule == "invalid" || strings.Contains(e.Detail, "not evaluated") {
				return false
			}
			failed++
		}
		return failed > 0
	}
}

func relaxIn(rec *runner.Record) func(string) []chain.ExpectResult {
	relaxable := relaxableIn(rec)
	return func(id string) []chain.ExpectResult {
		if !relaxable(id) {
			return nil
		}
		sr, _ := rec.Step(id)
		return sr.Expect
	}
}

func isIDKey(k string) bool {
	l := strings.ToLower(k)
	if l == "id" || strings.HasPrefix(l, "id_") || strings.HasSuffix(l, "_id") {
		return true
	}
	if len(k) > 2 && strings.HasPrefix(k, "id") && k[2] >= 'A' && k[2] <= 'Z' {
		return true
	}
	return len(k) > 2 && strings.HasSuffix(k, "Id")
}

func collectIDs(v any, into map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, item := range t {
			if s, ok := item.(string); ok && isIDKey(k) && s != "" {
				into[s] = true
				continue
			}
			collectIDs(item, into)
		}
	case []any:
		for _, item := range t {
			collectIDs(item, into)
		}
	}
}

type primaryEntity struct {
	key string
	id  string
	obj string
}

func primaryIDKey(field string, obj map[string]any) string {
	for _, k := range []string{"id_" + field, field + "_id", "id"} {
		if s, ok := obj[k].(string); ok && s != "" {
			return k
		}
	}
	found := ""
	for k, v := range obj {
		if s, ok := v.(string); ok && s != "" && isIDKey(k) {
			if found != "" {
				return ""
			}
			found = k
		}
	}
	return found
}

func canonicalEntity(obj map[string]any) string {
	clean := map[string]any{}
	for k, v := range obj {
		if strings.HasSuffix(strings.ToLower(k), "_at") {
			continue
		}
		clean[k] = v
	}
	raw, _ := json.Marshal(clean)
	return string(raw)
}

func primaryEntities(resp any) []primaryEntity {
	top, ok := resp.(map[string]any)
	if !ok {
		return nil
	}
	out := []primaryEntity{}
	for _, k := range sortedKeys(top) {
		switch t := top[k].(type) {
		case string:
			if isIDKey(k) && t != "" {
				out = append(out, primaryEntity{key: k, id: t, obj: canonicalEntity(top)})
			}
		case map[string]any:
			if idKey := primaryIDKey(k, t); idKey != "" {
				out = append(out, primaryEntity{key: idKey, id: t[idKey].(string), obj: canonicalEntity(t)})
			}
		}
	}
	return out
}

func objectsWithID(v any, key, id string, into *[]string) {
	switch t := v.(type) {
	case map[string]any:
		if s, ok := t[key].(string); ok && s == id {
			*into = append(*into, canonicalEntity(t))
		}
		for _, item := range t {
			objectsWithID(item, key, id, into)
		}
	case []any:
		for _, item := range t {
			objectsWithID(item, key, id, into)
		}
	}
}

type stepEntityFacts struct {
	mentions map[string]bool
	acts     map[string]bool
	known    bool
}

func decodedRecordStep(sr *runner.StepRecord) (any, any) {
	var request, response any
	if len(sr.Request) > 0 {
		_ = json.Unmarshal(sr.Request, &request)
	}
	if len(sr.Response) > 0 {
		_ = json.Unmarshal(sr.Response, &response)
	}
	return request, response
}

func entityFactsOf(rec *runner.Record, id string) stepEntityFacts {
	return entityFactsWith(rec, id, false)
}

func entityFactsWith(rec *runner.Record, id string, stateWriter bool) stepEntityFacts {
	f := stepEntityFacts{mentions: map[string]bool{}, acts: map[string]bool{}}
	at := -1
	for i, sr := range rec.Steps {
		if sr.ID == id {
			at = i
			break
		}
	}
	if at < 0 {
		return f
	}
	sr := rec.Steps[at]
	request, response := decodedRecordStep(sr)
	collectIDs(request, f.mentions)
	collectIDs(response, f.mentions)
	if prim := primaryEntities(response); len(prim) > 0 {
		f.known = true
		named := map[string]bool{}
		collectIDs(request, named)
		for _, p := range prim {
			if replayedEarlier(rec, at, sr.Call, p) {
				continue
			}
			f.acts[p.id] = true
			if stateWriter && named[p.id] {
				for _, obj := range entityObjects(response, p) {
					collectIDs(obj, f.acts)
				}
			}
		}
		return f
	}
	collectIDs(request, f.acts)
	f.known = len(f.acts) > 0
	return f
}

func entityObjects(v any, p primaryEntity) []any {
	out := []any{}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if s, ok := t[p.key].(string); ok && s == p.id {
				out = append(out, t)
				return
			}
			for _, item := range t {
				walk(item)
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	walk(v)
	return out
}

func replayedEarlier(rec *runner.Record, at int, call string, p primaryEntity) bool {
	for j := at - 1; j >= 0; j-- {
		prev := rec.Steps[j]
		if prev.Call != call {
			continue
		}
		_, response := decodedRecordStep(prev)
		seen := []string{}
		objectsWithID(response, p.key, p.id, &seen)
		for _, obj := range seen {
			if obj == p.obj {
				return true
			}
		}
	}
	return false
}

func relatedDroppedWrites(res *chain.SliceResult, rec *runner.Record) ([]string, []string) {
	if rec == nil || len(res.DroppedWrites) == 0 {
		return nil, nil
	}
	used := map[string]bool{}
	for _, k := range res.Kept {
		for id := range entityFactsOf(rec, k.ID).mentions {
			used[id] = true
		}
	}
	related := map[string]bool{}
	for _, d := range res.DroppedWrites {
		if createsListedChild(rec, d.ID, res) {
			related[d.ID] = true
			for id := range entityFactsOf(rec, d.ID).mentions {
				used[id] = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, d := range res.DroppedWrites {
			if related[d.ID] {
				continue
			}
			f := entityFactsWith(rec, d.ID, res.StateWriters[d.ID])
			touches := !f.known
			for id := range f.acts {
				if used[id] {
					touches = true
				}
			}
			if !touches {
				continue
			}
			related[d.ID] = true
			changed = true
			for id := range f.mentions {
				used[id] = true
			}
		}
	}
	rel, other := []string{}, []string{}
	for _, d := range res.DroppedWrites {
		if related[d.ID] {
			rel = append(rel, d.ID)
		} else {
			other = append(other, d.ID)
		}
	}
	return rel, other
}

func stoppedWhereSourcePassed(replay, source *runner.Record) string {
	relaxable := relaxableIn(source)
	for _, sr := range replay.Steps {
		if !failing(sr) {
			continue
		}
		was, ok := source.Step(sr.ID)
		if ok && (was.Status == runner.StatusPassed || relaxable(sr.ID)) {
			return sr.ID
		}
		return ""
	}
	return ""
}

func keptStepsBroken(replay, source *runner.Record, target string) []string {
	var out []string
	for _, sr := range replay.Steps {
		if sr.ID == target || !failing(sr) {
			continue
		}
		if was, ok := source.Step(sr.ID); ok && was.Status == runner.StatusPassed {
			out = append(out, sr.ID)
		}
	}
	return out
}

func sliceCollisionNote(e *env, res *chain.SliceResult, replay *runner.Record) string {
	if literal := detectLiteralCollision(e, res.Chain, replay); literal != nil {
		return literal.line() + "."
	}
	reuse := detectFixtureReuse(e, res.Chain, replay)
	if reuse == nil {
		return ""
	}
	if reuse.finding() {
		return "FINDING: " + reuse.line() + "."
	}
	if len(reuse.vars) == 0 {
		return reuse.line() + ". A plain re-run of the slice generates a fresh value."
	}
	return reuse.line() + ".\nRe-run the slice with a fresh value: " + reuse.fresh()
}

func createsListedChild(rec *runner.Record, writeID string, res *chain.SliceResult) bool {
	sr, ok := rec.Step(writeID)
	if !ok {
		return false
	}
	request, response := decodedRecordStep(sr)
	parents := map[string]bool{}
	collectIDs(request, parents)
	prim := primaryEntities(response)
	if len(parents) == 0 || len(prim) == 0 {
		return false
	}
	for _, k := range res.Kept {
		ks, ok := rec.Step(k.ID)
		if !ok {
			continue
		}
		keptRequest, keptResponse := decodedRecordStep(ks)
		filters := map[string]bool{}
		collectIDs(keptRequest, filters)
		shared := false
		for id := range parents {
			if filters[id] {
				shared = true
				break
			}
		}
		if !shared {
			continue
		}
		for _, p := range prim {
			if listsItemsKeyed(keptResponse, p.key) || assertsListItemKey(res.Chain, k.ID, p.key) {
				return true
			}
		}
	}
	return false
}

func assertsListItemKey(c *chain.Chain, stepID, key string) bool {
	if c == nil {
		return false
	}
	s, ok := c.Step(stepID)
	if !ok {
		return false
	}
	for _, e := range s.Expect {
		segs := chain.SplitPath(e.Path)
		for i := 1; i < len(segs); i++ {
			if segs[i] == key && isIndex(segs[i-1]) {
				return true
			}
		}
	}
	return false
}

func isIndex(seg string) bool {
	if seg == "" {
		return false
	}
	for _, r := range seg {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func listsItemsKeyed(v any, key string) bool {
	switch t := v.(type) {
	case map[string]any:
		for _, item := range t {
			if listsItemsKeyed(item, key) {
				return true
			}
		}
	case []any:
		for _, item := range t {
			if obj, ok := item.(map[string]any); ok {
				if _, has := obj[key]; has {
					return true
				}
			}
			if listsItemsKeyed(item, key) {
				return true
			}
		}
	}
	return false
}
