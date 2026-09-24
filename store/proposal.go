package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

var (
	ErrNoProposal      = errors.New("no pending proposal")
	ErrProposalChanged = errors.New("the proposed run no longer matches the proposal")
	ErrNoEvidence      = errors.New("a proposal must say what was checked")
)

type Proposal struct {
	Chain      string    `json:"chain"`
	RunID      string    `json:"run_id"`
	Target     string    `json:"target"`
	Build      string    `json:"build,omitempty"`
	ProposedBy string    `json:"proposed_by"`
	ProposedAt time.Time `json:"proposed_at"`
	Checked    string    `json:"checked"`
	Supersede  bool      `json:"supersede,omitempty"`
	Replaces   string    `json:"replaces,omitempty"`
	Digest     string    `json:"digest"`
	Report     string    `json:"report"`
	ComparedTo string    `json:"compared_to,omitempty"`
	Unstable   []string  `json:"unstable,omitempty"`
	Replaced   []Differ  `json:"differs_from_replaced,omitempty"`
}

type Differ struct {
	Step  string `json:"step"`
	Side  string `json:"side"`
	Path  string `json:"path"`
	Delta string `json:"delta"`
}

func (d Differ) String() string {
	return d.Side + " " + d.Path + " " + d.Delta
}

type ProposalInput struct {
	By         string
	Checked    string
	Supersede  bool
	Now        time.Time
	ComparedTo string
	Unstable   []string
	Replaced   []Differ
}

func (s *Store) Propose(rec *runner.Record, in ProposalInput) (*Proposal, error) {
	in.Checked = strings.TrimSpace(in.Checked)
	if in.Checked == "" {
		return nil, ErrNoEvidence
	}
	if !rec.Passed() {
		return nil, notPassed(rec)
	}
	if err := s.checkSealed(rec); err != nil {
		return nil, err
	}
	replaces := ""
	prev, err := s.LoadSafeSpot(rec.Chain)
	switch {
	case err == nil:
		if !in.Supersede {
			return nil, fmt.Errorf("%w at %s (confirmed by %s at %s); pass -supersede to propose replacing it",
				ErrExists, s.SafeSpotPath(rec.Chain), prev.ConfirmedBy, prev.ConfirmedAt.Format(time.RFC3339))
		}
		replaces = prev.RunID
	case errors.Is(err, os.ErrNotExist):
	default:
		return nil, err
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	by := strings.TrimSpace(in.By)
	if by == "" {
		by = "agent"
	}
	p := &Proposal{
		Chain: rec.Chain, RunID: rec.RunID, Target: rec.Target, Build: rec.Build,
		ProposedBy: by, ProposedAt: now.UTC(), Checked: in.Checked,
		Supersede: in.Supersede, Replaces: replaces,
		Digest: recordDigest(rec), Report: s.ReportPath(rec.Chain),
		ComparedTo: in.ComparedTo, Unstable: in.Unstable,
	}
	if replaces != "" {
		p.Replaced = in.Replaced
	}
	if err := os.MkdirAll(filepath.Dir(p.Report), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p.Report, []byte(ProposalReport(p, rec)), 0o644); err != nil {
		return nil, err
	}
	if err := writeJSON(s.ProposalPath(rec.Chain), p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) Approve(chainName string, c Confirmation) (*SafeSpot, string, error) {
	p, err := s.LoadProposal(chainName)
	if err != nil {
		return nil, "", err
	}
	rec, err := s.LoadRun(p.Chain, p.RunID)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrProposalChanged, err)
	}
	if recordDigest(rec) != p.Digest {
		return nil, "", fmt.Errorf("%w: run %s was rewritten after it was proposed", ErrProposalChanged, p.RunID)
	}
	if err := s.checkSealed(rec); err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrProposalChanged, err)
	}
	if strings.TrimSpace(c.Note) == "" {
		c.Note = p.Checked
	}
	c.Supersede = p.Supersede
	c.Proposal = p
	spot, path, err := s.Promote(rec, c)
	if err != nil {
		return nil, "", err
	}
	s.DropProposal(chainName)
	return spot, path, nil
}

func (s *Store) LoadProposal(chainName string) (*Proposal, error) {
	path := s.ProposalPath(chainName)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%w for chain %s", ErrNoProposal, chainName)
	}
	p := &Proposal{}
	if err := readJSON(path, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) ListProposals() ([]*Proposal, error) {
	names, err := listJSONFiles(s.pendingDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := []*Proposal{}
	for _, n := range names {
		p := &Proposal{}
		if err := readJSON(filepath.Join(s.pendingDir(), n), p); err == nil {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chain < out[j].Chain })
	return out, nil
}

func (s *Store) DropProposal(chainName string) bool {
	err := os.Remove(s.ProposalPath(chainName))
	os.Remove(s.ReportPath(chainName))
	return err == nil
}

func (s *Store) HasProposal(chainName string) bool {
	_, err := os.Stat(s.ProposalPath(chainName))
	return err == nil
}

func (s *Store) ProposalPath(chainName string) string {
	return filepath.Join(s.pendingDir(), slug(chainName)+".json")
}

func (s *Store) ReportPath(chainName string) string {
	return filepath.Join(s.pendingDir(), slug(chainName)+".md")
}

func (s *Store) pendingDir() string {
	return filepath.Join(s.SafeSpotsDir, "pending")
}

const reportExcerpt = 600

func ProposalReport(p *Proposal, rec *runner.Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Safe spot proposal: %s\n\n", p.Chain)
	fmt.Fprintf(&b, "Proposed by %s at %s. Nothing is a safe spot until a person approves it.\n\n",
		p.ProposedBy, p.ProposedAt.Format(time.RFC3339))
	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| run | `%s` |\n| target | `%s` |\n", p.RunID, p.Target)
	if p.Build != "" {
		fmt.Fprintf(&b, "| build | `%s` |\n", p.Build)
	}
	fmt.Fprintf(&b, "| status | %s, %d step(s), %d ms |\n", rec.Status, len(rec.Steps), rec.DurationMS)
	if len(rec.Vars) > 0 {
		keys := make([]string, 0, len(rec.Vars))
		for k := range rec.Vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("`%s=%v`", k, rec.Vars[k]))
		}
		fmt.Fprintf(&b, "| vars | %s |\n", strings.Join(parts, " "))
	}
	if p.Replaces != "" {
		fmt.Fprintf(&b, "| replaces | the safe spot from run `%s`, which is archived on approval |\n", p.Replaces)
	}
	fmt.Fprintf(&b, "| digest | `%s` |\n\n", p.Digest)

	b.WriteString(ProposalSummary(p, rec) + "\n")
	b.WriteString("## What the proposer checked\n\n")
	b.WriteString(p.Checked + "\n\n")

	b.WriteString("## Steps\n\n")
	for _, st := range rec.Steps {
		fmt.Fprintf(&b, "### %d. %s (`%s`), %s\n\n", st.Index, st.ID, st.Call, st.Status)
		if len(st.Expect) == 0 {
			b.WriteString("No expectations: this step's response is recorded but nothing about it was asserted.\n\n")
		} else {
			for _, e := range st.Expect {
				mark := "held"
				if !e.Passed {
					mark = "FAILED"
				}
				want := ""
				if e.Want != nil {
					want = fmt.Sprintf(" %v", e.Want)
				}
				fmt.Fprintf(&b, "- `%s` %s%s, got `%v`: %s\n", e.Path, e.Rule, want, e.Got, mark)
			}
			b.WriteString("\n")
		}
		if len(st.Response) > 0 {
			text := string(st.Response)
			var pretty bytes.Buffer
			if json.Indent(&pretty, st.Response, "", "  ") == nil {
				text = pretty.String()
			}
			if len(text) > reportExcerpt {
				text = text[:reportExcerpt] + " …"
			}
			fmt.Fprintf(&b, "```json\n%s\n```\n\n", text)
		}
	}

	b.WriteString("## Your decision\n\n")
	b.WriteString("Approving says the responses above are CORRECT for this backend, not merely green: every later\n")
	b.WriteString("`shrt verify` of this chain is judged against them. Read the responses, not only the assertions.\n\n")
	fmt.Fprintf(&b, "- approve: `shrt confirm %s -approve -by <your email>`, or tell the agent yes and it runs that\n", p.Chain)
	fmt.Fprintf(&b, "- reject: `shrt confirm %s -reject`\n", p.Chain)
	return b.String()
}

func ProposalSummary(p *Proposal, rec *runner.Record) string {
	var b strings.Builder
	passed := 0
	for _, st := range rec.Steps {
		if st.Status == runner.StatusPassed {
			passed++
		}
	}
	fmt.Fprintf(&b, "**Safe spot proposal: `%s`**, run `%s`, %d/%d steps passed, target `%s`", p.Chain, p.RunID, passed, len(rec.Steps), p.Target)
	if p.Build != "" {
		fmt.Fprintf(&b, ", build `%s`", p.Build)
	}
	if p.Replaces == "" {
		b.WriteString("\n\n| # | step | sent | asserted, all held | backend answered |\n|---|---|---|---|---|\n")
	} else {
		fmt.Fprintf(&b, "\n\n| # | step | sent | asserted, all held | backend answered | vs replaced safe spot `%s` |\n|---|---|---|---|---|---|\n", p.Replaces)
	}
	for _, st := range rec.Steps {
		answered := answeredSummary(st)
		if also := alsoBaselined(rec, st); also != "" {
			answered += "; also baselined: " + also
		}
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s |", st.Index, cell(st.ID), cell(sentSummary(st)), assertedSummary(st), answered)
		if p.Replaces != "" {
			fmt.Fprintf(&b, " %s |", cell(replacedSummary(p.Replaced, st.ID)))
		}
		b.WriteString("\n")
	}
	if p.Replaces != "" {
		fmt.Fprintf(&b, "\nApproving replaces the safe spot from run `%s`, which is archived.\n", p.Replaces)
		if len(p.Replaced) == 0 {
			b.WriteString("It sent the same requests and got the same responses, beyond ids and timestamps.\n")
		} else {
			fmt.Fprintf(&b, "**%d difference(s) from the safe spot it replaces**, what approving signs off on:\n\n", len(p.Replaced))
			for _, d := range p.Replaced {
				fmt.Fprintf(&b, "- `%s` %s\n", d.Step, flat(d.String()))
			}
		}
	}
	switch {
	case p.ComparedTo == "":
		fmt.Fprintf(&b, "\n**Not checked for fields that change every run:** no earlier passing run of this chain against `%s` is recorded "+
			"(a run against another target says nothing about this one). Run it once more and propose again to see them.\n", p.Target)
	case len(p.Unstable) == 0:
		fmt.Fprintf(&b, "\nCompared with the earlier passing run `%s`: no field differs beyond ids, timestamps and values echoing a fixture name, so `shrt verify` should not report drift on an unchanged backend.\n", p.ComparedTo)
	default:
		fmt.Fprintf(&b, "\n**Warning: %d field(s) differ from the earlier passing run `%s`** and are not declared volatile, so every `shrt verify` will report them as drift unless the chain's `volatile:` covers them (or they are a real difference):\n\n", len(p.Unstable), p.ComparedTo)
		for _, u := range p.Unstable {
			fmt.Fprintf(&b, "- `%s`\n", u)
		}
	}
	redacted, scrubbed := redactedSummary(rec)
	if len(redacted) > 0 {
		fmt.Fprintf(&b, "\n**Redacted, never compared by `shrt verify`:** %s. A `redact` path blanks the value in every run record, "+
			"so the safe spot holds no value there and verify cannot see it change; assert it in the chain if it matters.\n", strings.Join(redacted, ", "))
	}
	if len(scrubbed) > 0 {
		fmt.Fprintf(&b, "\n**Scrubbed by value, never compared by `shrt verify`:** %s. No `redact` path covers them: each held a secret "+
			"the run knew (a credential or token it sent), so the value was blanked; stop echoing the secret there if the field matters.\n", strings.Join(scrubbed, ", "))
	}
	patterns := volatileSummary(rec)
	if len(patterns) == 0 {
		b.WriteString("\nApproving makes every response field above, not only the asserted ones, the baseline `shrt verify` compares against.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "\n**Volatile, never compared by `shrt verify`:** %s\n", strings.Join(patterns, ", "))
	if masked := fullyMasked(rec); len(masked) > 0 {
		fmt.Fprintf(&b, "\n**Warning: every response field of step(s) %s is volatile**, so `shrt verify` compares nothing of those responses "+
			"and approving sets no baseline for them. Narrow the `volatile` patterns unless that is intended.\n", strings.Join(masked, ", "))
	}
	b.WriteString("\nApproving makes every response field above that no volatile pattern covers, not only the asserted ones, the baseline `shrt verify` compares against.\n")
	return b.String()
}

func redactedSummary(rec *runner.Record) ([]string, []string) {
	paths := pathmask.NewMasker(rec.Redacted)
	redacted, scrubbed := []string{}, []string{}
	for _, st := range rec.Steps {
		for _, p := range pathmask.RedactedPaths(st.Response) {
			if len(rec.Redacted) > 0 && !paths.Masks(p) {
				scrubbed = append(scrubbed, "`"+st.ID+" "+p+"`")
				continue
			}
			redacted = append(redacted, "`"+st.ID+" "+p+"`")
		}
	}
	return redacted, scrubbed
}

func volatileSummary(rec *runner.Record) []string {
	out := []string{}
	for _, p := range rec.Volatile {
		out = append(out, "`"+p+"`")
	}
	for _, st := range rec.Steps {
		for _, p := range st.Volatile {
			out = append(out, fmt.Sprintf("`%s` (%s)", p, st.ID))
		}
	}
	return out
}

func fullyMasked(rec *runner.Record) []string {
	out := []string{}
	for _, st := range rec.Steps {
		var body any
		if len(st.Response) == 0 || json.Unmarshal(st.Response, &body) != nil {
			continue
		}
		masker := pathmask.NewMasker(append(append([]string{}, rec.Volatile...), st.Volatile...))
		if leaves, open := maskedLeaves(masker.Apply(body)); leaves > 0 && open == 0 {
			out = append(out, st.ID)
		}
	}
	return out
}

func maskedLeaves(v any) (int, int) {
	masked, open := 0, 0
	add := func(m, o int) { masked, open = masked+m, open+o }
	switch t := v.(type) {
	case map[string]any:
		for _, item := range t {
			add(maskedLeaves(item))
		}
	case []any:
		for _, item := range t {
			add(maskedLeaves(item))
		}
	case string:
		if t == pathmask.MaskVolatile {
			return 1, 0
		}
		return 0, 1
	default:
		return 0, 1
	}
	return masked, open
}

func replacedSummary(all []Differ, step string) string {
	parts := []string{}
	for _, d := range all {
		if d.Step == step {
			parts = append(parts, d.String())
		}
	}
	if len(parts) == 0 {
		return "same"
	}
	return strings.Join(parts, "; ")
}

const (
	summaryCell   = 90
	answeredCell  = 160
	summaryValue  = 24
	assertedCell  = 320
	sentFieldsMax = 6

	alsoBaselinedMax = 4
)

func cell(s string) string {
	return clip(flat(s), summaryCell)
}

func flat(s string) string {
	var b strings.Builder
	quoted, escaped, space := false, false, false
	for _, r := range strings.ReplaceAll(s, "|", "/") {
		white := unicode.IsSpace(r)
		switch {
		case quoted && r == ' ':
			b.WriteRune(r)
			continue
		case white:
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
		switch {
		case escaped:
			escaped = false
		case r == '\\' && quoted:
			escaped = true
		case r == '"':
			quoted = !quoted
		}
	}
	return b.String()
}

func clip(s string, n int) string {
	if len([]rune(s)) > n {
		s = string([]rune(s)[:n]) + "…"
	}
	return s
}

func shortValue(v any) string {
	if s, isString := v.(string); isString && (s == "" || strings.TrimSpace(s) != s) {
		return clip(strconv.Quote(s), summaryValue)
	}
	if b, err := json.Marshal(v); err == nil {
		if _, isString := v.(string); !isString {
			return clip(flat(string(b)), summaryValue)
		}
	}
	return clip(flat(fmt.Sprint(v)), summaryValue)
}

func sentSummary(st *runner.StepRecord) string {
	var body any
	if len(st.Request) == 0 || json.Unmarshal(st.Request, &body) != nil {
		return "-"
	}
	leaves, refs := []string{}, []string{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch t := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(joinKey(prefix, k), t[k])
			}
		case []any:
			if len(t) == 0 {
				leaves = append(leaves, prefix+"=[]")
			}
			for i, e := range t {
				walk(joinKey(prefix, fmt.Sprint(i)), e)
			}
		default:
			leaf := prefix + "=" + shortValue(t)
			if referenceLike(prefix, t) {
				refs = append(refs, leaf)
			} else {
				leaves = append(leaves, leaf)
			}
		}
	}
	walk("", body)
	leaves = append(leaves, refs...)
	if len(leaves) == 0 {
		return "{}"
	}
	out := leaves
	if len(out) > sentFieldsMax {
		out = append(append([]string(nil), out[:sentFieldsMax]...), fmt.Sprintf("+%d more", len(leaves)-sentFieldsMax))
	}
	return strings.Join(out, " ")
}

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func referenceLike(path string, v any) bool {
	key := path
	if i := strings.LastIndex(path, "."); i >= 0 {
		key = path[i+1:]
	}
	lower := strings.ToLower(key)
	switch {
	case lower == "id", strings.HasPrefix(lower, "id_"), strings.HasSuffix(lower, "_id"),
		strings.HasSuffix(key, "Id"), strings.HasSuffix(key, "ID"), strings.Contains(lower, "idempotency"):
		return true
	}
	text, ok := v.(string)
	return ok && uuidShape.MatchString(text)
}

func joinKey(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + "." + k
}

func assertedSummary(st *runner.StepRecord) string {
	if len(st.Expect) == 0 {
		return "nothing asserted"
	}
	parts := make([]string, 0, len(st.Expect))
	for _, e := range st.Expect {
		part := e.Path + " " + e.Rule
		if e.Want != nil {
			part += " " + shortValue(e.Want)
		}
		parts = append(parts, flat(part))
	}
	out := ""
	for i, part := range parts {
		next := part
		if i > 0 {
			next = out + "; " + part
		}
		if i > 0 && len([]rune(next)) > assertedCell {
			return out + fmt.Sprintf("; +%d more", len(parts)-i)
		}
		out = next
	}
	return out
}

func answeredSummary(st *runner.StepRecord) string {
	head, pairs := answerParts(st)
	out := clip(flat(head), answeredCell)
	for i, pair := range pairs {
		pair = flat(pair)
		sep := " "
		if i == 0 {
			sep = "; "
		}
		rest := ""
		if i < len(pairs)-1 {
			rest = fmt.Sprintf(" +%d more", len(pairs)-i-1)
		}
		if len([]rune(out+sep+pair+rest)) > answeredCell {
			if i == 0 {
				return out + fmt.Sprintf("; +%d more", len(pairs))
			}
			return out + fmt.Sprintf(" +%d more", len(pairs)-i)
		}
		out += sep + pair
	}
	return out
}

func answerParts(st *runner.StepRecord) (string, []string) {
	if st.Transport != nil {
		if st.HTTPStatus != 0 {
			return fmt.Sprintf("HTTP %d %s: %s", st.HTTPStatus, st.Transport.Code, st.Transport.Message), nil
		}
		return st.Transport.Code + ": " + st.Transport.Message, nil
	}
	var body any
	if json.Unmarshal(st.Response, &body) != nil {
		return st.Status, nil
	}
	path := chain.EnvelopePath()
	out, ok := verdictText(body, path, "details.0.app_code", "details.0.reason", "message")
	if !ok {
		return "answered, nothing at " + path, nil
	}
	if items := itemsSummary(body); items != "" {
		out += "; items: " + items
	}
	return out, assertedValues(st, path)
}

func alsoBaselined(rec *runner.Record, st *runner.StepRecord) string {
	var body any
	if st.Transport != nil || len(st.Response) == 0 || json.Unmarshal(st.Response, &body) != nil {
		return ""
	}
	asserted := map[string]bool{chain.EnvelopePath(): true}
	for _, e := range st.Expect {
		asserted[e.Path] = true
	}
	volatile := pathmask.NewMasker(append(append([]string{}, rec.Volatile...), st.Volatile...))
	fixtures := []string{}
	for _, v := range rec.Vars {
		if text, ok := v.(string); ok && len(text) >= 2 && strings.Trim(text, "0123456789.-") != "" {
			fixtures = append(fixtures, text)
		}
	}
	type leaf struct {
		path  string
		depth int
		value any
	}
	leaves := []leaf{}
	var walk func(path string, depth int, v any)
	walk = func(path string, depth int, v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				walk(joinKey(path, k), depth+1, x)
			}
		case []any:
			for i, x := range t {
				walk(joinKey(path, fmt.Sprint(i)), depth+1, x)
			}
		case nil:
		default:
			if asserted[path] || volatile.Masks(path) || referenceLike(path, t) || baselineNoise(t, fixtures) {
				return
			}
			leaves = append(leaves, leaf{path, depth, t})
		}
	}
	walk("", 0, body)
	sort.Slice(leaves, func(i, j int) bool {
		if leaves[i].depth != leaves[j].depth {
			return leaves[i].depth < leaves[j].depth
		}
		return leaves[i].path < leaves[j].path
	})
	parts := []string{}
	for i, l := range leaves {
		if i == alsoBaselinedMax {
			parts = append(parts, fmt.Sprintf("+%d more", len(leaves)-i))
			break
		}
		parts = append(parts, l.path+"="+shortValue(l.value))
	}
	return strings.Join(parts, " ")
}

func baselineNoise(v any, fixtures []string) bool {
	text, ok := v.(string)
	if !ok {
		return false
	}
	if text == "" || strings.Contains(text, pathmask.MaskRedacted) {
		return true
	}
	if _, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return true
	}
	for _, f := range fixtures {
		if strings.Contains(text, f) {
			return true
		}
	}
	return false
}

func assertedValues(st *runner.StepRecord, envelope string) []string {
	seen := map[string]bool{envelope: true}
	parts := []string{}
	for _, e := range st.Expect {
		if seen[e.Path] || e.Got == nil || chain.IsTransportPath(e.Path) || referenceLike(e.Path, e.Got) {
			continue
		}
		seen[e.Path] = true
		if present, ok := e.Got.(bool); ok && e.Rule == "exists" {
			parts = append(parts, e.Path+map[bool]string{true: " present", false: " absent"}[present])
			continue
		}
		parts = append(parts, e.Path+"="+shortValue(e.Got))
	}
	return parts
}

func verdictText(body any, path string, extra ...string) (string, bool) {
	verdict, ok := chain.Get(body, path)
	if !ok {
		return "", false
	}
	out := fmt.Sprint(verdict)
	parent := ""
	if i := strings.LastIndex(path, "."); i > 0 {
		parent = path[:i] + "."
	}
	for _, field := range extra {
		if v, ok := chain.Get(body, parent+field); ok && fmt.Sprint(v) != "" {
			out += " " + fmt.Sprint(v)
		}
	}
	return out, true
}

func itemsSummary(body any) string {
	list, field, ok := strings.Cut(chain.ItemEnvelope(), "[].")
	if !ok {
		return ""
	}
	rows, found := chain.Get(body, strings.TrimSuffix(list, "."))
	items, isList := rows.([]any)
	if !found || !isList || len(items) == 0 {
		return ""
	}
	order := []string{}
	counts := map[string]int{}
	for _, item := range items {
		text, ok := verdictText(item, field, "details.0.app_code", "details.0.reason")
		if !ok {
			text = "no verdict"
		}
		if counts[text] == 0 {
			order = append(order, text)
		}
		counts[text]++
	}
	parts := make([]string, 0, len(order))
	for _, text := range order {
		parts = append(parts, fmt.Sprintf("%d %s", counts[text], text))
	}
	return strings.Join(parts, ", ")
}
