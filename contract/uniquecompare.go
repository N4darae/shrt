package contract

import "regexp"

const (
	UniqueCaseIgnore = "ignore"
	UniqueCaseExact  = "exact"
)

var (
	caseSensitive = regexp.MustCompile(`(?i)case[- ]?sensitiv|respect\w*\s+(?:the\s+)?(?:letter\s+)?case|exact\s+(?:letter\s+)?case|preserv\w*\s+(?:the\s+)?(?:letter\s+)?case`)
	trimClaimed   = regexp.MustCompile(`(?i)\btrim\w*|\bstrip\w*|(?:surrounding|leading\s+(?:and|or)\s+trailing)\s+(?:white\s?space|spaces?)\s+(?:is|are)\s+(?:ignored|removed|dropped)|ignor\w*\s+(?:surrounding|leading\s+(?:and|or)\s+trailing)\s+(?:white\s?space|spaces?)`)
	trimDenied    = regexp.MustCompile(`(?i)\buntrimmed\b|\bunstripped\b`)
	negation      = regexp.MustCompile(`(?i)\b(?:no|not|never|without|nor|neither|doesn't|does\s+not|don't|do\s+not|isn't|is\s+not|aren't|are\s+not|won't|cannot|can't)\b`)
	clauseBreak   = regexp.MustCompile(`[;,.()]|\bbut\b|\bwhile\b`)
)

func claims(text string, claimed, denied *regexp.Regexp) (yes, no bool) {
	if denied != nil && denied.MatchString(text) {
		no = true
	}
	for _, clause := range clauseBreak.Split(text, -1) {
		loc := claimed.FindStringIndex(clause)
		if loc == nil {
			continue
		}
		if negation.MatchString(clause[:loc[0]]) {
			no = true
		} else {
			yes = true
		}
	}
	return yes, no
}

func ignoresCase(f Failure, text string) bool {
	if f.Unique != nil && f.Unique.Case != "" {
		return f.Unique.Case == UniqueCaseIgnore
	}
	yes, no := claims(text, caseIgnored, caseSensitive)
	return yes && !no
}

func comparesCaseExactly(f Failure, text string) bool {
	if f.Unique != nil && f.Unique.Case != "" {
		return f.Unique.Case == UniqueCaseExact
	}
	yes, no := claims(text, caseSensitive, nil)
	return yes && !no
}

func trimsSpace(f Failure, text string) bool {
	if f.Unique != nil && f.Unique.Trim != nil {
		return *f.Unique.Trim
	}
	yes, no := claims(text, trimClaimed, trimDenied)
	return yes && !no
}
