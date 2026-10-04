package chain

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/namecase"
)

const (
	DefaultEnvelopeField = "error"
	DefaultEnvelopePath  = "error.code"
	DefaultEnvelopeOK    = "OK"
)

type conventions struct {
	field    string
	path     string
	ok       string
	itemPath string
	readOnly []string
	codes    []string
}

var (
	conventionsMu sync.RWMutex
	active        = defaultConventions()
)

func defaultConventions() conventions {
	return conventions{
		field:    DefaultEnvelopeField,
		path:     DefaultEnvelopePath,
		ok:       DefaultEnvelopeOK,
		readOnly: DefaultReadOnlyPrefixes(),
		codes:    DefaultCodeFields(),
	}
}

func DefaultCodeFields() []string {
	return []string{"app_code", "reason", "error_code"}
}

func CodeFields() []string {
	conventionsMu.RLock()
	defer conventionsMu.RUnlock()
	return append([]string(nil), active.codes...)
}

func EnvelopeLeaf() string {
	conventionsMu.RLock()
	defer conventionsMu.RUnlock()
	return active.path[strings.LastIndex(active.path, ".")+1:]
}

func setCodeFieldsLocked(names []string) {
	out := []string{}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		out = DefaultCodeFields()
	}
	active.codes = out
}

func EnvelopeField() string {
	conventionsMu.RLock()
	defer conventionsMu.RUnlock()
	return active.field
}

func EnvelopePath() string {
	conventionsMu.RLock()
	defer conventionsMu.RUnlock()
	return active.path
}

func EnvelopeOK() string {
	conventionsMu.RLock()
	defer conventionsMu.RUnlock()
	return active.ok
}

func ItemEnvelope() string {
	conventionsMu.RLock()
	defer conventionsMu.RUnlock()
	return active.itemPath
}

func ReadOnlyPrefixes() []string {
	conventionsMu.RLock()
	defer conventionsMu.RUnlock()
	return append([]string(nil), active.readOnly...)
}

func SetItemEnvelope(path string) {
	conventionsMu.Lock()
	defer conventionsMu.Unlock()
	active.itemPath = strings.TrimSpace(path)
}

func setReadOnlyPrefixesLocked(prefixes []string) {
	if len(prefixes) == 0 {
		active.readOnly = DefaultReadOnlyPrefixes()
		return
	}
	active.readOnly = append([]string(nil), prefixes...)
}

func setEnvelopeLocked(path, ok string) {
	path = strings.TrimSpace(path)
	ok = strings.TrimSpace(ok)
	if path == "" {
		path = DefaultEnvelopePath
	}
	if ok == "" {
		ok = DefaultEnvelopeOK
	}
	active.path = path
	active.ok = ok
	active.field = path
	if i := strings.Index(path, "."); i > 0 {
		active.field = path[:i]
	}
}

type ItemRefusal struct {
	Path string
	Code string
	Line string
}

func (r ItemRefusal) String() string { return r.Path + " = " + r.Code }

func splitItemEnvelope() (listPath, field string, err error) {
	list, field, ok := strings.Cut(ItemEnvelope(), "[].")
	if !ok {
		return "", "", fmt.Errorf("conventions.item_envelope_path is %q, which is not of the form "+
			"<list>[].<path> — the per-item verdict is not being checked at all, and a batch rpc that "+
			"refuses every line still reports success", ItemEnvelope())
	}
	return strings.TrimSuffix(list, "."), field, nil
}

func ItemRefusals(response any) ([]ItemRefusal, error) {
	if ItemEnvelope() == "" {
		return nil, nil
	}
	listPath, field, err := splitItemEnvelope()
	if err != nil {
		return nil, err
	}
	rows, found := Get(response, listPath)
	if !found {
		return nil, nil
	}
	items, ok := rows.([]any)
	if !ok {
		return nil, fmt.Errorf("conventions.item_envelope_path names %q, which this response carries but "+
			"is not a list — check the path against the descriptor", listPath)
	}
	out := []ItemRefusal{}
	accounted := 0
	explicitOK := false
	unset := []int{}
	empty := []int{}
	for i, item := range items {
		v, found := Get(item, field)
		if !found || v == nil {
			if found || declaresUnsetVerdict(item, field) {
				accounted++
			}
			unset = append(unset, i)
			continue
		}
		accounted++
		code := stringify(v)
		switch code {
		case EnvelopeOK():
			explicitOK = true
		case "":
			empty = append(empty, i)
		default:
			line := fmt.Sprintf("%s.%d", listPath, i)
			out = append(out, ItemRefusal{Path: line + "." + field, Code: code, Line: line})
		}
	}
	missing := []int{}
	if explicitOK || len(out) > 0 {
		missing = append(missing, empty...)
	}
	if explicitOK {
		missing = append(missing, unset...)
	}
	for _, i := range missing {
		line := fmt.Sprintf("%s.%d", listPath, i)
		out = append(out, ItemRefusal{Path: line + "." + field, Code: NoItemVerdict, Line: line})
	}
	sort.SliceStable(out, func(a, b int) bool { return itemIndex(out[a].Line) < itemIndex(out[b].Line) })
	if len(items) > 0 && accounted == 0 {
		return nil, fmt.Errorf("conventions.item_envelope_path expects each %s[] to carry %q, and none of "+
			"the %d item(s) declares it at all. The per-item verdict is NOT being checked: a batch refusing "+
			"every line would report success. Check the path against the descriptor",
			listPath, field, len(items))
	}
	return out, nil
}

const NoItemVerdict = "(no verdict)"

type MisspeltItemVerdict struct {
	Refusal ItemRefusal
	Key     string
	Want    string
}

func MisspeltItemVerdicts(sent any, unknown []string) []MisspeltItemVerdict {
	if ItemEnvelope() == "" {
		return nil
	}
	listPath, field, err := splitItemEnvelope()
	if err != nil {
		return nil
	}
	fieldSegs := SplitPath(field)
	var out []MisspeltItemVerdict
	seen := map[string]bool{}
	for _, u := range unknown {
		rest, ok := strings.CutPrefix(u, listPath+"[].")
		if !ok {
			continue
		}
		segs := strings.Split(rest, ".")
		depth := len(segs) - 1
		if depth >= len(fieldSegs) || !namecase.Equal(segs[depth], fieldSegs[depth]) || segs[depth] == fieldSegs[depth] {
			continue
		}
		if strings.Join(segs[:depth], ".") != strings.Join(fieldSegs[:depth], ".") {
			continue
		}
		rows, _ := Get(sent, listPath)
		items, _ := rows.([]any)
		for i, item := range items {
			parent := item
			if depth > 0 {
				parent, _ = Get(item, strings.Join(fieldSegs[:depth], "."))
			}
			obj, ok := parent.(map[string]any)
			if !ok {
				continue
			}
			if _, exact := obj[fieldSegs[depth]]; exact {
				continue
			}
			if _, variant := obj[segs[depth]]; !variant {
				continue
			}
			line := fmt.Sprintf("%s.%d", listPath, i)
			if seen[line] {
				continue
			}
			seen[line] = true
			out = append(out, MisspeltItemVerdict{
				Refusal: ItemRefusal{Path: line + "." + field, Code: NoItemVerdict, Line: line},
				Key:     strings.Join(append(append([]string{}, segs[:depth]...), segs[depth]), "."),
				Want:    strings.Join(fieldSegs[:depth+1], "."),
			})
		}
	}
	return out
}

func itemIndex(line string) int {
	n, _ := strconv.Atoi(line[strings.LastIndex(line, ".")+1:])
	return n
}

func declaresUnsetVerdict(item any, field string) bool {
	segs := SplitPath(field)
	for i := 1; i <= len(segs); i++ {
		v, found := Get(item, strings.Join(segs[:i], "."))
		if !found {
			return false
		}
		if v == nil {
			return true
		}
	}
	return false
}

func ItemEnvelopeDeclared(fields []*catalog.Field) bool {
	if ItemEnvelope() == "" {
		return false
	}
	listPath, field, err := splitItemEnvelope()
	if err != nil {
		return false
	}
	list, found := catalog.FieldAt(fields, SplitPath(listPath))
	if !found || list == nil || !list.Repeated {
		return false
	}
	return list.Truncated || catalog.HasPath(list.Fields, SplitPath(field))
}

func ValidateItemEnvelope(cat *catalog.Catalog) error {
	return ValidateItemEnvelopeIn(cat, ItemEnvelope())
}

func ValidateItemEnvelopeIn(cat *catalog.Catalog, path string) error {
	if path == "" {
		return nil
	}
	listPath, field, ok := strings.Cut(path, "[].")
	if !ok {
		return fmt.Errorf("conventions.item_envelope_path is %q, which is not of the form "+
			"<list>[].<path> — the per-item verdict is not being checked at all, and a batch rpc that "+
			"refuses every line still reports success", path)
	}
	listPath = strings.TrimSuffix(listPath, ".")
	for _, m := range cat.Methods() {
		fields := catalog.DescribeMessage(m.Output()).Fields
		list, found := catalog.FieldAt(fields, SplitPath(listPath))
		if !found || list == nil || !list.Repeated {
			continue
		}
		if list.Truncated || catalog.HasPath(list.Fields, SplitPath(field)) {
			return nil
		}
	}
	return fmt.Errorf("conventions.item_envelope_path is %q, and no response message in the descriptor "+
		"declares that list with that field — nothing would ever be checked, so a batch rpc refusing "+
		"every line would report success. Fix the path against the descriptor, or remove the key if "+
		"this backend has no per-item verdict", path)
}

func ValidateEnvelopeIn(cat *catalog.Catalog, path string) error {
	path = strings.TrimSpace(path)
	if path == "" || cat == nil || len(cat.Methods()) == 0 {
		return nil
	}
	segs := SplitPath(path)
	for _, m := range cat.Methods() {
		if catalog.HasResponsePath(catalog.DescribeMessage(m.Output()).Fields, segs) {
			return nil
		}
	}
	hint := ""
	if found := catalog.DetectEnvelope(cat); len(found) > 0 {
		hint = fmt.Sprintf(" The response messages carry %q; if that is the verdict, set envelope_path: %s.", found[0].Path, found[0].Path)
	}
	return fmt.Errorf("conventions.envelope_path is %q, and no response message in the descriptor declares "+
		"that field, so nothing was sent: every step asserting the envelope would compare against a path "+
		"that is never present, and an in-band refusal would pass unseen.%s Fix the path against the "+
		"descriptor ('shrt doctor' checks it)", path, hint)
}

func joinDataPath(path string) string {
	segs := SplitPath(path)
	kept := make([]string, 0, len(segs))
	for _, s := range segs {
		if isDigits(s) {
			continue
		}
		kept = append(kept, s)
	}
	return strings.Join(kept, ".")
}

func IsVerdictPath(path string) bool {
	if IsEnvelopePath(path) {
		return true
	}
	if ItemEnvelope() == "" {
		return false
	}
	listPath, field, err := splitItemEnvelope()
	if err != nil {
		return false
	}
	return joinDataPath(path) == joinDataPath(listPath+"."+field)
}

func isScalarValue(v any) bool {
	switch v.(type) {
	case nil, []any, map[string]any:
		return false
	}
	return true
}

func VacuousNotEqual(path string, want any) bool {
	return IsVerdictPath(path) && stringify(want) != EnvelopeOK()
}

func VacuousNotEqualResult(path string, want, got any) bool {
	if VacuousNotEqual(path, want) {
		return true
	}
	return isScalarValue(want) && !isScalarValue(got)
}

func (e Expectation) PinsValue() bool {
	if e.Equals != nil || e.Contains != "" || e.HasComparison() {
		return true
	}
	return e.NotEqual != nil && !VacuousNotEqual(e.Path, e.NotEqual)
}

func DeclaresVerdict(expect []Expectation, path string) bool {
	want := strings.Join(SplitPath(path), ".")
	for _, e := range expect {
		if e.PinsValue() && strings.Join(SplitPath(e.Path), ".") == want {
			return true
		}
	}
	return false
}

func DeclaresRefusal(expect []Expectation, r ItemRefusal) bool {
	if DeclaresVerdict(expect, r.Path) {
		return true
	}
	line := r.Line
	if _, field, err := splitItemEnvelope(); line == "" && err == nil && strings.HasSuffix(r.Path, "."+field) {
		line = strings.TrimSuffix(r.Path, "."+field)
	}
	line = strings.Join(SplitPath(line), ".")
	if line == "" {
		return false
	}
	for _, e := range expect {
		if !namesCode(e) {
			continue
		}
		segs := SplitPath(e.Path)
		if len(segs) == 0 || !strings.HasPrefix(strings.Join(segs, "."), line+".") {
			continue
		}
		if isCodeField(segs) {
			return true
		}
	}
	return false
}

func UndeclaredRefusals(refusals []ItemRefusal, expect []Expectation) []ItemRefusal {
	out := []ItemRefusal{}
	for _, r := range refusals {
		if !DeclaresRefusal(expect, r) {
			out = append(out, r)
		}
	}
	return out
}

func SetEnvelope(path, ok string) {
	conventionsMu.Lock()
	defer conventionsMu.Unlock()
	setEnvelopeLocked(path, ok)
}

func IsEnvelopePath(path string) bool {
	return path == EnvelopeField() || strings.HasPrefix(path, EnvelopeField()+".")
}

func CoversVerdict(path string) bool {
	verdict := SplitPath(EnvelopePath())
	segs := SplitPath(path)
	if len(segs) == 0 || len(segs) > len(verdict) {
		return false
	}
	for i, seg := range segs {
		if !namecase.Equal(seg, verdict[i]) {
			return false
		}
	}
	return true
}

func namesCode(e Expectation) bool {
	return e.Contains != "" || (e.Equals != nil && stringify(e.Equals) != "")
}

func isCodeField(segs []string) bool {
	return len(segs) > 0 && slices.Contains(CodeFields(), segs[len(segs)-1])
}

func PinsVerdictCode(e Expectation) bool {
	if !namesCode(e) {
		return false
	}
	segs := SplitPath(e.Path)
	verdict := SplitPath(EnvelopePath())
	if len(segs) == 0 || len(segs) < len(verdict) {
		return false
	}
	for i, seg := range verdict[:len(verdict)-1] {
		if !namecase.Equal(segs[i], seg) {
			return false
		}
	}
	return isCodeField(segs)
}

func IsVerdictItself(path string) bool {
	return CoversVerdict(path) && len(SplitPath(path)) == len(SplitPath(EnvelopePath()))
}

func IsPagingFieldName(name string) bool {
	page, token := false, false
	for _, w := range namecase.Words(name) {
		switch w {
		case "cursor", "pagination":
			return true
		case "page":
			page = true
		case "token":
			token = true
		}
	}
	return page && token
}

func IsMetadataField(name string) bool {
	return name == EnvelopeField() || IsPagingFieldName(name)
}

func ApplyConventions(readOnlyPrefixes []string, envelopePath, envelopeOK string) {
	conventionsMu.Lock()
	defer conventionsMu.Unlock()
	setReadOnlyPrefixesLocked(readOnlyPrefixes)
	setEnvelopeLocked(envelopePath, envelopeOK)
}

func ApplyItemEnvelope(path string) { SetItemEnvelope(path) }

func ApplyCodeFields(names []string) {
	conventionsMu.Lock()
	defer conventionsMu.Unlock()
	setCodeFieldsLocked(names)
}
