package chain

import (
	"fmt"
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
	if i := strings.LastIndex(active.path, "."); i >= 0 {
		return active.path[i+1:]
	}
	return active.path
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

func SetReadOnlyPrefixes(prefixes []string) {
	conventionsMu.Lock()
	defer conventionsMu.Unlock()
	setReadOnlyPrefixesLocked(prefixes)
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
		if code == EnvelopeOK() {
			explicitOK = true
		}
		if code != "" && code != EnvelopeOK() {
			line := fmt.Sprintf("%s.%d", listPath, i)
			out = append(out, ItemRefusal{Path: line + "." + field, Code: code, Line: line})
		}
	}
	if explicitOK {
		for _, i := range unset {
			line := fmt.Sprintf("%s.%d", listPath, i)
			out = append(out, ItemRefusal{Path: line + "." + field, Code: NoItemVerdict, Line: line})
		}
		sort.SliceStable(out, func(a, b int) bool { return itemIndex(out[a].Line) < itemIndex(out[b].Line) })
	}
	if len(items) > 0 && accounted == 0 {
		return nil, fmt.Errorf("conventions.item_envelope_path expects each %s[] to carry %q, and none of "+
			"the %d item(s) declares it at all. The per-item verdict is NOT being checked: a batch refusing "+
			"every line would report success. Check the path against the descriptor",
			listPath, field, len(items))
	}
	return out, nil
}

const NoItemVerdict = "(no verdict)"

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
		if isIndex(s) {
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
	if e.Equals != nil || e.Contains != "" {
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
	codes := CodeFields()
	for _, e := range expect {
		if !e.PinsValue() {
			continue
		}
		segs := SplitPath(e.Path)
		if len(segs) == 0 || !strings.HasPrefix(strings.Join(segs, "."), line+".") {
			continue
		}
		for _, name := range codes {
			if segs[len(segs)-1] == name {
				return true
			}
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
