package contract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

const (
	longTextPad   = 64
	uuidLength    = 36
	multiBytePart = "Ünïcødé-日本語-✓-"
)

var (
	atMostN    = regexp.MustCompile(`(?i)(?:at most|up to|maximum(?: of| is)?|max\.?|no (?:more|longer) than|longer than|more than|exceeds?|over)\s+(\d+)\s*(?:characters|chars|char|letters|runes|code points|bytes)\b`)
	lengthWord = regexp.MustCompile(`(?i)\b(?:too long|longer than|length|characters|chars|exceeds?)\b`)
	freeText   = map[string]bool{"name": true, "title": true, "description": true, "note": true, "notes": true, "comment": true,
		"label": true, "display": true, "text": true, "message": true, "summary": true, "remark": true, "memo": true, "subject": true, "body": true}
)

type textField struct {
	key   string
	name  string
	max   int
	fail  *Failure
	email bool
	free  bool
}

func statedMaximum(lib *Library, rpc string, c *RPCContract, name string) (int, *Failure, bool) {
	for _, f := range lib.AllFailures(rpc) {
		if f.Field != name && !mentionsField(f.When, name) {
			continue
		}
		if m := atMostN.FindStringSubmatch(f.When); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				failure := f
				return n, &failure, true
			}
		}
	}
	if fc := c.Fields[name]; fc != nil {
		if m := atMostN.FindStringSubmatch(fc.Note); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				for _, f := range lib.AllFailures(rpc) {
					if f.Field == name && lengthWord.MatchString(f.When+" "+f.Reason) {
						failure := f
						return n, &failure, true
					}
				}
				return n, nil, true
			}
		}
	}
	return 0, nil, false
}

func isFreeText(name string) bool {
	for _, w := range namecase.Words(name) {
		if freeText[strings.ToLower(w)] {
			return true
		}
	}
	return false
}

func padding(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		b.WriteByte(alphabet[i%len(alphabet)])
	}
	return b.String()
}

func lengthen(v string, pad string) string {
	if at := strings.LastIndex(v, "@"); at > 0 && !strings.Contains(v[at:], "}") {
		return v[:at] + "-" + pad + v[at:]
	}
	return v + "-" + pad
}

func exactText(n int, unique bool) (string, bool) {
	if !unique {
		return padding(n), true
	}
	if n < uuidLength+2 {
		return "", false
	}
	return "${uuid}-" + padding(n-uuidLength-1), true
}

func (p *Plan) probeTextLength(lib *Library, isTarget func(*chain.Step) bool) {
	for _, st := range append([]*chain.Step{}, p.Chain.Steps...) {
		if !isTarget(st) || chain.IsReadOnlyCall(st.Call) || p.isLogin(st.Call) {
			continue
		}
		c, ok := lib.Get(st.Call)
		if !ok {
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		carrier, echoed := "", map[string]bool{}
		for _, fd := range catalog.DescribeMessage(m.Output()).Fields {
			if fd.Kind != "message" || fd.Repeated || fd.MapKey != "" || fd.Name == chain.EnvelopeField() || IsVerdictFieldName(fd.Name) {
				continue
			}
			for _, sf := range fd.Fields {
				if sf.Kind == "string" && !sf.Repeated {
					echoed[sf.Name] = true
				}
			}
			if carrier == "" {
				carrier = fd.Name
			} else {
				carrier = "-"
			}
		}
		if carrier == "" || carrier == "-" {
			continue
		}
		unique := map[string]bool{}
		for _, f := range lib.AllFailures(st.Call) {
			if noun, isUnique := uniquenessNoun(f); isUnique {
				if field := p.uniqueField(st, c, f, noun); field != "" {
					unique[stripIndexes(field)] = true
				}
			}
		}
		fields := []textField{}
		for _, f := range catalog.DescribeMessage(m.Input()).Fields {
			if f.Kind != "string" || f.Repeated || f.MapKey != "" || len(f.EnumValues) > 0 || idLike(f.Name) || isIdempotencyField(f) || !echoed[f.Name] {
				continue
			}
			key, ok := namecase.LookupKey(st.Body, f.Name)
			if !ok {
				continue
			}
			v, isText := st.Body[key].(string)
			if !isText || strings.TrimSpace(v) == "" || wholeReference(v) || refersToStep(v) {
				continue
			}
			tf := textField{key: key, name: f.Name, email: strings.Contains(v, "@"), free: isFreeText(f.Name)}
			tf.max, tf.fail, _ = statedMaximum(lib, st.Call, c, f.Name)
			fields = append(fields, tf)
		}
		if len(fields) == 0 {
			continue
		}
		p.addTextProbes(lib, st, m, carrier, fields, unique)
	}
}

func (p *Plan) textReadBack(lib *Library, st, probe *chain.Step, m *catalog.Method, carrier string, names []string) *chain.Step {
	idPath := ""
	for _, sf := range carrierFields(m, carrier) {
		if IsEntityIDField(sf.Name) && sf.Kind == "string" {
			if _, sent := namecase.LookupKey(probe.Body, sf.Name); !sent {
				idPath = carrier + "." + sf.Name
				break
			}
		}
	}
	if idPath == "" {
		return nil
	}
	e, ok := p.readerMatching(lib, st, idPath, false)
	if !ok {
		return nil
	}
	stored := map[string]bool{}
	for _, sf := range carrierFields(e.reader, e.carrier) {
		stored[sf.Name] = true
	}
	body := catalog.ScaffoldWith(e.reader.Input(), catalog.ScaffoldOptions{})
	setBodyPath(body, e.field, "${"+probe.ID+"."+idPath+"}")
	read := &chain.Step{
		ID:          p.freeStepID(defaultID(e.reader.Name) + "_after_" + probe.ID),
		Description: fmt.Sprintf("the %s %s stored: the text exactly as sent.", e.carrier, probe.ID),
		Call:        e.reader.FullName,
		Auth:        e.contract.Auth,
		Body:        body,
		Expect:      SuccessExpectation(e.reader),
	}
	p.assertEcho(read)
	for _, name := range names {
		if stored[name] {
			read.Expect = append(read.Expect, chain.Expectation{Path: e.carrier + "." + name, Equals: "${steps." + probe.ID + ".request." + name + "}"})
		}
	}
	return read
}

func (p *Plan) textProbe(lib *Library, st *chain.Step, m *catalog.Method, carrier, suffix, what string, set map[string]string) []*chain.Step {
	probe := p.probeCopy(lib, st, suffix)
	renameStepRefs(probe, st.ID, probe.ID)
	names := []string{}
	for _, key := range sortedKeys(set) {
		probe.Body[key] = set[key]
		names = append(names, key)
	}
	probe.Description = fmt.Sprintf("as %s, but %s %s: accepted, and echoed and stored exactly as sent.", st.ID, strings.Join(names, ", "), what)
	probe.Expect = SuccessExpectation(m)
	for _, name := range names {
		probe.Expect = append(probe.Expect, chain.Expectation{Path: carrier + "." + name, Equals: "${steps." + probe.ID + ".request." + name + "}"})
	}
	out := []*chain.Step{probe}
	if read := p.textReadBack(lib, st, probe, m, carrier, names); read != nil {
		out = append(out, read)
	}
	return out
}

func (p *Plan) addTextProbes(lib *Library, st *chain.Step, m *catalog.Method, carrier string, fields []textField, unique map[string]bool) {
	long, multi := map[string]string{}, map[string]string{}
	capped := []string{}
	for _, tf := range fields {
		v := st.Body[tf.key].(string)
		if tf.max > 0 {
			capped = append(capped, tf.name)
			continue
		}
		long[tf.key] = lengthen(v, padding(longTextPad))
		if tf.free && !tf.email {
			multi[tf.key] = v + " " + strings.TrimSuffix(strings.Repeat(multiBytePart, 3), "-")
		}
	}
	added, said := []*chain.Step{}, []string{}
	if len(long) > 0 {
		steps := p.textProbe(lib, st, m, carrier, "long_text", fmt.Sprintf("grown by %d characters", longTextPad+1), long)
		added = append(added, steps...)
		said = append(said, steps[0].ID+" (each lengthened by "+strconv.Itoa(longTextPad+1)+" characters)")
	}
	if len(multi) > 0 {
		p.Chain.Steps = append(p.Chain.Steps, added...)
		added = nil
		steps := p.textProbe(lib, st, m, carrier, "unicode_text", "carrying multi-byte characters", multi)
		added = append(added, steps...)
		said = append(said, steps[0].ID+" (free text with multi-byte characters)")
	}
	p.Chain.Steps = append(p.Chain.Steps, added...)
	for _, tf := range fields {
		if tf.max <= 0 {
			continue
		}
		at, ok := exactText(tf.max, unique[tf.name])
		if !ok || tf.email {
			p.note("step %s: %s has a stated maximum of %d characters, but a value of exactly that length cannot be built here "+
				"(it must be unique and shorter than a ${uuid}, or it is an email): probe it at %d and %d by hand", st.ID, tf.name, tf.max, tf.max, tf.max+1)
			continue
		}
		over, _ := exactText(tf.max+1, unique[tf.name])
		steps := p.textProbe(lib, st, m, carrier, tf.name+"_at_max", fmt.Sprintf("exactly %d characters, its stated maximum,", tf.max), map[string]string{tf.key: at})
		p.Chain.Steps = append(p.Chain.Steps, steps...)
		refused := p.probeCopy(lib, st, tf.name+"_over_max")
		renameStepRefs(refused, st.ID, refused.ID)
		refused.Body[tf.key] = over
		if tf.fail != nil {
			refused.Expect = refusalFor(m, *tf.fail)
			refused.Description = fmt.Sprintf("%s at %d characters, one over its maximum, is refused with %s.", tf.name, tf.max+1, tf.fail.Label())
		} else {
			refused.Expect = []chain.Expectation{{Path: chain.EnvelopePath(), NotEqual: chain.EnvelopeOK()}}
			refused.Description = fmt.Sprintf("%s at %d characters, one over its maximum, is refused.", tf.name, tf.max+1)
		}
		p.Chain.Steps = append(p.Chain.Steps, p.guardUnchanged(lib, []*chain.Step{refused}, refused.ID)...)
		said = append(said, fmt.Sprintf("%s (%s at its maximum, %d) and %s (%d, refused)", steps[0].ID, tf.name, tf.max, refused.ID, tf.max+1))
	}
	msg := fmt.Sprintf("step %s: text the response echoes is probed with values the fixtures never reach: %s; each asserts "+
		"the text echoed and, where a read takes the id, stored exactly as sent, so a backend that truncates, trims or "+
		"re-encodes it fails", st.ID, strings.Join(said, "; "))
	if len(capped) < len(fields) {
		msg += ". If the backend limits a field's length, state it in its note or a failure's when: (\"at most 40 characters\") " +
			"and plan again: the probe then sends exactly that many and one more, expecting the refusal"
	}
	p.note("%s", msg)
}
