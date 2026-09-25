package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/runner"
)

const (
	readsRelated   = "related"
	readsUnrelated = "unrelated"
)

type fieldReads struct {
	write   string
	verdict string
	fields  []string
	readers []string
}

func (f fieldReads) note() string {
	return fmt.Sprintf("dropped write step %s changes %s, which %s never read: not in their request or response messages, and their contracts "+
		"neither need %s's service nor name it in a failure", f.write, strings.Join(f.fields, ", "), strings.Join(f.readers, ", "), f.write)
}

func classifyFieldReads(e *env, res *chain.SliceResult, rec *runner.Record, related []string) (keep []string, reads []fieldReads) {
	lib, err := e.library()
	if err != nil || e.cat == nil {
		return related, nil
	}
	kept := map[string]bool{}
	for _, id := range related {
		kept[id] = true
	}
	for changed := true; changed; {
		changed = false
		for _, id := range related {
			if !kept[id] {
				continue
			}
			readers := []string{}
			for _, k := range res.Kept {
				readers = append(readers, k.ID)
			}
			for _, other := range related {
				if other != id && kept[other] {
					readers = append(readers, other)
				}
			}
			if fieldReadsOf(e.cat, lib, res, rec, id, readers).verdict != readsRelated {
				kept[id] = false
				changed = true
			}
		}
	}
	readers := []string{}
	for _, k := range res.Kept {
		readers = append(readers, k.ID)
	}
	for _, id := range related {
		if kept[id] {
			keep = append(keep, id)
			readers = append(readers, id)
		}
	}
	for _, id := range related {
		if !kept[id] {
			reads = append(reads, fieldReadsOf(e.cat, lib, res, rec, id, readers))
		}
	}
	return keep, reads
}

func fieldReadsOf(cat *catalog.Catalog, lib *contract.Library, res *chain.SliceResult, rec *runner.Record, writeID string, readerIDs []string) fieldReads {
	out := fieldReads{write: writeID, verdict: readsRelated}
	sr, ok := rec.Step(writeID)
	if !ok || createsListedChild(rec, writeID, res) {
		return out
	}
	facts := entityFactsOf(rec, writeID)
	if !facts.known {
		return out
	}
	wm, err := cat.Lookup(sr.Call)
	if err != nil {
		return out
	}
	_, response := decodedRecordStep(sr)
	changed := map[string]bool{}
	changedFields(response, envelopeParent(), changed)
	if len(changed) == 0 {
		return out
	}
	for f := range changed {
		out.fields = append(out.fields, f)
	}
	sort.Strings(out.fields)
	service, _, _ := strings.Cut(wm.FullName, "/")
	noun := strings.ToLower(strings.TrimSuffix(service[strings.LastIndex(service, ".")+1:], "Service"))
	at := recordIndex(rec, writeID)
	for _, readerID := range readerIDs {
		ks, ok := rec.Step(readerID)
		if !ok || recordIndex(rec, readerID) < at {
			continue
		}
		mentions := entityFactsOf(rec, readerID).mentions
		touches := false
		for id := range facts.acts {
			if mentions[id] {
				touches = true
			}
		}
		if !touches {
			continue
		}
		km, err := cat.Lookup(ks.Call)
		if err != nil {
			return out
		}
		out.readers = append(out.readers, readerID)
		names := map[string]bool{}
		messageFieldNames(catalog.DescribeMessage(km.Input()).Fields, names)
		messageFieldNames(catalog.DescribeMessage(km.Output()).Fields, names)
		for f := range changed {
			if names[f] {
				return out
			}
		}
		rc, has := lib.Get(km.FullName)
		if !has {
			return out
		}
		for _, n := range append(append([]string{}, rc.Needs...), rc.Before...) {
			rpc, _, _ := strings.Cut(n, "@")
			if s, _, _ := strings.Cut(rpc, "/"); rpc == wm.FullName || s == service || strings.HasSuffix(service, "."+s) {
				return out
			}
		}
		if wc, has := lib.Get(wm.FullName); has {
			for _, b := range wc.Before {
				if rpc, _, _ := strings.Cut(b, "@"); rpc == km.FullName {
					return out
				}
			}
		}
		for _, f := range lib.AllFailures(km.FullName) {
			text := strings.ToLower(f.Reason + " " + f.When + " " + f.Message + " " + f.Field)
			if noun != "" && strings.Contains(text, noun) {
				return out
			}
			for field := range changed {
				for _, phrase := range fieldPhrases(field) {
					if strings.Contains(text, phrase) {
						return out
					}
				}
			}
		}
	}
	if len(out.readers) == 0 {
		return out
	}
	out.verdict = readsUnrelated
	return out
}

func envelopeParent() string {
	path := chain.EnvelopePath()
	if i := strings.Index(path, "."); i >= 0 {
		return path[:i]
	}
	return ""
}

func changedFields(v any, envelope string, into map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, item := range t {
			if k == envelope || isIDKey(k) || strings.HasSuffix(strings.ToLower(k), "_at") {
				continue
			}
			switch item.(type) {
			case map[string]any, []any:
				changedFields(item, envelope, into)
			default:
				into[k] = true
			}
		}
	case []any:
		for _, item := range t {
			changedFields(item, envelope, into)
		}
	}
}

func messageFieldNames(fields []*catalog.Field, into map[string]bool) {
	for _, f := range fields {
		into[f.Name] = true
		messageFieldNames(f.Fields, into)
	}
}

func fieldPhrases(field string) []string {
	words := strings.Split(strings.ToLower(field), "_")
	out := []string{strings.ToLower(field), strings.Join(words, " ")}
	if len(words) >= 3 {
		out = append(out, strings.Join(words[1:], " "))
	}
	return out
}

func recordIndex(rec *runner.Record, id string) int {
	for i, sr := range rec.Steps {
		if sr.ID == id {
			return i
		}
	}
	return -1
}
