package contract

import (
	"fmt"
	"slices"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
)

var roleSpecific = lazyRegexp(`(?i)\b(?:roles?|caller|callers|profile|profiles|principal)\b|\bonly (?:to|for) (?:an? |the )?[A-Z]{2,}`)

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
		if prof == st.Auth || prof == invalidProfile || prof == "default" || slices.Contains(out, prof) {
			continue
		}
		out = append(out, prof)
	}
	return out
}

func profileSuffix(prof string) string {
	return strings.ToLower(profileChars().ReplaceAllString(prof, "_"))
}

func roleSpecificPath(c *RPCContract, path string) bool {
	if c == nil {
		return false
	}
	leaf := chain.PathLeaf(path)
	for _, m := range []map[string]string{c.Terminal, c.SoftSignals} {
		for k, text := range m {
			if (k == path || k == leaf) && roleSpecific().MatchString(text) {
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
		at := pathmask.Join(path, f.Name)
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
		c, m, ok := p.contractOf(lib, st.Call)
		if !ok || !openToEveryRole(c) || IsTodo(strings.Join(c.RequiresRole, " ")) {
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
	lists := []string{}
	includes := []chain.Expectation{}
	for _, fd := range catalog.DescribeMessage(m.Output()).Fields {
		if IsVerdictFieldName(fd.Name) {
			continue
		}
		if fd.Repeated && fd.MapKey == "" && fd.Kind == "message" && len(fd.Fields) > 0 {
			if n, ok := assertedLength(st, fd.Name); ok && n > 0 {
				for i := 0; i < n; i++ {
					responseLeaves(fd.Fields, fmt.Sprintf("%s.%d", fd.Name, i), &leaves)
				}
				lists = append(lists, fmt.Sprintf("%s.%d", fd.Name, n))
				continue
			}
			found := false
			for _, e := range st.Expect {
				if e.Path == fd.Name && e.Includes != nil {
					includes = append(includes, e)
					found = true
				}
			}
			if found {
				continue
			}
		}
		if fd.Repeated || fd.MapKey != "" {
			skipped = append(skipped, fd.Name)
			continue
		}
		responseLeaves([]*catalog.Field{fd}, "", &leaves)
	}
	compared, exempt := []string{}, []string{}
	for _, path := range leaves {
		if roleSpecificPath(c, path) {
			exempt = append(exempt, path)
			continue
		}
		compared = append(compared, path)
	}
	if len(compared) == 0 && len(includes) == 0 {
		return
	}
	at := st.ID
	ids := []string{}
	for _, prof := range profiles {
		probe := probeStep(st, p.freeStepID(st.ID+"_as_"+profileSuffix(prof)))
		probe.Auth = prof
		probe.Description = fmt.Sprintf("the same read as %s, as profile %s: the same answer, field for field.", st.ID, prof)
		probe.Expect = SuccessExpectation(m)
		for _, path := range compared {
			probe.Expect = append(probe.Expect, chain.Expectation{Path: path, Equals: "${" + st.ID + "." + path + "}"})
		}
		for _, path := range lists {
			probe.Expect = append(probe.Expect, chain.Expectation{Path: path, Exists: boolPtr(false)})
		}
		for _, e := range includes {
			probe.Expect = append(probe.Expect, e.MapOperands(cloneBody))
		}
		p.insertAfter(at, probe)
		at = probe.ID
		ids = append(ids, probe.ID)
	}
	msg := fmt.Sprintf("step %s: its contract lets every role call it, so %s %s the same read as another profile and "+
		"%s each field %s answered (%s) equal: a backend that hides or zeroes a field for a lower role fails",
		st.ID, strings.Join(ids, ", "), pluralVerb(len(ids), "sends", "send"), pluralVerb(len(ids), "asserts", "assert"), st.ID, strings.Join(clipList(compared, 8), ", "))
	if len(lists) > 0 {
		msg += fmt.Sprintf("; each item of the list, by position, and no item past %s", strings.Join(lists, ", "))
	}
	if len(includes) > 0 {
		msg += "; every fixture the list must include, by id"
	}
	if len(exempt) > 0 {
		msg += fmt.Sprintf("; %s %s documented as role-specific in terminal:/soft_signals:, so not compared", strings.Join(exempt, ", "), pluralVerb(len(exempt), "is", "are"))
	}
	if len(skipped) > 0 {
		msg += fmt.Sprintf("; the repeated %s %s not compared item by item: assert what each profile must see", strings.Join(skipped, ", "), pluralVerb(len(skipped), "is", "are"))
	}
	p.note("%s", msg)
}

func (p *Plan) writeParity(lib *Library, st *chain.Step, m *catalog.Method, profiles []string) {
	entities := p.entitiesOf(lib, st)
	if idPath := p.createdIDPath(m); idPath != "" {
		if e, ok := p.readerMatching(lib, st, idPath, true); ok {
			entities = append(entities, e)
		}
	}
	comparable := []entityRead{}
	for _, e := range entities {
		kept := slices.DeleteFunc(slices.Clone(e.scalars), func(name string) bool { return isStampName(name) || chain.IsExpiryName(name) })
		if len(kept) > 0 {
			e.scalars = kept
			comparable = append(comparable, e)
		}
	}
	if len(comparable) == 0 {
		if idPath := p.createdIDPath(m); idPath != "" && p.createParity(lib, st, idPath, profiles) {
			return
		}
		if readers := p.textOnlyReaders(lib, st, p.createdIDPath(m)); len(readers) > 0 {
			p.note("step %s: its contract lets every role call it, and %s %s the id of what it changes, but %s answers "+
				"no number or state, only text and ids that each profile's own fixture sends differently, so nothing "+
				"compares its effect as another profile: call it as each profile and assert what each read returns",
				st.ID, strings.Join(readers, ", "), pluralVerb(len(readers), "takes", "take"), pluralVerb(len(readers), "it", "each"))
			return
		}
		p.note("step %s: its contract lets every role call it, but no read rpc in the contracts takes the id of what it "+
			"changes, so nothing compares its effect as another profile: call it as each profile on a fixture of its own "+
			"and read back what it changed", st.ID)
		return
	}
	upto := stepIndex(p.Chain.Steps, st.ID)
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
	for _, s := range p.Chain.Steps[:upto] {
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
		r := e.readStep("", "", "${"+producer+"."+e.idPath+"}")
		p.assertEcho(r)
		return r
	}
	readID := func(e entityRead, tail string) string { return p.freeStepID(p.readBase(e) + "_after_" + tail) }
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
		for _, s := range p.Chain.Steps[:upto] {
			if owned[s.ID] {
				rename[s.ID] = p.freeStepID(s.ID + suffix)
			}
		}
		added := []*chain.Step{}
		for _, s := range p.Chain.Steps[:upto] {
			if !owned[s.ID] {
				continue
			}
			c := p.fixtureCopy(lib, s, rename[s.ID], rename, fmt.Sprintf("as %s, for %s to act on as %s.", s.ID, st.ID, prof))
			added = append(added, c)
		}
		w := probeStep(st, p.freeStepID(st.ID+"_as_"+profileSuffix(prof)))
		w.Auth = prof
		retarget(w, rename)
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
			r.Expect = append(r.Expect, e.asIn(admin)...)
			added = append(added, r)
		}
		p.Chain.Steps = append(p.Chain.Steps, added...)
		ids = append(ids, w.ID)
		p.parities = append(p.parities, parityCopy{write: st.ID, copy: w.ID, suffix: suffix, profile: prof, rename: rename})
	}
	p.note("step %s: its contract lets every role call it, so %s %s it as another profile on fixtures of its own, created and "+
		"prepared as %s's were (unique fields changed, numbers kept), and the reads that follow assert the same numbers and states "+
		"as the reads after %s: a backend that applies the write differently for a lower role fails",
		st.ID, strings.Join(ids, ", "), pluralVerb(len(ids), "repeats", "repeat"), st.ID, st.ID)
}

func (p *Plan) createParity(lib *Library, st *chain.Step, idPath string, profiles []string) bool {
	if _, ok := p.readerMatching(lib, st, idPath, false); !ok {
		return false
	}
	ids := []string{}
	for _, prof := range profiles {
		w := probeStep(st, p.freeStepID(st.ID+"_as_"+profileSuffix(prof)))
		w.Auth = prof
		p.freshen(lib, w)
		renameStepRefs(w, st.ID, w.ID)
		w.Description = fmt.Sprintf("%s as profile %s, with its unique fields changed: created as it is for the default profile, and read back as sent.", st.ID, prof)
		p.assertEcho(w)
		added := []*chain.Step{w}
		if read := p.readBackStep(lib, w, idPath); read != nil {
			added = append(added, read)
		}
		p.Chain.Steps = append(p.Chain.Steps, added...)
		ids = append(ids, w.ID)
	}
	p.note("step %s: its contract lets every role call it, so %s %s it as another profile, with its unique fields changed, "+
		"and %s the record back: every field it sent, as sent. It answers no number or state to compare with the default "+
		"profile's, so a backend that refuses the create for a lower role, or stores it differently, fails there",
		st.ID, strings.Join(ids, ", "), pluralVerb(len(ids), "repeats", "repeat"), pluralVerb(len(ids), "reads", "read"))
	return true
}

type parityCopy struct {
	write   string
	copy    string
	suffix  string
	profile string
	rename  map[string]string
}

func (p *Plan) copyIntoParities(lib *Library, added *chain.Step, producer string) []string {
	at := stepIndex(p.Chain.Steps, added.ID)
	out := []string{}
	for _, pc := range p.parities {
		twin := pc.rename[producer]
		if twin == "" || at < 0 || at > stepIndex(p.Chain.Steps, pc.write) {
			continue
		}
		c := copyStep(added, p.freeStepID(added.ID+pc.suffix))
		retarget(c, pc.rename)
		renameStepRefs(c, added.ID, c.ID)
		p.freshen(lib, c)
		c.Description = fmt.Sprintf("as %s, for %s to act on as %s: its fixtures are prepared as %s's were.", added.ID, pc.copy, pc.profile, pc.write)
		pos := p.latestReference(c)
		if pos < 0 || pos >= stepIndex(p.Chain.Steps, pc.copy) {
			continue
		}
		p.insertAfter(p.Chain.Steps[pos].ID, c)
		pc.rename[added.ID] = c.ID
		out = append(out, c.ID)
	}
	return out
}

func stepIndex(steps []*chain.Step, id string) int {
	return slices.IndexFunc(steps, func(s *chain.Step) bool { return s.ID == id })
}

func (p *Plan) createdIDPath(m *catalog.Method) string {
	for _, fd := range carriersOf(m) {
		for _, sf := range fd.Fields {
			if IsEntityIDField(sf.Name) && sf.Kind == "string" && fieldByName(catalog.DescribeMessage(m.Input()).Fields, sf.Name) == nil {
				return fd.Name + "." + sf.Name
			}
		}
	}
	return ""
}
