package chain

import "time"

type ReferenceExample struct {
	Ref     string
	Meaning string
}

var ReferenceExamples = []ReferenceExample{
	{"${vars.book_code}", "a chain var"},
	{"${vars.nested.qty}", "a dotted path inside a var"},
	{"${env.API_USER}", "an environment variable; missing is an error, never `\"\"`"},
	{"${create_deal.id_deal}", "a field of an earlier step's RESPONSE"},
	{"${steps.create_deal.response.id_deal}", "the same, written out"},
	{"${steps.create_deal.request.id_book}", "a field of an earlier step's REQUEST"},
	{"${create_deal.deals.0.id_deal}", "a list index"},
	{"${exports.deal_id}", "a value an earlier step exported"},
	{"${deal_id}", "the same export, bare"},
	{"${now}", "RFC3339, pinned here for reproducibility"},
	{"${nowunix}", "unix seconds"},
	{"${nowunix+3600}", "an hour ahead — a bounded-future `expires_at` without a literal that goes stale"},
	{"${now-86400}", "the same offset in RFC3339; the unit is always SECONDS"},
	{"${today}", "the UTC midnight of this run — a business date, already a multiple of 86400"},
	{"${today-86400}", "the business date before it"},
	{"${uuid}", "fresh per reference — idempotency keys"},
	{"deal-${create_deal.id_deal}-x", "interpolated inside a longer string, so the result is text"},
	{"${vars.ref_in_a_var}", "a var whose own value is `${uuid}` — handed back **VERBATIM**, never resolved. `lint` now rejects it"},
}

const ReferenceExampleStep = "create_deal"

func ReferenceExampleScope() *Scope {
	sc := NewScope(map[string]any{
		"book_code":    "BOOK-A",
		"nested":       map[string]any{"qty": float64(7)},
		"ref_in_a_var": "${uuid}",
	})
	sc.Now = func() time.Time { return time.Date(2026, 9, 11, 10, 44, 34, 0, time.UTC) }
	sc.Env = func(k string) (string, bool) {
		if k == "API_USER" {
			return "someone", true
		}
		return "", false
	}
	sc.Record(ReferenceExampleStep,
		map[string]any{"id_book": "b-1", "idempotency_key": "k-1"},
		map[string]any{"id_deal": "d-9", "deals": []any{map[string]any{"id_deal": "d-9"}}})
	sc.Exports["deal_id"] = "d-9"
	return sc
}
