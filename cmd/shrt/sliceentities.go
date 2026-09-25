package main

import (
	"encoding/json"
	"fmt"
	"sort"
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
		if _, wroteNothing := refused(id); wroteNothing {
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
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
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
		for _, p := range prim {
			if !replayedEarlier(rec, at, sr.Call, p) {
				f.acts[p.id] = true
			}
		}
		return f
	}
	collectIDs(request, f.acts)
	f.known = len(f.acts) > 0
	return f
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
	for changed := true; changed; {
		changed = false
		for _, d := range res.DroppedWrites {
			if related[d.ID] {
				continue
			}
			f := entityFactsOf(rec, d.ID)
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
		if sr.Status != runner.StatusFailed && sr.Status != runner.StatusError {
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

func otherEntitiesNote(other []string) string {
	if len(other) == 0 {
		return ""
	}
	return fmt.Sprintf("dropped write step(s) %s change no entity a kept step uses (the ids they act on appear in no kept step's "+
		"request or response in the source run, or they answered exactly as an earlier call of the same rpc did)", strings.Join(other, ", "))
}
