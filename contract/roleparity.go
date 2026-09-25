package contract

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

var roleSpecific = regexp.MustCompile(`(?i)\b(?:roles?|caller|callers|profile|profiles|principal)\b|\bonly (?:to|for) (?:an? |the )?[A-Z]{2,}`)

func openToEveryRole(c *RPCContract) bool {
	if c == nil {
		return false
	}
	if len(c.RequiresRole) == 0 {
		return true
	}
	return c.DeclaresNoRole()
}

func (p *Plan) parityProfiles(st *chain.Step) []string {
	out := []string{}
	for _, prof := range p.opts.Profiles {
		if prof == st.Auth || prof == invalidProfile || prof == "default" || containsString(out, prof) {
			continue
		}
		out = append(out, prof)
	}
	return out
}

func profileSuffix(prof string) string {
	return strings.ToLower(profileChars.ReplaceAllString(prof, "_"))
}

func roleSpecificPath(c *RPCContract, path string) bool {
	if c == nil {
		return false
	}
	leaf := leafName(path)
	for _, m := range []map[string]string{c.Terminal, c.SoftSignals} {
		for k, text := range m {
			if (k == path || k == leaf) && roleSpecific.MatchString(text) {
				return true
			}
		}
	}
	return false
}

func responseLeaves(fields []*catalog.Field, path string, out *[]string) {
	for _, f := range fields {
		if f.Repeated || f.MapKey != "" {
			continue
		}
		at := join(path, f.Name)
		if f.Kind == "message" && f.JSONForm == "" && len(f.Fields) > 0 {
			responseLeaves(f.Fields, at, out)
			continue
		}
		*out = append(*out, at)
	}
}

func (p *Plan) probeRoleParity(lib *Library, isTarget func(*chain.Step) bool) {
	if !p.opts.Auth {
		return
	}
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || p.isLogin(st.Call) || st.SkipAuth {
			continue
		}
		c, ok := lib.Get(st.Call)
		if !ok || !openToEveryRole(c) || IsTodo(strings.Join(c.RequiresRole, " ")) {
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		profiles := p.parityProfiles(st)
		if len(profiles) == 0 {
			continue
		}
		if chain.IsReadOnlyCall(st.Call) {
			p.readParity(st, m, c, profiles)
			continue
		}
		p.writeParity(lib, st, m, profiles)
	}
}

func (p *Plan) readParity(st *chain.Step, m *catalog.Method, c *RPCContract, profiles []string) {
	leaves := []string{}
	skipped := []string{}
	for _, fd := range catalog.DescribeMessage(m.Output()).Fields {
		if fd.Name == chain.EnvelopeField() || IsVerdictFieldName(fd.Name) {
			continue
		}
		if fd.Repeated || fd.MapKey != "" {
			skipped = append(skipped, fd.Name)
			continue
		}
		if fd.Kind == "message" && fd.JSONForm == "" && len(fd.Fields) > 0 {
			responseLeaves(fd.Fields, fd.Name, &leaves)
			continue
		}
		leaves = append(leaves, fd.Name)
	}
	compared, exempt := []string{}, []string{}
	for _, path := range leaves {
		if roleSpecificPath(c, path) {
			exempt = append(exempt, path)
			continue
		}
		compared = append(compared, path)
	}
	if len(compared) == 0 {
		return
	}
	at := st.ID
	ids := []string{}
	for _, prof := range profiles {
		probe := copyStep(st, p.freeStepID(st.ID+"_as_"+profileSuffix(prof)))
		probe.Export = nil
		probe.Auth = prof
		probe.Description = fmt.Sprintf("the same read as %s, as profile %s: the same answer, field for field.", st.ID, prof)
		probe.Expect = SuccessExpectation(m)
		for _, path := range compared {
			probe.Expect = append(probe.Expect, chain.Expectation{Path: path, Equals: "${" + st.ID + "." + path + "}"})
		}
		p.insertAfter(at, probe)
		at = probe.ID
		ids = append(ids, probe.ID)
	}
	msg := fmt.Sprintf("step %s: its contract lets every role call it, so %s %s the same read as another profile and "+
		"%s each field %s answered (%s) equal: a backend that hides or zeroes a field for a lower role fails",
		st.ID, strings.Join(ids, ", "), pluralVerb(len(ids), "sends", "send"), pluralVerb(len(ids), "asserts", "assert"), st.ID, strings.Join(compared, ", "))
	if len(exempt) > 0 {
		msg += fmt.Sprintf("; %s %s documented as role-specific in terminal:/soft_signals:, so not compared", strings.Join(exempt, ", "), pluralIs(len(exempt)))
	}
	if len(skipped) > 0 {
		msg += fmt.Sprintf("; the repeated %s %s not compared item by item: assert what each profile must see", strings.Join(skipped, ", "), pluralIs(len(skipped)))
	}
	p.note("%s", msg)
}

func (p *Plan) writeParity(lib *Library, st *chain.Step, m *catalog.Method, profiles []string) {
	entities := p.entitiesOf(lib, st)
	if idPath := p.createdIDPath(st, m); idPath != "" {
		if e, ok := p.readerFor(lib, st, idPath); ok {
			entities = append(entities, e)
		}
	}
	comparable := []entityRead{}
	for _, e := range entities {
		kept := []string{}
		for _, name := range e.scalars {
			if !isStampName(name) && !isExpiryName(name) {
				kept = append(kept, name)
			}
		}
		if len(kept) > 0 {
			e.scalars = kept
			comparable = append(comparable, e)
		}
	}
	if len(comparable) == 0 {
		p.note("step %s: its contract lets every role call it, but no read rpc in the contracts takes the id of what it "+
			"changes, so nothing compares its effect as another profile: call it as each profile on a fixture of its own "+
			"and read back what it changed", st.ID)
		return
	}
	index := map[string]int{}
	for i, s := range p.Chain.Steps {
		index[s.ID] = i
	}
	owned := map[string]bool{}
	for _, e := range comparable {
		if e.producer != st {
			owned[e.producer.ID] = true
		}
	}
	for _, src := range referencedSteps(st.Body) {
		if prod := p.stepByID(src); prod != nil && !chain.IsReadOnlyCall(prod.Call) {
			owned[src] = true
		}
	}
	for _, s := range p.Chain.Steps[:index[st.ID]] {
		if owned[s.ID] || chain.IsReadOnlyCall(s.Call) || s.SkipAuth || isRefusalStep(s) {
			continue
		}
		for id := range owned {
			if readsStep(s, id) {
				owned[s.ID] = true
				break
			}
		}
	}

	baseRead := func(e entityRead, producer string) *chain.Step {
		body := catalog.ScaffoldWith(e.reader.Input(), catalog.ScaffoldOptions{})
		setBodyPath(body, e.field, "${"+producer+"."+e.idPath+"}")
		r := &chain.Step{Call: e.reader.FullName, Auth: e.contract.Auth, Body: body, Expect: SuccessExpectation(e.reader)}
		p.assertEcho(r)
		return r
	}
	readID := func(e entityRead, tail string) string {
		base := defaultID(e.reader.Name)
		if pm, err := p.cat.Lookup(e.producer.Call); err == nil {
			if suffix := strings.TrimPrefix(e.producer.ID, defaultID(pm.Name)); isIndexSuffix(suffix) {
				base += suffix
			}
		}
		return p.freeStepID(base + "_after_" + tail)
	}
	adminReads := map[string]string{}
	at := st.ID
	for _, e := range comparable {
		r := baseRead(e, e.producer.ID)
		r.ID = readID(e, st.ID)
		r.Description = fmt.Sprintf("the %s as %s left it, which the same write as another profile must match.", e.carrier, st.ID)
		p.insertAfter(at, r)
		at = r.ID
		adminReads[e.producer.ID] = r.ID
	}

	ids := []string{}
	for _, prof := range profiles {
		suffix := "_for_" + profileSuffix(prof)
		rename := map[string]string{}
		for _, s := range p.Chain.Steps[:index[st.ID]] {
			if owned[s.ID] {
				rename[s.ID] = p.freeStepID(s.ID + suffix)
			}
		}
		added := []*chain.Step{}
		for _, s := range p.Chain.Steps[:index[st.ID]] {
			if !owned[s.ID] {
				continue
			}
			c := copyStep(s, rename[s.ID])
			c.Export = nil
			c.Body, _ = rewriteRefs(c.Body, rename).(map[string]any)
			for i := range c.Expect {
				c.Expect[i] = c.Expect[i].MapOperands(func(v any) any { return rewriteRefs(v, rename) })
			}
			p.freshen(lib, c)
			c.Description = fmt.Sprintf("as %s, for %s to act on as %s.", s.ID, st.ID, prof)
			p.assertEcho(c)
			added = append(added, c)
		}
		w := copyStep(st, p.freeStepID(st.ID+"_as_"+profileSuffix(prof)))
		w.Export = nil
		w.Auth = prof
		w.Body, _ = rewriteRefs(w.Body, rename).(map[string]any)
		for i := range w.Expect {
			w.Expect[i] = w.Expect[i].MapOperands(func(v any) any { return rewriteRefs(v, rename) })
		}
		p.freshen(lib, w)
		renameStepRefs(w, st.ID, w.ID)
		w.Description = fmt.Sprintf("%s as profile %s, on fixtures of its own prepared as %s's were: the same effect.", st.ID, prof, st.ID)
		p.assertEcho(w)
		added = append(added, w)
		for _, e := range comparable {
			producer := rename[e.producer.ID]
			if e.producer == st {
				producer = w.ID
			}
			if producer == "" {
				continue
			}
			r := baseRead(e, producer)
			r.ID = readID(e, w.ID)
			admin := adminReads[e.producer.ID]
			r.Description = fmt.Sprintf("the %s as %s left it: %s as in %s.", e.carrier, w.ID, strings.Join(e.scalars, ", "), admin)
			for _, name := range e.scalars {
				path := e.carrier + "." + name
				r.Expect = append(r.Expect, chain.Expectation{Path: path, Equals: "${" + admin + "." + path + "}"})
			}
			added = append(added, r)
		}
		p.Chain.Steps = append(p.Chain.Steps, added...)
		ids = append(ids, w.ID)
	}
	p.note("step %s: its contract lets every role call it, so %s %s it as another profile on fixtures of its own, created and "+
		"prepared as %s's were (unique fields changed, numbers kept), and the reads that follow assert the same numbers and states "+
		"as the reads after %s: a backend that applies the write differently for a lower role fails",
		st.ID, strings.Join(ids, ", "), pluralVerb(len(ids), "repeats", "repeat"), st.ID, st.ID)
}

func (p *Plan) createdIDPath(st *chain.Step, m *catalog.Method) string {
	for _, fd := range catalog.DescribeMessage(m.Output()).Fields {
		if fd.Kind != "message" || fd.Repeated || fd.MapKey != "" || fd.Name == chain.EnvelopeField() || IsVerdictFieldName(fd.Name) {
			continue
		}
		for _, sf := range fd.Fields {
			if IsEntityIDField(sf.Name) && sf.Kind == "string" {
				in := false
				for _, f := range catalog.DescribeMessage(m.Input()).Fields {
					if f.Name == sf.Name {
						in = true
					}
				}
				if !in {
					return fd.Name + "." + sf.Name
				}
			}
		}
	}
	return ""
}
