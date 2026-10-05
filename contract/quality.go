package contract

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

const (
	WeightUndocumentedField       = 2
	WeightUnexplainedFailure      = 1
	WeightNoFailuresDeclared      = 2
	WeightUnwiredID               = 2
	WeightUncheckedID             = 1
	WeightUndeclaredResponseField = 1
	WeightMissingSummary          = 2
	WeightReadWithNoProducer      = 2
	WeightMissingRequiresRole     = 2
	WeightEmptyRequired           = 2
	WeightNoContract              = 2
)

const (
	PhaseHappy   = "happy"
	PhaseFailure = "failure"
	PhaseAll     = "all"
)

type QualityTerm struct {
	Weight int
	Phase  string
	Label  string
	Count  func(QualityRPC) int
	Detail func(QualityRPC) string
}

func (t QualityTerm) InPhase(phase string) bool {
	return phase == "" || phase == PhaseAll || phase == t.Phase
}

func CheckPhase(phase string) error {
	switch phase {
	case "", PhaseAll, PhaseHappy, PhaseFailure:
		return nil
	}
	return fmt.Errorf("unknown -phase %q, want %s, %s or %s", phase, PhaseHappy, PhaseFailure, PhaseAll)
}

func QualityTerms() []QualityTerm {
	flagged := func(weight int, phase, label string, on func(QualityRPC) bool, detail string) QualityTerm {
		return QualityTerm{weight, phase, label, func(r QualityRPC) int {
			if on(r) {
				return 1
			}
			return 0
		}, func(QualityRPC) string { return detail }}
	}
	listed := func(weight int, phase, label string, of func(QualityRPC) []string, clip int, noun string) QualityTerm {
		return QualityTerm{weight, phase, label, func(r QualityRPC) int { return len(of(r)) }, func(r QualityRPC) string {
			names := of(r)
			if clip > 0 {
				names = clipList(names, clip)
			}
			return fmt.Sprintf("%d %s: %s", len(of(r)), noun, strings.Join(names, ", "))
		}}
	}
	return []QualityTerm{
		flagged(WeightNoContract, PhaseHappy, "rpc in the catalog that no overlay covers, on top of what an empty entry for it scores",
			func(r QualityRPC) bool { return r.NoContract },
			"no contract in any overlay: scored as an empty entry plus this charge — shrt contract init <domain> writes one"),
		listed(WeightUndocumentedField, PhaseHappy, "undocumented request field",
			func(r QualityRPC) []string { return r.UndocumentedFields }, 0, "undocumented field(s)"),
		listed(WeightUnwiredID, PhaseHappy, "required-or-unexplained id field with no from/same_as/value",
			func(r QualityRPC) []string { return r.UnwiredIDs }, 0, "id(s) with no from/same_as/value"),
		flagged(WeightNoFailuresDeclared, PhaseFailure, "write rpc declaring no failures at all",
			func(r QualityRPC) bool { return r.NoFailuresDeclared }, "no failures declared at all"),
		flagged(WeightMissingSummary, PhaseHappy, "missing summary",
			func(r QualityRPC) bool { return !r.HasSummary }, "no summary"),
		flagged(WeightMissingRequiresRole, PhaseHappy, "rpc with no requires_role at all — the literal NONE declares no role gate",
			func(r QualityRPC) bool { return r.MissingRequiresRole },
			"no requires_role: declare the roles, or the literal NONE if the rpc reaches no role gate"),
		flagged(WeightReadWithNoProducer, PhaseHappy, "read rpc no write rpc can reach, with no no_producer saying why",
			func(r QualityRPC) bool { return r.ReadWithNoProducer },
			"read rpc with no producer: no needs/from/same_as edge to any write rpc, and no no_producer: saying why the rows are already there"),
		flagged(WeightEmptyRequired, PhaseHappy, "rpc with request fields and an empty required — the literal NONE declares that the server rejects nothing",
			func(r QualityRPC) bool { return r.EmptyRequired },
			"empty required: list the fields the server rejects without, or the literal NONE if it rejects nothing — a chain built from an empty required lints clean while sending zero values"),
		listed(WeightUncheckedID, PhaseFailure, "wired id with no checked_by",
			func(r QualityRPC) []string { return r.UncheckedIDs }, 0, "wired id(s) with no checked_by"),
		listed(WeightUndeclaredResponseField, PhaseHappy, "response field in no exports/terminal/soft_signals",
			func(r QualityRPC) []string { return r.UndeclaredResponseFields }, 6, "response field(s) in no exports/terminal/soft_signals"),
		listed(WeightUnexplainedFailure, PhaseFailure, "failure declared with no when/unreachable/pending_deploy",
			func(r QualityRPC) []string { return r.UnexplainedFailures }, 6, "failure(s) with no when/unreachable"),
	}
}

func ScoreOfPhase(r QualityRPC, phase string) int {
	total := 0
	for _, t := range QualityTerms() {
		if t.InPhase(phase) {
			total += t.Count(r) * t.Weight
		}
	}
	return total
}

type QualityRPC struct {
	Domain                   string   `json:"domain"`
	RPC                      string   `json:"rpc"`
	UndocumentedFields       []string `json:"undocumented_fields"`
	UnexplainedFailures      []string `json:"unexplained_failures"`
	UnfilledTodos            []string `json:"unfilled_todos"`
	UnwiredIDs               []string `json:"unwired_ids"`
	UncheckedIDs             []string `json:"unchecked_ids"`
	UndeclaredResponseFields []string `json:"undeclared_response_fields"`
	NoFailuresDeclared       bool     `json:"no_failures_declared"`
	ReadWithNoProducer       bool     `json:"read_with_no_producer"`
	MissingRequiresRole      bool     `json:"missing_requires_role"`
	EmptyRequired            bool     `json:"empty_required"`
	NoContract               bool     `json:"no_contract"`
	WiredFields              int      `json:"wired_fields"`
	HasSummary               bool     `json:"has_summary"`
	Score                    int      `json:"score"`
}

type QualityReport struct {
	RPCs       []QualityRPC `json:"rpcs"`
	TotalScore int          `json:"total_score"`
	Phase      string       `json:"phase,omitempty"`
}

type MethodShape struct {
	RequestFields  []string `json:"request_fields"`
	ResponseFields []string `json:"response_fields"`
	Streaming      bool     `json:"streaming,omitempty"`
}

func (r QualityReport) ScoreByDomain() map[string]int {
	out := map[string]int{}
	for _, row := range r.RPCs {
		out[row.Domain] += row.Score
	}
	return out
}

func (r QualityReport) GapsByDomain() map[string]int {
	out := map[string]int{}
	for _, row := range r.RPCs {
		out[row.Domain]++
	}
	return out
}

func MethodShapes(cat *catalog.Catalog) map[string]MethodShape {
	out := map[string]MethodShape{}
	if cat == nil {
		return out
	}
	for _, m := range cat.Methods() {
		shape := MethodShape{RequestFields: []string{}, ResponseFields: []string{}, Streaming: m.Streaming()}
		for _, f := range catalog.DescribeMessage(m.Input()).Fields {
			shape.RequestFields = append(shape.RequestFields, f.Name)
		}
		for _, f := range catalog.DescribeMessage(m.Output()).Fields {
			if f.Name != chain.EnvelopeField() {
				shape.ResponseFields = append(shape.ResponseFields, f.Name)
			}
		}
		out[m.FullName] = shape
	}
	return out
}

func MeasurePhase(lib *Library, cat *catalog.Catalog, domain, phase string) QualityReport {
	shapes := MethodShapes(cat)
	report := QualityReport{RPCs: []QualityRPC{}, Phase: phase}
	add := func(row QualityRPC) {
		row.Score = ScoreOfPhase(row, phase)
		report.TotalScore += row.Score
		if row.Score > 0 {
			report.RPCs = append(report.RPCs, row)
		}
	}
	for _, o := range lib.Overlays {
		if domain != "" && o.Domain != domain {
			continue
		}
		for _, rpc := range chain.SortedKeys(o.RPCs) {
			c := o.RPCs[rpc]
			if c == nil || shapes[rpc].Streaming {
				continue
			}
			add(measureRPC(o.Domain, rpc, c, shapes[rpc], lib.RequiredBy(rpc)))
		}
	}
	if cat != nil {
		for _, m := range cat.Methods() {
			if m.Streaming() || (domain != "" && DomainOf(m) != domain) {
				continue
			}
			if _, ok := lib.Get(m.FullName); ok {
				continue
			}
			row := measureRPC(DomainOf(m), m.FullName, &RPCContract{}, shapes[m.FullName], lib.RequiredBy(m.FullName))
			row.NoContract = true
			add(row)
		}
	}
	slices.SortStableFunc(report.RPCs, func(a, b QualityRPC) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), strings.Compare(a.Domain, b.Domain), strings.Compare(a.RPC, b.RPC))
	})
	return report
}

func saysAnything(f *FieldContract) bool {
	if f == nil {
		return false
	}
	for _, v := range []string{f.From, f.Value, f.SameAs, f.OneOf, f.CheckedBy} {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return Explains(f.Note)
}

func requiredSaysSomething(c *RPCContract, shape MethodShape) bool {
	if len(c.Required) == 1 && strings.TrimSpace(c.Required[0]) == RequiredNone {
		return true
	}
	return slices.ContainsFunc(c.Required, func(key string) bool { return slices.Contains(shape.RequestFields, headSegment(key)) })
}

func measureRPC(domain, rpc string, c *RPCContract, shape MethodShape, requiredBy []string) QualityRPC {
	documented := map[string]bool{}
	for key, f := range c.Fields {
		if saysAnything(f) {
			documented[headSegment(key)] = true
		}
	}
	for _, key := range c.Required {
		if !IsRequiredLiteral(key) {
			documented[headSegment(key)] = true
		}
	}

	undocumented := slices.DeleteFunc(append([]string{}, shape.RequestFields...), func(name string) bool { return documented[name] })

	declaredResponse := map[string]bool{}
	for _, section := range []map[string]string{c.Exports, c.Terminal, c.SoftSignals} {
		for key, why := range section {
			if !IsTodo(why) {
				declaredResponse[headSegment(key)] = true
			}
		}
	}
	undeclaredResponse := slices.DeleteFunc(append([]string{}, shape.ResponseFields...), func(name string) bool { return declaredResponse[name] })

	unexplained := []string{}
	for _, f := range c.Failures {
		if f.When == "" && f.Unreachable == "" && f.PendingDeploy == "" {
			unexplained = append(unexplained, unexplainedLabel(f))
		}
	}

	fields := effectiveFields(c)
	wired := 0
	for _, f := range fields {
		if hasValueSource(f) {
			wired++
		}
	}

	writePath := !chain.IsReadOnlyCall(rpc)
	unwired, unchecked := measureIDKeys(c, fields, writePath)
	row := QualityRPC{
		Domain:                   domain,
		RPC:                      rpc,
		UndocumentedFields:       undocumented,
		UnexplainedFailures:      unexplained,
		UnfilledTodos:            chain.SortedKeys(c.Unfilled),
		UnwiredIDs:               unwired,
		UncheckedIDs:             unchecked,
		UndeclaredResponseFields: undeclaredResponse,
		NoFailuresDeclared:       writePath && len(c.Failures) == 0,
		ReadWithNoProducer:       !writePath && !hasWriteProducer(c, fields, requiredBy) && !Explains(c.NoProducer),
		MissingRequiresRole:      len(c.RequiresRole) == 0,
		EmptyRequired:            len(shape.RequestFields) > 0 && !requiredSaysSomething(c, shape),
		WiredFields:              wired,
		HasSummary:               Explains(c.Summary),
	}
	row.Score = ScoreOfPhase(row, PhaseAll)
	return row
}

func hasWriteProducer(c *RPCContract, fields map[string]*FieldContract, requiredBy []string) bool {
	isWrite := func(node string) bool {
		rpc, _ := SplitNode(node)
		return rpc != "" && !chain.IsReadOnlyCall(rpc)
	}
	if slices.ContainsFunc(c.Needs, isWrite) || slices.ContainsFunc(requiredBy, isWrite) {
		return true
	}
	for _, f := range fields {
		for _, raw := range []string{f.From, f.SameAs} {
			if ref, err := ParseRef(raw); err == nil && isWrite(ref.RPC) {
				return true
			}
		}
	}
	return false
}

var placeholderText = map[string]bool{
	"x": true, "xx": true, "xxx": true, "y": true, "z": true, "n/a": true, "na": true,
	"tbd": true, "tba": true, "fixme": true, "todo": true, "?": true, "??": true, "-": true,
	"none": true, "unknown": true, "ditto": true, "same": true, "see above": true,
}

const minExplanationWords = 3

func Explains(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || IsTodo(trimmed) {
		return false
	}
	if placeholderText[strings.ToLower(strings.Trim(trimmed, ".!"))] {
		return false
	}
	return len(strings.Fields(trimmed)) >= minExplanationWords
}

func measureIDKeys(c *RPCContract, fields map[string]*FieldContract, writePath bool) (unwired, unchecked []string) {
	unwired, unchecked = []string{}, []string{}
	keys := map[string]bool{}
	for key := range fields {
		if IsEntityIDField(key) {
			keys[key] = true
		}
	}
	for _, key := range c.Required {
		if IsEntityIDField(key) {
			keys[key] = true
		}
	}
	for _, key := range chain.SortedKeys(keys) {
		sourced, checked, noted := false, false, false
		for name, f := range fields {
			if relatedKey(name, key) {
				sourced = sourced || hasValueSource(f)
				checked = checked || hasValueSource(f) && f.CheckedBy != "" && !IsTodo(f.CheckedBy)
				noted = noted || strings.TrimSpace(f.Note) != "" && !IsTodo(f.Note)
			}
		}
		if sourced {
			if !checked {
				unchecked = append(unchecked, key)
			}
			continue
		}
		if writePath && (slices.ContainsFunc(c.Required, func(r string) bool { return relatedKey(r, key) }) || !noted) {
			unwired = append(unwired, key)
		}
	}
	return unwired, unchecked
}

func effectiveFields(c *RPCContract) map[string]*FieldContract {
	out := map[string]*FieldContract{}
	for name, f := range c.Fields {
		if f != nil {
			out[name] = f
		}
	}
	for _, alias := range c.Aliases {
		if alias == nil {
			continue
		}
		for name, f := range alias.Fields {
			if f != nil && !hasValueSource(out[name]) {
				out[name] = f
			}
		}
	}
	return out
}

func hasValueSource(f *FieldContract) bool {
	return f != nil && (f.From != "" || f.SameAs != "" || f.Value != "")
}

func relatedKey(a, b string) bool {
	if a == b || strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".") {
		return true
	}
	x, y := chain.SplitPath(a), chain.SplitPath(b)
	for len(x) > 0 && len(y) > 0 {
		switch {
		case x[0] == y[0]:
			x, y = x[1:], y[1:]
		case chain.IsDigits(x[0]) && !chain.IsDigits(y[0]):
			x = x[1:]
		case chain.IsDigits(y[0]) && !chain.IsDigits(x[0]):
			y = y[1:]
		default:
			return false
		}
	}
	return true
}

func unexplainedLabel(f Failure) string {
	switch {
	case f.Reason != "":
		return f.Reason
	case f.Code != 0:
		return strconv.Itoa(f.Code)
	default:
		return "(unnamed)"
	}
}

func headSegment(key string) string {
	head, _, _ := strings.Cut(key, ".")
	return head
}

func clipList(items []string, max int) []string {
	if len(items) <= max {
		return items
	}
	out := append([]string{}, items[:max]...)
	return append(out, fmt.Sprintf("… %d more", len(items)-max))
}

func GapReasons(r QualityRPC, phase string) []string {
	out := []string{}
	for _, t := range QualityTerms() {
		if !t.InPhase(phase) || t.Count(r) == 0 || t.Detail == nil {
			continue
		}
		out = append(out, t.Detail(r))
	}
	return out
}
