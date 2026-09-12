package contract

import (
	"fmt"
	"sort"
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

func ValidPhase(phase string) bool {
	switch phase {
	case "", PhaseAll, PhaseHappy, PhaseFailure:
		return true
	}
	return false
}

func QualityTerms() []QualityTerm {
	flag := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	return []QualityTerm{
		{WeightUndocumentedField, PhaseHappy, "undocumented request field",
			func(r QualityRPC) int { return len(r.UndocumentedFields) },
			func(r QualityRPC) string {
				return fmt.Sprintf("%d undocumented field(s): %s", len(r.UndocumentedFields), strings.Join(r.UndocumentedFields, ", "))
			}},
		{WeightUnwiredID, PhaseHappy, "required-or-unexplained id field with no from/same_as/value",
			func(r QualityRPC) int { return len(r.UnwiredIDs) },
			func(r QualityRPC) string {
				return fmt.Sprintf("%d id(s) with no from/same_as/value: %s", len(r.UnwiredIDs), strings.Join(r.UnwiredIDs, ", "))
			}},
		{WeightNoFailuresDeclared, PhaseFailure, "write rpc declaring no failures at all",
			func(r QualityRPC) int { return flag(r.NoFailuresDeclared) },
			func(QualityRPC) string { return "no failures declared at all" }},
		{WeightMissingSummary, PhaseHappy, "missing summary",
			func(r QualityRPC) int { return flag(!r.HasSummary) },
			func(QualityRPC) string { return "no summary" }},
		{WeightMissingRequiresRole, PhaseHappy, "rpc with no requires_role at all — the literal NONE declares no role gate",
			func(r QualityRPC) int { return flag(r.MissingRequiresRole) },
			func(QualityRPC) string {
				return "no requires_role: declare the roles, or the literal NONE if the rpc reaches no role gate"
			}},
		{WeightReadWithNoProducer, PhaseHappy, "read rpc no write rpc can reach, with no no_producer saying why",
			func(r QualityRPC) int { return flag(r.ReadWithNoProducer) },
			func(QualityRPC) string {
				return "read rpc with no producer: no needs/from/same_as edge to any write rpc, and no no_producer: saying why the rows are already there"
			}},
		{WeightEmptyRequired, PhaseHappy, "rpc with request fields and an empty required — the literal NONE declares that the server rejects nothing",
			func(r QualityRPC) int { return flag(r.EmptyRequired) },
			func(QualityRPC) string {
				return "empty required: list the fields the server rejects without, or the literal NONE if it rejects nothing — a chain built from an empty required lints clean while sending zero values"
			}},
		{WeightUncheckedID, PhaseFailure, "wired id with no checked_by",
			func(r QualityRPC) int { return len(r.UncheckedIDs) },
			func(r QualityRPC) string {
				return fmt.Sprintf("%d wired id(s) with no checked_by: %s", len(r.UncheckedIDs), strings.Join(r.UncheckedIDs, ", "))
			}},
		{WeightUndeclaredResponseField, PhaseHappy, "response field in no exports/terminal/soft_signals",
			func(r QualityRPC) int { return len(r.UndeclaredResponseFields) },
			func(r QualityRPC) string {
				return fmt.Sprintf("%d response field(s) in no exports/terminal/soft_signals: %s",
					len(r.UndeclaredResponseFields), strings.Join(clipList(r.UndeclaredResponseFields, 6), ", "))
			}},
		{WeightUnexplainedFailure, PhaseFailure, "failure declared with no when/unreachable/pending_deploy",
			func(r QualityRPC) int { return len(r.UnexplainedFailures) },
			func(r QualityRPC) string {
				return fmt.Sprintf("%d failure(s) with no when/unreachable: %s",
					len(r.UnexplainedFailures), strings.Join(clipList(r.UnexplainedFailures, 6), ", "))
			}},
	}
}

func ScoreOf(r QualityRPC) int { return ScoreOfPhase(r, PhaseAll) }

func ScoreOfPhase(r QualityRPC, phase string) int {
	total := 0
	for _, t := range QualityTerms() {
		if !t.InPhase(phase) {
			continue
		}
		total += t.Count(r) * t.Weight
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
		shape := MethodShape{RequestFields: []string{}, ResponseFields: []string{}}
		for _, f := range catalog.DescribeMessage(m.Input()).Fields {
			shape.RequestFields = append(shape.RequestFields, f.Name)
		}
		for _, f := range catalog.DescribeMessage(m.Output()).Fields {
			if referenceableResponseField(f) {
				shape.ResponseFields = append(shape.ResponseFields, f.Name)
			}
		}
		out[m.FullName] = shape
	}
	return out
}

func referenceableResponseField(f *catalog.Field) bool {
	if f.Name == chain.EnvelopeField() {
		return false
	}
	if f.Kind == "message" || f.Kind == "group" {
		return f.Repeated
	}
	return true
}

func Measure(lib *Library, cat *catalog.Catalog, domain string) QualityReport {
	return MeasurePhase(lib, cat, domain, PhaseAll)
}

func MeasurePhase(lib *Library, cat *catalog.Catalog, domain, phase string) QualityReport {
	shapes := MethodShapes(cat)
	report := QualityReport{RPCs: []QualityRPC{}, Phase: phase}
	for _, o := range lib.Overlays {
		if domain != "" && o.Domain != domain {
			continue
		}
		for _, rpc := range sortedContractNames(o.RPCs) {
			c := o.RPCs[rpc]
			if c == nil {
				continue
			}
			row := measureRPC(o.Domain, rpc, c, shapes[rpc], lib.RequiredBy(rpc))
			row.Score = ScoreOfPhase(row, phase)
			report.TotalScore += row.Score
			if row.Score == 0 {
				continue
			}
			report.RPCs = append(report.RPCs, row)
		}
	}
	sort.SliceStable(report.RPCs, func(i, j int) bool {
		a, b := report.RPCs[i], report.RPCs[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Domain != b.Domain {
			return a.Domain < b.Domain
		}
		return a.RPC < b.RPC
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
	if c.DeclaresNothingRequired() {
		return true
	}
	real := map[string]bool{}
	for _, name := range shape.RequestFields {
		real[name] = true
	}
	for _, key := range c.Required {
		if real[headSegment(key)] {
			return true
		}
	}
	return false
}

func measureRPC(domain, rpc string, c *RPCContract, shape MethodShape, requiredBy []string) QualityRPC {
	documented := map[string]bool{}
	for key, f := range c.Fields {
		if !saysAnything(f) {
			continue
		}
		documented[headSegment(key)] = true
	}
	for _, key := range c.Required {
		if IsRequiredLiteral(key) {
			continue
		}
		documented[headSegment(key)] = true
	}

	undocumented := []string{}
	for _, name := range shape.RequestFields {
		if !documented[name] {
			undocumented = append(undocumented, name)
		}
	}

	declaredResponse := map[string]bool{}
	for key := range c.Exports {
		declaredResponse[headSegment(key)] = true
	}
	for key := range c.Terminal {
		declaredResponse[headSegment(key)] = true
	}
	for key := range c.SoftSignals {
		declaredResponse[headSegment(key)] = true
	}
	undeclaredResponse := []string{}
	for _, name := range shape.ResponseFields {
		if !declaredResponse[name] {
			undeclaredResponse = append(undeclaredResponse, name)
		}
	}

	unexplained := []string{}
	for _, f := range c.Failures {
		if f.When != "" || f.Unreachable != "" || f.PendingDeploy != "" {
			continue
		}
		unexplained = append(unexplained, unexplainedLabel(f))
	}

	fields := effectiveFields(c)
	wired := 0
	for _, f := range fields {
		if hasValueSource(f) {
			wired++
		}
	}

	writePath := !isReadOnly(lastSegment(rpc))
	unwired, unchecked := measureIDKeys(c, fields, writePath)

	noFailures := writePath && len(c.Failures) == 0

	noProducer := !writePath && !hasWriteProducer(c, fields, requiredBy) && !noteExplains(c.NoProducer)

	missingRole := len(c.RequiresRole) == 0

	emptyRequired := len(shape.RequestFields) > 0 && !requiredSaysSomething(c, shape)

	row := QualityRPC{
		Domain:                   domain,
		RPC:                      rpc,
		UndocumentedFields:       undocumented,
		UnexplainedFailures:      unexplained,
		UnfilledTodos:            sortedFlagKeys(c.Unfilled),
		UnwiredIDs:               unwired,
		UncheckedIDs:             unchecked,
		UndeclaredResponseFields: undeclaredResponse,
		NoFailuresDeclared:       noFailures,
		ReadWithNoProducer:       noProducer,
		MissingRequiresRole:      missingRole,
		EmptyRequired:            emptyRequired,
		WiredFields:              wired,
		HasSummary:               Explains(c.Summary),
	}
	row.Score = ScoreOf(row)
	return row
}

func hasWriteProducer(c *RPCContract, fields map[string]*FieldContract, requiredBy []string) bool {
	isWrite := func(node string) bool {
		rpc, _ := SplitNode(node)
		rpc = strings.TrimSpace(rpc)
		return rpc != "" && !isReadOnly(lastSegment(rpc))
	}
	for _, n := range c.Needs {
		if isWrite(n) {
			return true
		}
	}
	for _, f := range fields {
		if f == nil {
			continue
		}
		for _, raw := range []string{f.From, f.SameAs} {
			ref, err := ParseRef(raw)
			if err == nil && isWrite(ref.RPC) {
				return true
			}
		}
	}
	for _, n := range requiredBy {
		if isWrite(n) {
			return true
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

func noteExplains(note string) bool {
	return Explains(note)
}

func measureIDKeys(c *RPCContract, fields map[string]*FieldContract, writePath bool) (unwired, unchecked []string) {
	unwired, unchecked = []string{}, []string{}
	keys := map[string]bool{}
	for key := range fields {
		if isEntityIDKey(key) {
			keys[key] = true
		}
	}
	for _, key := range c.Required {
		if isEntityIDKey(key) {
			keys[key] = true
		}
	}
	for _, key := range sortedFlagKeys(keys) {
		if sourced, checked := idKeyState(fields, key); sourced {
			if !checked {
				unchecked = append(unchecked, key)
			}
			continue
		}
		if !writePath {
			continue
		}
		if !requiredCovers(c.Required, key) && explained(fields, key) {
			continue
		}
		unwired = append(unwired, key)
	}
	return unwired, unchecked
}

func idKeyState(fields map[string]*FieldContract, key string) (sourced, checked bool) {
	for name, f := range fields {
		if f == nil || !relatedKey(name, key) {
			continue
		}
		if hasValueSource(f) {
			sourced = true
			if f.CheckedBy != "" && !IsTodo(f.CheckedBy) {
				checked = true
			}
		}
	}
	return sourced, checked
}

func explained(fields map[string]*FieldContract, key string) bool {
	for name, f := range fields {
		if f == nil || !relatedKey(name, key) {
			continue
		}
		if strings.TrimSpace(f.Note) != "" && !IsTodo(f.Note) {
			return true
		}
	}
	return false
}

func requiredCovers(required []string, key string) bool {
	for _, r := range required {
		if relatedKey(r, key) {
			return true
		}
	}
	return false
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
			if f == nil {
				continue
			}
			if prior, ok := out[name]; ok && hasValueSource(prior) {
				continue
			}
			out[name] = f
		}
	}
	return out
}

func hasValueSource(f *FieldContract) bool {
	return f != nil && (f.From != "" || f.SameAs != "" || f.Value != "")
}

func isEntityIDKey(key string) bool { return IsEntityIDField(key) }

func relatedKey(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".")
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

func sortedFlagKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedContractNames(m map[string]*RPCContract) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
