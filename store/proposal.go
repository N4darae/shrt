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
	"unicode/utf8"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
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
	Carried    []string  `json:"differs_where_earlier_run_held_replaced,omitempty"`
	Replaced   []Differ  `json:"differs_from_replaced,omitempty"`

	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`
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
	Carried    []string
	Replaced   []Differ
	Branch     string
	Commit     string
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
		Branch: in.Branch, Commit: in.Commit,
	}
	if replaces != "" {
		p.Replaced = in.Replaced
		p.Carried = in.Carried
	} else {
		p.Unstable = append(p.Unstable, in.Carried...)
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

func (p *Proposal) ProposedOn() string {
	switch {
	case p.Branch != "" && p.Commit != "":
		return fmt.Sprintf("branch `%s` at commit `%s`", p.Branch, p.Commit)
	case p.Branch != "":
		return fmt.Sprintf("branch `%s`", p.Branch)
	case p.Commit != "":
		return fmt.Sprintf("commit `%s`", p.Commit)
	}
	return ""
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
	b.WriteString(proposalHeader(p, rec))
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
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s |", st.Index, cell(st.ID), flat(sentSummary(st)), assertedSummary(st), answered)
		if p.Replaces != "" {
			fmt.Fprintf(&b, " %s |", cell(replacedSummary(p.Replaced, st.ID)))
		}
		b.WriteString("\n")
	}
	b.WriteString(proposalFindings(p, rec, 0))
	return b.String()
}

const briefList = 8

func ProposalBrief(p *Proposal, rec *runner.Record, description string) string {
	var b strings.Builder
	b.WriteString(proposalHeader(p, rec) + "\n\n")
	if description = strings.TrimSpace(description); description != "" {
		fmt.Fprintf(&b, "What it does: %s\n\n", flat(description))
	}
	fmt.Fprintf(&b, "What the proposer checked: %s\n\n", flat(p.Checked))
	calls, answers, fields := counter{}, counter{}, counter{}
	asserted, bare, verdictOnly, warned := 0, []string{}, []string{}, []string{}
	envelope := chain.EnvelopePath()
	for _, st := range rec.Steps {
		calls.add(shortCall(st.Call))
		answers.add(answerKind(st, envelope))
		asserted += len(st.Expect)
		if len(st.Expect) == 0 {
			bare = append(bare, "`"+st.ID+"`")
		}
		beyond := false
		for _, e := range st.Expect {
			if !atEnvelope(e.Path, envelope) && !chain.IsTransportPath(e.Path) {
				beyond = true
				fields.add(indexPattern.ReplaceAllString(e.Path, ".N"))
			}
		}
		if len(st.Expect) > 0 && !beyond && answerKind(st, envelope) == chain.EnvelopeOK() {
			verdictOnly = append(verdictOnly, "`"+st.ID+"`")
		}
		if st.Warning != "" {
			warned = append(warned, fmt.Sprintf("`%s` %s", st.ID, clip(flat(st.Warning), summaryCell)))
		}
	}
	fmt.Fprintf(&b, "- %d steps calling %s\n", len(rec.Steps), calls.top(briefList))
	fmt.Fprintf(&b, "- answered, as each step expected: %s\n", answers.top(briefList))
	fmt.Fprintf(&b, "- %d assertions, all held; the fields asserted most: %s\n", asserted, fields.top(briefList))
	if len(bare) > 0 {
		fmt.Fprintf(&b, "- **%d step(s) assert nothing**, so only the baseline checks them: %s\n", len(bare), clipList(bare, briefList))
	}
	if len(verdictOnly) > 0 {
		fmt.Fprintf(&b, "- %d successful step(s) assert only the verdict: %s\n", len(verdictOnly), clipList(verdictOnly, briefList))
	}
	if len(warned) > 0 {
		fmt.Fprintf(&b, "- **%d step(s) carry a warning**: %s\n", len(warned), clipList(warned, 3))
	}
	b.WriteString(proposalFindings(p, rec, briefList))
	return b.String()
}

var indexPattern = regexp.MustCompile(`\.[0-9]+`)

type counter struct {
	order []string
	n     map[string]int
}

func (c *counter) add(key string) {
	if c.n == nil {
		c.n = map[string]int{}
	}
	if c.n[key] == 0 {
		c.order = append(c.order, key)
	}
	c.n[key]++
}

func (c *counter) top(max int) string {
	keys := append([]string{}, c.order...)
	sort.SliceStable(keys, func(i, j int) bool { return c.n[keys[i]] > c.n[keys[j]] })
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s ×%d", k, c.n[k]))
	}
	if len(parts) == 0 {
		return "none"
	}
	return clipList(parts, max)
}

func clipList(items []string, max int) string {
	if max > 0 && len(items) > max {
		return strings.Join(items[:max], ", ") + fmt.Sprintf(" and %d more", len(items)-max)
	}
	return strings.Join(items, ", ")
}

func shortCall(call string) string {
	if i := strings.LastIndex(call, "/"); i >= 0 {
		return call[i+1:]
	}
	return call
}

func answerKind(st *runner.StepRecord, envelope string) string {
	if st.Transport != nil {
		return st.Transport.Code
	}
	var body any
	if json.Unmarshal(st.Response, &body) != nil {
		return st.Status
	}
	out, ok := verdictText(body, envelope, "details.0.app_code", "details.0.reason")
	if ok {
		return out
	}
	top, _ := body.(map[string]any)
	messages, _ := top[catalog.StreamMessages].([]any)
	for _, m := range messages {
		v, found := verdictText(m, envelope, "details.0.app_code", "details.0.reason")
		if found && !ok {
			out, ok = v, true
		}
		if found && v != chain.EnvelopeOK() {
			out = v
			break
		}
	}
	if !ok {
		return "nothing at " + envelope
	}
	return out
}

var streamedPrefix = regexp.MustCompile(`^` + catalog.StreamMessages + `\.\d+\.`)

func atEnvelope(path, envelope string) bool {
	return path == envelope || streamedPrefix.MatchString(path) && streamedPrefix.ReplaceAllString(path, "") == envelope
}

func proposalHeader(p *Proposal, rec *runner.Record) string {
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
	if rec.ChainSource != "" {
		fmt.Fprintf(&b, ", chain file `%s`", filepath.Base(rec.ChainSource))
	}
	if where := p.ProposedOn(); where != "" {
		fmt.Fprintf(&b, ", proposed on %s", where)
	}
	return b.String()
}

func proposalFindings(p *Proposal, rec *runner.Record, max int) string {
	var b strings.Builder
	if p.Replaces != "" {
		fmt.Fprintf(&b, "\nApproving replaces the safe spot from run `%s`, which is archived.\n", p.Replaces)
		if len(p.Replaced) == 0 {
			b.WriteString("It sent the same requests and got the same responses, beyond ids, timestamps and values that only echo a fixture name (`sku-${vars.tag}`), which verify masks too.\n")
		} else {
			fmt.Fprintf(&b, "**%d difference(s) from the safe spot it replaces**, what approving signs off on:\n\n", len(p.Replaced))
			for i, d := range p.Replaced {
				if max > 0 && i == max {
					fmt.Fprintf(&b, "- and %d more in the full report\n", len(p.Replaced)-max)
					break
				}
				fmt.Fprintf(&b, "- `%s` %s\n", d.Step, flat(d.String()))
			}
		}
	}
	if p.ComparedTo != "" && len(p.Carried) > 0 {
		fmt.Fprintf(&b, "\n%d field(s) differ from the earlier passing run `%s` only where that run still held what the safe spot it replaces holds, "+
			"so it was recorded before the change this proposal signs off on: they are that change, not values that change every run, "+
			"and they were not checked for that. Run the chain once more and propose again to check them against a run of the new backend: %s\n",
			len(p.Carried), p.ComparedTo, clipList(carriedPaths(p.Carried), max))
	}
	switch {
	case p.ComparedTo == "":
		fmt.Fprintf(&b, "\n**Not checked for fields that change every run:** no earlier passing run of this chain against `%s` is recorded "+
			"(a run against another target says nothing about this one). Run it once more and propose again to see them.\n", p.Target)
	case len(p.Unstable) > 0:
		fmt.Fprintf(&b, "\n**Warning: %d field(s) differ from the earlier passing run `%s`** and are not declared volatile, so every `shrt verify` will report them as drift unless the chain's `volatile:` covers them (or they are a real difference):\n\n", len(p.Unstable), p.ComparedTo)
		for i, line := range unstableLines(p.Unstable) {
			if max > 0 && i == max {
				fmt.Fprintf(&b, "- and more in the full report\n")
				break
			}
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	redacted, scrubbed := redactedSummary(rec)
	if len(redacted) > 0 {
		fmt.Fprintf(&b, "\n**Redacted, never compared by `shrt verify`:** %s. A `redact` path blanks the value in every run record, "+
			"so the safe spot holds no value there and verify cannot see it change; assert it in the chain if it matters.\n", clipList(redacted, max))
	}
	if len(scrubbed) > 0 {
		fmt.Fprintf(&b, "\n**Scrubbed by value, never compared by `shrt verify`:** %s. No `redact` path covers them: each held a secret "+
			"the run knew (a credential or token it sent), so the value was blanked; stop echoing the secret there if the field matters.\n", clipList(scrubbed, max))
	}
	patterns := volatileSummary(rec)
	if len(patterns) == 0 {
		b.WriteString("\nApproving makes every response field of the run, not only the asserted ones, the baseline `shrt verify` compares against.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "\n**Volatile, never compared by `shrt verify`:** %s\n", clipList(patterns, max))
	if masked := fullyMasked(rec); len(masked) > 0 {
		fmt.Fprintf(&b, "\n**Warning: every response field of step(s) %s is volatile**, so `shrt verify` compares nothing of those responses "+
			"and approving sets no baseline for them. Narrow the `volatile` patterns unless that is intended.\n", strings.Join(masked, ", "))
	}
	b.WriteString("\nApproving makes every response field of the run that no volatile pattern covers, not only the asserted ones, the baseline `shrt verify` compares against.\n")
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
	summaryValue  = 24
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
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := n
	if at := lastRune(r[:n+1], ' '); at > n/2 {
		cut = at
	} else if at := segmentPrefix(r, n); len(at) > n/2 {
		cut = len(at)
	}
	return strings.TrimRight(string(r[:cut]), " ") + "…"
}

func shortValue(v any) string {
	if s, isString := v.(string); isString && (s == "" || strings.TrimSpace(s) != s) {
		return clipMiddle(strconv.Quote(s), summaryValue)
	}
	if b, err := json.Marshal(v); err == nil {
		if _, isString := v.(string); !isString {
			return clipMiddle(flat(string(b)), summaryValue)
		}
	}
	return clipMiddle(flat(fmt.Sprint(v)), summaryValue)
}

func fullValue(v any) string {
	if s, isString := v.(string); isString && (s == "" || strings.TrimSpace(s) != s) {
		return strconv.Quote(s)
	}
	if _, isString := v.(string); !isString {
		if b, err := json.Marshal(v); err == nil {
			return flat(string(b))
		}
	}
	return flat(fmt.Sprint(v))
}

func clipMiddle(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if strings.Contains(s, " ") {
		if out, ok := clipWords(strings.Split(s, " "), n); ok {
			return out
		}
	}
	return clipToken(r, n)
}

func clipWords(words []string, n int) (string, bool) {
	budget := n - 2
	tail, used := 0, 0
	for i := len(words) - 1; i > 0; i-- {
		w := utf8.RuneCountInString(words[i]) + 1
		if used+w > budget/2 {
			break
		}
		used += w
		tail++
	}
	head := 0
	for head < len(words)-tail {
		w := utf8.RuneCountInString(words[head]) + 1
		if used+w > budget {
			break
		}
		used += w
		head++
	}
	if head == 0 && tail == 0 {
		return "", false
	}
	parts := append([]string{}, words[:head]...)
	parts = append(parts, "…")
	parts = append(parts, words[len(words)-tail:]...)
	return strings.Join(parts, " "), true
}

func clipToken(r []rune, n int) string {
	budget := n - 1
	var tail []rune
	if at := lastRune(r, '@'); at > 0 && len(r)-at <= budget-2 {
		tail = r[at:]
	} else {
		tail = segmentSuffix(r, budget-budget/3)
	}
	head := segmentPrefix(r[:len(r)-len(tail)], budget-len(tail))
	return string(head) + "…" + string(tail)
}

func segmentSeparator(c rune) bool {
	return c == '-' || c == '_' || c == '.' || c == '/' || c == ':' || c == '@'
}

func lastRune(r []rune, c rune) int {
	for i := len(r) - 1; i >= 0; i-- {
		if r[i] == c {
			return i
		}
	}
	return -1
}

func segmentPrefix(r []rune, max int) []rune {
	if len(r) <= max {
		return r
	}
	for i := max; i > 0; i-- {
		if segmentSeparator(r[i-1]) {
			return r[:i]
		}
	}
	return r[:max]
}

func segmentSuffix(r []rune, max int) []rune {
	if len(r) <= max {
		return r
	}
	for i := len(r) - max; i < len(r); i++ {
		if i > 0 && segmentSeparator(r[i-1]) {
			return r[i:]
		}
	}
	return r[len(r)-max:]
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
			if referenceLike(prefix, t) {
				refs = append(refs, prefix+"="+shortValue(t))
			} else {
				leaves = append(leaves, prefix+"="+fullValue(t))
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
			part += " " + fullValue(e.Want)
		}
		parts = append(parts, flat(part))
	}
	return strings.Join(parts, "; ")
}

func answeredSummary(st *runner.StepRecord) string {
	head, pairs := answerParts(st)
	out := flat(head)
	for i, pair := range pairs {
		sep := " "
		if i == 0 {
			sep = "; "
		}
		out += sep + flat(pair)
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
	unordered := map[string]bool{}
	for _, p := range st.Unordered {
		unordered[listKey(p)] = true
	}
	var walk func(path string, depth int, v any)
	walk = func(path string, depth int, v any) {
		if path != "" && volatile.Masks(path) {
			return
		}
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				walk(joinKey(path, k), depth+1, x)
			}
		case []any:
			if unordered[listKey(path)] && len(t) > 0 {
				leaves = append(leaves, leaf{path, depth, fmt.Sprintf("%d item(s) in any order", len(t))})
				return
			}
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
		parts = append(parts, l.path+"="+fullValue(l.value))
	}
	return strings.Join(parts, " ")
}

func listKey(path string) string {
	out := []string{}
	for _, seg := range strings.Split(path, ".") {
		if _, err := strconv.Atoi(seg); err != nil && seg != "" {
			out = append(out, seg)
		}
	}
	return namecase.Fold(strings.Join(out, "."))
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
		parts = append(parts, e.Path+"="+fullValue(e.Got))
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

func itemRefusals(st *runner.StepRecord) []string {
	list, field, ok := strings.Cut(chain.ItemEnvelope(), "[].")
	var body any
	if !ok || json.Unmarshal(st.Response, &body) != nil {
		return nil
	}
	scope, _, _ := strings.Cut(field, ".")
	var out []string
	seen := map[string]bool{}
	for _, e := range st.Expect {
		rest, under := strings.CutPrefix(e.Path, list+".")
		index, sub, _ := strings.Cut(rest, ".")
		if !under || !e.Passed || seen[index] || sub != scope && !strings.HasPrefix(sub, scope+".") {
			continue
		}
		item, _ := chain.Get(body, list+"."+index)
		if v, found := chain.Get(item, field); found && fmt.Sprint(v) != chain.EnvelopeOK() {
			seen[index] = true
			text, _ := verdictText(item, field, "details.0.app_code", "details.0.reason")
			out = append(out, text)
		}
	}
	return out
}

func carriedPaths(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		path, _, _ := strings.Cut(l, ": ")
		out = append(out, "`"+path+"`")
	}
	return out
}

func unstableLines(unstable []string) []string {
	type list struct{ step, path string }
	type entry struct{ path, delta string }
	lists := map[list]bool{}
	for _, u := range unstable {
		step, path, _ := strings.Cut(u, " ")
		path, _, _ = strings.Cut(path, ": ")
		if root, indexed := listRoot(path); indexed {
			lists[list{step, root}] = true
		}
	}
	order := []any{}
	entries := map[list][]entry{}
	for _, u := range unstable {
		step, rest, _ := strings.Cut(u, " ")
		path, delta, _ := strings.Cut(rest, ": ")
		root, _ := listRoot(path)
		key := list{step, root}
		if !lists[key] {
			order = append(order, u)
			continue
		}
		if _, seen := entries[key]; !seen {
			order = append(order, key)
		}
		entries[key] = append(entries[key], entry{path, delta})
	}
	out := make([]string, 0, len(order))
	for _, o := range order {
		switch t := o.(type) {
		case string:
			step, rest, _ := strings.Cut(t, " ")
			path, delta, _ := strings.Cut(rest, ": ")
			line := "`" + step + " " + path + "`"
			if delta != "" {
				line += " " + delta
			}
			out = append(out, line)
		case list:
			es := entries[t]
			grew, known, fields, items := false, true, map[string]bool{}, map[string]bool{}
			for _, e := range es {
				if e.delta == "" {
					known = false
				}
				field, index := fieldPattern(t.path, e.path)
				if e.path == t.path || (strings.Contains(e.delta, "absent") && field == t.path+".*") {
					grew = true
				}
				fields[field] = true
				items[index] = true
			}
			if known && !grew {
				shown, patterns := []string{}, []string{}
				for _, e := range es {
					shown = append(shown, fmt.Sprintf("`%s` %s", e.path, e.delta))
				}
				for p := range fields {
					patterns = append(patterns, p)
				}
				sort.Strings(patterns)
				if len(shown) > 4 {
					shown = append(shown[:4], fmt.Sprintf("and %d more", len(shown)-4))
				}
				line := fmt.Sprintf("`%s %s`: the list has as many items as in the earlier run, and only %d field(s) inside them differ: %s. "+
					"That may be a real change; if the field varies from run to run, declare `volatile: [%s]` on step `%s`",
					t.step, t.path, len(es), strings.Join(shown, ", "), strings.Join(patterns, ", "), t.step)
				if len(items) > 1 {
					line += fmt.Sprintf(" (or `unordered: [%s]` if only the items' order changes)", t.path)
				}
				out = append(out, line)
				continue
			}
			grown := ""
			for _, e := range es {
				if e.path == t.path && e.delta != "" {
					grown = " (" + e.delta + ")"
				}
			}
			out = append(out, fmt.Sprintf("`%s %s`: %d field(s) of this list differ%s, so its items change from run to run "+
				"(a list other runs add to grows every run). If that is expected, declare `volatile: [%s]` on step `%s` "+
				"(or `unordered: [%s]` if only its order changes), or narrow the request to this run's records",
				t.step, t.path, len(es), grown, t.path, t.step, t.path))
		}
	}
	return out
}

func fieldPattern(root, path string) (string, string) {
	rest := strings.TrimPrefix(strings.TrimPrefix(path, root), ".")
	index, field, _ := strings.Cut(rest, ".")
	if field == "" {
		return root + ".*", index
	}
	return root + ".*." + field, index
}

func listRoot(path string) (string, bool) {
	segs := strings.Split(path, ".")
	for i, seg := range segs {
		if _, err := strconv.Atoi(seg); err == nil && i > 0 {
			return strings.Join(segs[:i], "."), true
		}
	}
	return path, false
}

type ProposalRow struct {
	Chain, Run, Steps, Refusals, Check, Volatile string
}

func ProposalRowOf(p *Proposal, rec *runner.Record) ProposalRow {
	passed := 0
	refusals := counter{}
	bare, verdictOnly, warned := 0, 0, 0
	envelope := chain.EnvelopePath()
	for _, st := range rec.Steps {
		if st.Status == runner.StatusPassed {
			passed++
		}
		answer := answerKind(st, envelope)
		if answer != chain.EnvelopeOK() {
			refusals.add(answer)
		}
		for _, text := range itemRefusals(st) {
			refusals.add(text)
		}
		beyond := false
		for _, e := range st.Expect {
			beyond = beyond || !atEnvelope(e.Path, envelope) && !chain.IsTransportPath(e.Path)
		}
		switch {
		case len(st.Expect) == 0:
			bare++
		case !beyond && answer == chain.EnvelopeOK():
			verdictOnly++
		}
		if st.Warning != "" {
			warned++
		}
	}
	check := []string{}
	add := func(n int, what string) {
		if n > 0 {
			check = append(check, fmt.Sprintf("%d %s", n, what))
		}
	}
	add(bare, "step(s) assert nothing")
	add(verdictOnly, "step(s) assert only the verdict")
	add(warned, "warning(s)")
	add(len(p.Replaced), "difference(s) from the replaced safe spot")
	add(len(p.Unstable), "field(s) differ from the earlier run and are not volatile")
	add(len(p.Carried), "field(s) carried from before the change")
	if p.ComparedTo == "" {
		check = append(check, "no earlier passing run to compare with")
	}
	redacted, scrubbed := redactedSummary(rec)
	if len(redacted) > 0 {
		check = append(check, "redacted "+clipList(redacted, 3))
	}
	if len(scrubbed) > 0 {
		check = append(check, "scrubbed "+clipList(scrubbed, 3))
	}
	stepVolatile, chainVolatile := []string{}, []string{}
	for _, st := range rec.Steps {
		for _, v := range st.Volatile {
			stepVolatile = append(stepVolatile, fmt.Sprintf("`%s` (%s)", v, st.ID))
		}
	}
	for _, v := range rec.Volatile {
		chainVolatile = append(chainVolatile, "`"+v+"`")
	}
	if len(stepVolatile) > 0 {
		check = append(check, "volatile "+clipList(stepVolatile, 3))
	}
	if masked := fullyMasked(rec); len(masked) > 0 {
		check = append(check, "every field volatile at "+clipList(masked, 3))
	}
	row := ProposalRow{Chain: p.Chain, Run: p.RunID, Steps: fmt.Sprintf("%d/%d", passed, len(rec.Steps)),
		Refusals: "none", Check: "none", Volatile: strings.Join(chainVolatile, ", ")}
	if len(refusals.order) > 0 {
		row.Refusals = refusals.top(3)
	}
	if len(check) > 0 {
		row.Check = strings.Join(check, "; ")
	}
	return row
}
