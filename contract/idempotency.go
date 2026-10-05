package contract

import (
	"fmt"
	"slices"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/pathmask"
)

var (
	keyConflict     = lazyRegexp(`(?i)idempoten|key ?reuse|key ?conflict|key ?mismatch`)
	keyConflictWhen = lazyRegexp(`(?i)\bkey\b[^.;]*\b(?:different|another|other|changed)\s+(?:body|request|payload|content)`)
)

func (p *Plan) probeIdempotency(lib *Library, isTarget func(*chain.Step) bool) {
	for st := range p.targets(isTarget, isRead) {
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		var keyField *catalog.Field
		for _, f := range catalog.DescribeMessage(m.Input()).Fields {
			if isIdempotencyField(f) {
				keyField = f
			}
		}
		if keyField == nil {
			continue
		}
		key, ok := namecase.LookupKey(st.Body, keyField.Name)
		if !ok {
			continue
		}
		carrier, idField, numbers := "", "", []string{}
		for _, fd := range carriersOf(m) {
			for _, sf := range fd.Fields {
				if idField == "" && IsEntityIDField(sf.Name) && sf.Kind == "string" {
					carrier, idField = fd.Name, sf.Name
				}
			}
			if carrier == fd.Name {
				for _, sf := range fd.Fields {
					if chain.IsNumericKind(sf.Kind) && !sf.Repeated {
						numbers = append(numbers, sf.Name)
					}
				}
			}
		}
		if carrier == "" {
			continue
		}
		p.addIdempotencyProbes(lib, st, m, key, carrier, idField, numbers)
	}
}

func (p *Plan) addIdempotencyProbes(lib *Library, st *chain.Step, m *catalog.Method, key, carrier, idField string, numbers []string) {
	sent := "${steps." + st.ID + ".request." + key + "}"
	first := "${" + st.ID + "." + carrier + "." + idField + "}"
	idPath := carrier + "." + idField

	replay := probeStep(st, p.freeStepID(st.ID+"_replay"))
	renameStepRefs(replay, st.ID, replay.ID)
	replay.Body[key] = sent
	replay.Description = fmt.Sprintf("the same request with the same %s returns the %s %s created, not a second one.", key, carrier, st.ID)
	replay.Expect = append(SuccessExpectation(m), chain.Expectation{Path: idPath, Equals: first})

	other := copyStep(replay, p.freeStepID(st.ID+"_replay_other_body"))
	bumped := bumpNumbers(other.Body, catalog.DescribeMessage(m.Input()).Fields)
	other.Description = fmt.Sprintf("the same %s with another body (%s changed) still returns the first %s, unchanged.", key, strings.Join(bumped, ", "), carrier)
	other.Expect = append(SuccessExpectation(m), chain.Expectation{Path: idPath, Equals: first})
	for _, n := range numbers {
		other.Expect = append(other.Expect, chain.Expectation{Path: carrier + "." + n, Equals: "${" + st.ID + "." + carrier + "." + n + "}"})
	}
	conflict := ""
	for _, f := range lib.AllFailures(st.Call) {
		if keyConflict().MatchString(f.Reason) || keyConflictWhen().MatchString(f.When) {
			other.Expect = refusalFor(m, f, true)
			other.Description = fmt.Sprintf("the same %s with another body (%s changed) is refused with %s.", key, strings.Join(bumped, ", "), f.Label())
			conflict = f.Label()
			break
		}
	}
	added := []*chain.Step{replay}
	if len(bumped) > 0 {
		added = append(added, other)
	}

	p.Chain.Steps = append(p.Chain.Steps, added...)
	what := "must return the first " + carrier
	if conflict != "" {
		what = "is refused with " + conflict + " when the body differs"
	}
	p.note("step %s: %s is an idempotency key; %s replays it with the same body and must return %s's %s, %s replays it with "+
		"another body and %s", st.ID, key, replay.ID, st.ID, idField, other.ID, what)
	p.replayAfterTransitions(lib, st, m, key, carrier, idField)
}

func (p *Plan) replayAfterTransitions(lib *Library, st *chain.Step, m *catalog.Method, key, carrier, idField string) {
	fd := fieldByName(catalog.DescribeMessage(m.Output()).Fields, carrier)
	at := slices.IndexFunc(fd.Fields, func(sf *catalog.Field) bool { return len(sf.EnumValues) > 1 && !sf.Repeated })
	if at < 0 {
		return
	}
	carrierMsg, stateField := fd.Message, fd.Fields[at]
	idPath := carrier + "." + idField
	read, ok := p.readerMatching(lib, st, idPath, true)
	if !ok {
		return
	}
	values := stateField.EnumValues[1:]
	short := enumShort(stateField.EnumValues)
	initial := ""
	if c, ok := lib.Get(st.Call); ok {
		initial = stateIn([]string{c.Exports[carrier], c.Summary}, values, short)
	}
	t := &listTarget{step: st, itemMsg: carrierMsg, itemID: idField, carrier: carrier}
	transitions, _ := p.transitionsFor(lib, t, st, values, short, initial)
	if len(transitions) == 0 {
		return
	}
	echoed := map[string]bool{}
	for _, sf := range carrierFields(m, carrier) {
		echoed[sf.Name] = true
	}
	added, ids := []*chain.Step{}, []string{}
	for _, tr := range transitions {
		label := chain.SnakeCase(tr.method.Name)
		fid := p.freeStepID(st.ID + "_for_replay_after_" + label)
		fixture := p.fixtureCopy(lib, st, fid, map[string]string{st.ID: fid}, fmt.Sprintf("as %s, with its own %s, for %s to move and then replay.", st.ID, key, label))
		fixtureID := "${" + fixture.ID + "." + idPath + "}"

		move := tr.step(p.freeStepID(label+"_for_replay"),
			fmt.Sprintf("moves %s to %s before its key is replayed.", fixture.ID, short[tr.value]), fixtureID, carrier+"."+stateField.Name)

		readID := p.freeStepID(chain.SnakeCase(read.reader.Name) + "_after_" + move.ID)
		fetch := read.readStep(readID, fmt.Sprintf("the %s as %s left it, which the replay must return.", read.carrier, move.ID), fixtureID)
		fetch.Expect = append(fetch.Expect, chain.Expectation{Path: read.carrier + "." + stateField.Name, Equals: tr.value})

		replay := copyStep(fixture, p.freeStepID(st.ID+"_replay_after_"+label))
		replay.Body[key] = "${steps." + fixture.ID + ".request." + key + "}"
		replay.Description = fmt.Sprintf("the same request with %s's %s, after %s: the %s as it stands now (%s), not as it was created.",
			fixture.ID, key, move.ID, carrier, short[tr.value])
		replay.Expect = append(SuccessExpectation(m), chain.Expectation{Path: idPath, Equals: fixtureID})
		for _, name := range read.scalars {
			if echoed[name] {
				replay.Expect = append(replay.Expect, chain.Expectation{Path: carrier + "." + name, Equals: "${" + readID + "." + read.carrier + "." + name + "}"})
			}
		}
		added = append(added, fixture, move, fetch, replay)
		ids = append(ids, replay.ID)
	}
	p.Chain.Steps = append(p.Chain.Steps, added...)
	p.note("step %s: a replay can be answered from a copy taken at creation, so %s %s %s after moving a fresh %s with each write "+
		"that changes its %s, and assert the replayed %s equals what the read just before returns, %s included",
		st.ID, strings.Join(ids, " and "), pluralVerb(len(ids), "replays", "replay"), key, carrier, stateField.Name, carrier, stateField.Name)
}

func bumpNumbers(body map[string]any, fields []*catalog.Field) []string {
	changed := []string{}
	var walk func(m map[string]any, fs []*catalog.Field, path string)
	walk = func(m map[string]any, fs []*catalog.Field, path string) {
		for _, f := range fs {
			key, ok := namecase.LookupKey(m, f.Name)
			if !ok || f.MapKey != "" || idLike(f.Name) {
				continue
			}
			at := pathmask.Join(path, f.Name)
			switch v := m[key].(type) {
			case []any:
				for i, item := range v {
					if im, ok := item.(map[string]any); ok {
						walk(im, f.Fields, fmt.Sprintf("%s.%d", at, i))
					}
				}
			case map[string]any:
				walk(v, f.Fields, at)
			default:
				if n, ok := numericValue(v); ok && n != 0 && chain.IsNumericKind(f.Kind) {
					m[key] = fmt.Sprint(n + 1)
					changed = append(changed, at)
				}
			}
		}
	}
	walk(body, fields, "")
	return changed
}
