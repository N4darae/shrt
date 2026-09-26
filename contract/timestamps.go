package contract

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

var durationPhrase = regexp.MustCompile(`(?i)\b(\d+|an?|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|fifteen|twenty|thirty|sixty)[\s-]*(seconds?|secs?|minutes?|mins?|hours?|hrs?|days?|weeks?)\b`)

var numberWords = map[string]int64{
	"a": 1, "an": 1, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8,
	"nine": 9, "ten": 10, "eleven": 11, "twelve": 12, "fifteen": 15, "twenty": 20, "thirty": 30, "sixty": 60,
}

func durationSeconds(text string) (int64, string, bool) {
	m := durationPhrase.FindStringSubmatch(text)
	if m == nil {
		return 0, "", false
	}
	n, ok := numberWords[strings.ToLower(m[1])]
	if !ok {
		parsed, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return 0, "", false
		}
		n = parsed
	}
	unit := strings.ToLower(m[2])
	switch {
	case strings.HasPrefix(unit, "sec"):
	case strings.HasPrefix(unit, "min"):
		n *= 60
	case strings.HasPrefix(unit, "h"):
		n *= 3600
	case strings.HasPrefix(unit, "day"):
		n *= 86400
	case strings.HasPrefix(unit, "week"):
		n *= 7 * 86400
	}
	return n, m[0], n > 0
}

func isExpiryName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "expire") || strings.Contains(lower, "expiry")
}

func isStampName(name string) bool {
	lower := strings.ToLower(name)
	for _, w := range []string{"created", "updated", "modified", "issued", "inserted", "registered"} {
		if strings.HasPrefix(lower, w) {
			return true
		}
	}
	return false
}

func contractNote(c *RPCContract, path string) string {
	last := path
	if i := strings.LastIndex(path, "."); i >= 0 {
		last = path[i+1:]
	}
	parts := []string{}
	for _, m := range []map[string]string{c.Terminal, c.Exports, c.SoftSignals} {
		for _, key := range []string{path, last} {
			if note := strings.TrimSpace(m[key]); note != "" && !IsTodo(note) {
				parts = append(parts, note)
			}
		}
	}
	return strings.Join(parts, " ")
}

func (p *Plan) earlierMethods() map[string]*catalog.Method {
	out := map[string]*catalog.Method{}
	for _, s := range p.Chain.Steps {
		if m, err := p.cat.Lookup(s.Call); err == nil {
			out[s.ID] = m
		}
	}
	return out
}

func (p *Plan) timestampExpectations(step *chain.Step, m *catalog.Method, c *RPCContract, domain string) []chain.Expectation {
	id := step.ID
	out := []chain.Expectation{}
	for _, path := range chain.TimestampFields(m) {
		last := path
		if i := strings.LastIndex(path, "."); i >= 0 {
			last = path[i+1:]
		}
		note := contractNote(c, path)
		if strings.Contains(strings.ToLower(note), "milli") {
			p.note("step %s: %s is in milliseconds by its contract, and ${nowunix} is seconds, so no range was scaffolded for it; "+
				"assert it with a bound you compute, or not at all", id, path)
			continue
		}
		switch {
		case isExpiryName(last):
			texts := []string{note, c.Summary, c.Note, domain}
			var secs int64
			phrase := ""
			for _, t := range texts {
				if s, ph, ok := durationSeconds(t); ok {
					secs, phrase = s, ph
					break
				}
			}
			if secs > 0 {
				out = append(out, chain.Expectation{Path: path, Within: &chain.Within{Of: "${nowunix+" + strconv.FormatInt(secs, 10) + "}", By: 5}})
				p.note("step %s: %s is asserted within 5s of ${nowunix+%d}, from %q in the contract: verify masks a timestamp, so this "+
					"range is what catches an expiry in the wrong unit or for the wrong span", id, path, secs, phrase)
				continue
			}
			out = append(out, chain.Expectation{Path: path, Gte: "${nowunix}"})
			p.note("step %s: %s is asserted gte ${nowunix}, not yet expired; the contract names no lifetime (\"valid for one hour\" in "+
				"its summary or in terminal:/exports: for %s), so a tighter within: range could not be scaffolded", id, path, last)
		case chain.IsCreationStampName(last) && chain.StampSource(step, path, p.earlierMethods()) != "":
			src := chain.StampSource(step, path, p.earlierMethods())
			out = append(out, chain.Expectation{Path: path, Equals: "${" + src + "." + path + "}"})
			p.note("step %s: %s is asserted equal to the %s step %s received: this call did not stamp it, it returns the stored "+
				"stamp, so a clock window would pass a stamp rewritten on every read", id, path, path, src)
		case isStampName(last) && chain.IsReadOnlyCall(m.FullName):
			p.note("step %s: %s was not stamped by this read and no earlier step it reads returned it, so no expectation was "+
				"scaffolded; assert it equals the stamp of the step that set it", id, path)
		case isStampName(last):
			out = append(out, chain.Expectation{Path: path, Within: &chain.Within{Of: "${nowunix}", By: 300}})
			p.note("step %s: %s is asserted within 300s of ${nowunix}: stamped by this call, so it is now, as far as the "+
				"backend's clock agrees with this machine's; verify masks it, so this range is its only check", id, path)
		}
	}
	return out
}
