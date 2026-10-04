package contract

import "testing"

func measureOne(t *testing.T, c *RPCContract, requestFields []string) QualityRPC {
	t.Helper()
	return measureRPC("d", "svc/Rpc", c, MethodShape{RequestFields: requestFields}, nil)
}

func measureNamed(t *testing.T, rpc string, c *RPCContract, requiredBy []string) QualityRPC {
	t.Helper()
	return measureRPC("d", rpc, c, MethodShape{}, requiredBy)
}

func settled(c *RPCContract) *RPCContract {
	if c.Summary == "" {
		c.Summary = "creates the thing and returns its id"
	}
	if len(c.Failures) == 0 {
		c.Failures = []Failure{{Code: 1000, Reason: "Refused", When: "the book is closed"}}
	}
	if len(c.RequiresRole) == 0 {
		c.RequiresRole = []string{"ADMIN"}
	}
	if len(c.Needs) == 0 {
		c.Needs = []string{"svc/CreateThing"}
	}
	if len(c.Required) == 0 {
		c.Required = []string{RequiredNone}
	}
	return c
}

func TestMeasureChargesAnRPCWhoseRequiredIsEmpty(t *testing.T) {
	c := settled(&RPCContract{
		Fields: map[string]*FieldContract{"name": {Value: "x"}},
	})
	c.Required = nil

	got := measureOne(t, c, []string{"name"})
	if !got.EmptyRequired {
		t.Fatal("an rpc with a request field and no required: was not charged — a chain built from it " +
			"lints clean while sending the zero value of every field the server rejects without")
	}

	c.Required = []string{RequiredNone}
	spared := measureOne(t, c, []string{"name"})
	if spared.EmptyRequired {
		t.Fatalf("the literal %s did not spare the charge, so there is no way to say 'nothing is required'", RequiredNone)
	}
	if spared.Score >= got.Score {
		t.Fatalf("declaring %s scored %d, not better than leaving required empty at %d", RequiredNone, spared.Score, got.Score)
	}
}

func TestMeasureEmptyRequired(t *testing.T) {
	named := map[string]*FieldContract{"name": {Value: "x"}}
	for _, tc := range []struct {
		name            string
		fields          map[string]*FieldContract
		required, sends []string
		want            bool
		why             string
	}{
		{"no request fields", nil, nil, nil, false,
			"an rpc whose request has no fields was charged for an empty required: — there is nothing it could list"},
		{"a bogus field name", nil, []string{"totally_bogus_field_name"}, []string{"name"}, true,
			"a required: entry naming a field the request does not have cleared the charge — " +
				"quality would report an answered contract while contract lint reports an error, and an " +
				"author optimising the score alone is rewarded for writing nonsense"},
		{"a real field beside a bogus one", nil, []string{"totally_bogus_field_name", "name"}, []string{"name"}, false,
			"one real field alongside a bogus one should still count as an answered required:"},
		{"UNKNOWN", named, []string{RequiredUnknown}, []string{"name"}, true,
			"UNKNOWN cleared the required charge — saying 'I could not find the handler' must not " +
				"score the same as having found it, or the cheapest path to a clean score is to stop looking"},
		{"NONE", named, []string{RequiredNone}, []string{"name"}, false, "NONE stopped clearing the charge"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := settled(&RPCContract{Fields: tc.fields})
			c.Required = tc.required
			if measureOne(t, c, tc.sends).EmptyRequired != tc.want {
				t.Fatal(tc.why)
			}
		})
	}
}

func TestRequiredNoneIsNotCountedAsADocumentedField(t *testing.T) {
	c := settled(&RPCContract{})
	c.Required = []string{RequiredNone}

	got := measureOne(t, c, []string{"NONE"})
	if len(got.UndocumentedFields) != 1 {
		t.Fatalf("undocumented = %v, want the real field named NONE still reported: the literal in "+
			"required: must not silently document a field that happens to share its name", got.UndocumentedFields)
	}
}

func TestMeasureCountsUndocumentedRequestFields(t *testing.T) {
	c := settled(&RPCContract{
		Fields: map[string]*FieldContract{"business_date": {Value: "2026-01-01"}},
	})
	got := measureOne(t, c, []string{"business_date", "note", "page"})
	if len(got.UndocumentedFields) != 2 {
		t.Fatalf("undocumented = %v, want [note page]", got.UndocumentedFields)
	}
	if got.UndocumentedFields[0] != "note" || got.UndocumentedFields[1] != "page" {
		t.Fatalf("undocumented = %v, want descriptor order [note page]", got.UndocumentedFields)
	}
	if got.Score != 4 {
		t.Fatalf("score = %d, want 4", got.Score)
	}
}

func TestMeasureDocumentsNestedKeyByItsHeadSegment(t *testing.T) {
	c := settled(&RPCContract{
		Required: []string{"lines.qty"},
		Fields:   map[string]*FieldContract{"filter.id_book": {Value: "x"}},
	})
	got := measureOne(t, c, []string{"lines", "filter"})
	if len(got.UndocumentedFields) != 0 {
		t.Fatalf("undocumented = %v, want none", got.UndocumentedFields)
	}
}

func TestMeasureCountsFailuresWithNoExplanation(t *testing.T) {
	c := settled(&RPCContract{
		Failures: []Failure{
			{Code: 1408, Reason: "Explained", When: "the book is closed"},
			{Code: 1409, Reason: "Unreachable", Unreachable: "no path reaches it"},
			{Code: 1410, Reason: "Pending", PendingDeploy: "abc1234"},
			{Code: 1411, Reason: "Bare"},
			{Code: 1412},
			{},
		},
	})
	got := measureOne(t, c, nil)
	want := []string{"Bare", "1412", "(unnamed)"}
	if len(got.UnexplainedFailures) != len(want) {
		t.Fatalf("unexplained = %v, want %v", got.UnexplainedFailures, want)
	}
	for i, w := range want {
		if got.UnexplainedFailures[i] != w {
			t.Fatalf("unexplained[%d] = %q, want %q", i, got.UnexplainedFailures[i], w)
		}
	}
	if got.Score != 3 {
		t.Fatalf("score = %d, want 3", got.Score)
	}
}

func TestMeasureChargesForAMissingSummary(t *testing.T) {
	c := settled(&RPCContract{})
	c.Summary = "   "
	got := measureOne(t, c, nil)
	if got.HasSummary {
		t.Fatal("HasSummary = true, want false")
	}
	if got.Score != WeightMissingSummary {
		t.Fatalf("score = %d, want %d — a missing summary used to be listed at 0", got.Score, WeightMissingSummary)
	}
}

func TestAnUnansweredTodoNoteLeavesTheFieldUndocumented(t *testing.T) {
	c := settled(&RPCContract{
		Fields: map[string]*FieldContract{"name": {Note: TodoMarker + ": where does this come from"}},
	})
	got := measureOne(t, c, []string{"name"})
	if len(got.UndocumentedFields) != 1 {
		t.Fatalf("a field whose only note is an unanswered TODO counted as documented: %v", got.UndocumentedFields)
	}
	if got.Score < WeightUndocumentedField {
		t.Fatalf("score = %d, want at least %d: the TODO count no longer scores, so the terms it used to "+
			"stand in for have to carry it — otherwise an author clears the score by deleting markers",
			got.Score, WeightUndocumentedField)
	}

	c.Fields["name"].Note = "the display name shown to the operator"
	if len(measureOne(t, c, []string{"name"}).UndocumentedFields) != 0 {
		t.Fatal("an answered note did not document the field")
	}
}

func TestTheScoreNoLongerCountsTodoMarkers(t *testing.T) {
	c := settled(&RPCContract{Unfilled: map[string]bool{"required": true, "fields.x.note": true}})
	bare := settled(&RPCContract{})
	if measureOne(t, c, nil).Score != measureOne(t, bare, nil).Score {
		t.Fatal("TODO markers still move the score. Answering one and deleting one discharge it identically, " +
			"so it measures marker removal rather than knowledge — the count belongs in contract status and " +
			"contract lint, which both still report it")
	}
}

func TestMeasureChargesAWriteRPCThatDeclaresNoFailures(t *testing.T) {
	got := measureRPC("d", "svc/CreateThing", &RPCContract{Summary: "creates the thing and returns its id", RequiresRole: []string{"ADMIN"}}, MethodShape{}, nil)
	if !got.NoFailuresDeclared {
		t.Fatal("NoFailuresDeclared = false, want true")
	}
	if got.Score != WeightNoFailuresDeclared {
		t.Fatalf("score = %d, want %d", got.Score, WeightNoFailuresDeclared)
	}
}

func TestMeasureLetsAReadOnlyRPCDeclareNoFailures(t *testing.T) {
	for _, rpc := range []string{"svc/FetchThing", "svc/ListThing", "svc/GetThing", "svc/SearchThing"} {
		got := measureRPC("d", rpc, &RPCContract{
			Summary: "reads the thing and returns it", RequiresRole: []string{"ADMIN"}, Needs: []string{"svc/CreateThing"},
		}, MethodShape{}, nil)
		if got.NoFailuresDeclared || got.Score != 0 {
			t.Fatalf("%s: score = %d, no_failures = %v, want a read path to be exempt",
				rpc, got.Score, got.NoFailuresDeclared)
		}
	}
}

func TestMeasureChargesARequiredIDWithNoEdge(t *testing.T) {
	c := settled(&RPCContract{
		Required: []string{"id_book"},
		Fields:   map[string]*FieldContract{"id_book": {Note: "the book this closes"}},
	})
	got := measureRPC("d", "svc/CloseBookDay", c, MethodShape{}, nil)
	if len(got.UnwiredIDs) != 1 || got.UnwiredIDs[0] != "id_book" {
		t.Fatalf("unwired = %v, want [id_book] — a note is not an edge on a required id", got.UnwiredIDs)
	}
	if got.Score != WeightUnwiredID {
		t.Fatalf("score = %d, want %d", got.Score, WeightUnwiredID)
	}
}

func TestMeasureAcceptsAnyValueSourceOnARequiredID(t *testing.T) {
	for name, f := range map[string]*FieldContract{
		"from":    {From: "svc/Other->id_book", CheckedBy: CheckedByFK},
		"same_as": {SameAs: "svc/Other->id_book", CheckedBy: CheckedByFK},
		"value":   {Value: "${uuid}", CheckedBy: CheckedByNone},
	} {
		c := settled(&RPCContract{Required: []string{"id_book"}, Fields: map[string]*FieldContract{"id_book": f}})
		got := measureRPC("d", "svc/CloseBookDay", c, MethodShape{}, nil)
		if got.Score != 0 {
			t.Fatalf("%s: score = %d, want 0 — %v", name, got.Score, qualityDetail(got))
		}
	}
}

func TestMeasureExemptsAPreviewRPCFromTheNoFailuresCharge(t *testing.T) {
	read := measureRPC("d", "svc/PreviewFeeVariance", &RPCContract{Summary: "creates the thing and returns its id", RequiresRole: []string{RoleNone}}, MethodShape{}, nil)
	if read.NoFailuresDeclared {
		t.Error("a Preview rpc is a read and must not be charged for declaring no failures")
	}
	write := measureRPC("d", "svc/CreateFeeVariance", &RPCContract{Summary: "creates the thing and returns its id", RequiresRole: []string{RoleNone}}, MethodShape{}, nil)
	if !write.NoFailuresDeclared {
		t.Error("a write rpc with no failures at all must still be charged")
	}
}

func TestMeasureAcceptsAnAliasWiringAnID(t *testing.T) {
	c := settled(&RPCContract{
		Required: []string{"id_instrument"},
		Fields:   map[string]*FieldContract{"id_instrument": {Note: "the source channel"}},
		Aliases: map[string]*AliasContract{
			"source": {Fields: map[string]*FieldContract{
				"id_instrument": {From: "svc/CreateInstrument@source->id_instrument", CheckedBy: CheckedByAppLookup},
			}},
		},
	})
	got := measureRPC("d", "svc/PreviewThing", c, MethodShape{}, nil)
	if got.Score != 0 {
		t.Fatalf("score = %d, want 0 — %v", got.Score, qualityDetail(got))
	}
}

func TestMeasureUnwiredIDs(t *testing.T) {
	for _, tc := range []struct {
		name, rpc string
		c         *RPCContract
		want      int
		why       string
	}{
		{"an id wired on an indexed sibling", "svc/ProposeThing", &RPCContract{
			Required: []string{"id_obligation"},
			Fields: map[string]*FieldContract{
				"id_obligation":   {Note: "repeated, 1..100 ids"},
				"id_obligation.0": {From: "svc/CreateDeal->results.0.id_obligation_give", CheckedBy: CheckedByNone},
			},
		}, 0, "want none — element 0 carries the edge"},
		{"an optional id that says why it is unwired", "svc/CreateFeeSchedule", &RPCContract{Fields: map[string]*FieldContract{
			"id_instrument": {Note: "Empty is the wildcard over every channel. Deliberately left unwired."},
		}}, 0, "want none — not required, and the note explains it"},
		{"an optional id whose only note is a todo", "svc/CreateFeeSchedule", &RPCContract{Fields: map[string]*FieldContract{
			"id_instrument": {Note: TodoMarker + ": where this value comes from, or delete the entry"},
		}}, 1, "want [id_instrument] — a " + TodoMarker + " is not an explanation"},
		{"an unwired id on a read path", "svc/FetchDailyBookSummary", &RPCContract{
			Required: []string{"id_book"},
			Fields:   map[string]*FieldContract{"id_book": {Note: "filter, empty means no filter"}},
		}, 0, "want none — a read filter needs no producer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := measureRPC("d", tc.rpc, settled(tc.c), MethodShape{}, nil); len(got.UnwiredIDs) != tc.want {
				t.Fatalf("unwired = %v, %s", got.UnwiredIDs, tc.why)
			}
		})
	}
}

func TestMeasureChargesAWiredIDWithNoCheckedBy(t *testing.T) {
	c := settled(&RPCContract{
		Fields: map[string]*FieldContract{
			"id_book":  {From: "svc/CreateBook->id_book"},
			"id_asset": {From: "svc/CreateAsset->id_asset", CheckedBy: TodoMarker + ": fk, app_lookup or none"},
			"qty":      {From: "svc/Other->qty"},
		},
	})
	got := measureRPC("d", "svc/FetchThing", c, MethodShape{}, nil)
	if len(got.UncheckedIDs) != 2 {
		t.Fatalf("unchecked = %v, want [id_asset id_book] — qty is not an id", got.UncheckedIDs)
	}
	if got.Score != 2*WeightUncheckedID {
		t.Fatalf("score = %d, want %d", got.Score, 2*WeightUncheckedID)
	}
}

func TestMeasureChargesResponseFieldsNoSectionClaims(t *testing.T) {
	c := settled(&RPCContract{
		Exports:     map[string]string{"rows.0.id_deal": "feed it to the next step"},
		Terminal:    map[string]string{"captured_at": "nothing consumes it"},
		SoftSignals: map[string]string{"truncated": "the page was cut"},
	})
	got := measureRPC("d", "svc/FetchThing", c, MethodShape{
		ResponseFields: []string{"rows", "captured_at", "truncated", "total", "cursor"},
	}, nil)
	want := []string{"total", "cursor"}
	if len(got.UndeclaredResponseFields) != len(want) {
		t.Fatalf("undeclared = %v, want %v", got.UndeclaredResponseFields, want)
	}
	for i, w := range want {
		if got.UndeclaredResponseFields[i] != w {
			t.Fatalf("undeclared[%d] = %q, want %q", i, got.UndeclaredResponseFields[i], w)
		}
	}
	if got.Score != 2*WeightUndeclaredResponseField {
		t.Fatalf("score = %d, want %d", got.Score, 2*WeightUndeclaredResponseField)
	}
}

func TestMeasureRanksAnEmptyScaffoldFarBelowACuratedContract(t *testing.T) {
	shape := MethodShape{
		RequestFields:  []string{"id_book", "business_date", "reason"},
		ResponseFields: []string{"already_closed"},
	}
	scaffold := &RPCContract{
		Summary: TodoMarker + ": what this rpc does and when to call it",
		Fields: map[string]*FieldContract{
			"id_book":       {Note: TodoMarker + ": where this value comes from, or delete the entry"},
			"business_date": {Note: TodoMarker + ": where this value comes from, or delete the entry"},
			"reason":        {Note: TodoMarker + ": where this value comes from, or delete the entry"},
		},
		Exports: map[string]string{"already_closed": TodoMarker + ": why a later step would need this"},
		Unfilled: map[string]bool{
			"required": true, "fields.id_book.note": true, "fields.business_date.note": true,
			"fields.reason.note": true, "exports.already_closed": true,
		},
	}
	curated := &RPCContract{
		Summary:      "Close the book for one business date.",
		RequiresRole: []string{"MANAGER"},
		Required:     []string{"id_book", "business_date"},
		Fields: map[string]*FieldContract{
			"id_book":       {From: "svc/CreateBook->id_book", CheckedBy: CheckedByFK},
			"business_date": {Value: "2026-01-01"},
			"reason":        {Note: "free text, echoed back on the audit row"},
		},
		Exports:  map[string]string{"already_closed": "the idempotent replay marker"},
		Failures: []Failure{{Code: 1301, Reason: "BookDayAlreadyClosed", When: "the day is already closed"}},
		Needs:    []string{"svc/CreateBook"},
	}
	bad := measureRPC("pricing", "svc/CloseBookDay", scaffold, shape, nil)
	good := measureRPC("pricing", "svc/CloseBookDay", curated, shape, nil)
	if good.Score != 0 {
		t.Fatalf("curated score = %d, want 0 — %v", good.Score, qualityDetail(good))
	}
	if bad.Score < 8 {
		t.Fatalf("scaffold score = %d, want it to cost real points — %v", bad.Score, qualityDetail(bad))
	}
}

func TestMeasureCountsWiredFields(t *testing.T) {
	c := settled(&RPCContract{
		Fields: map[string]*FieldContract{
			"a": {From: "svc/Other->id"},
			"b": {SameAs: "svc/Other->id"},
			"c": {Value: "1"},
			"d": {Note: "free text only"},
			"e": nil,
		},
	})
	got := measureOne(t, c, nil)
	if got.WiredFields != 3 {
		t.Fatalf("wired = %d, want 3", got.WiredFields)
	}
}

func TestMeasureKeepsOnlyRPCsWithAGap(t *testing.T) {
	clean := settled(&RPCContract{Required: []string{"id_book"},
		Fields: map[string]*FieldContract{"id_book": {From: "svc/Other->id_book", CheckedBy: CheckedByFK}}})
	gap := settled(&RPCContract{Failures: []Failure{{Reason: "Bare"}}})
	lib := NewLibrary([]*Overlay{{
		Domain: "d",
		RPCs:   map[string]*RPCContract{"svc/Clean": clean, "svc/Gap": gap},
	}})
	lib.Overlays[0].RPCs["svc/Nil"] = nil
	report := MeasurePhase(lib, nil, "", PhaseAll)
	if len(report.RPCs) != 1 || report.RPCs[0].RPC != "svc/Gap" {
		t.Fatalf("rows = %+v, want only svc/Gap", report.RPCs)
	}
	if report.TotalScore != 1 {
		t.Fatalf("total = %d, want 1", report.TotalScore)
	}
	if report.ScoreByDomain()["d"] != 1 || report.GapsByDomain()["d"] != 1 {
		t.Fatalf("per-domain = %v / %v", report.ScoreByDomain(), report.GapsByDomain())
	}
}

func TestMeasureSortsWorstFirst(t *testing.T) {
	lib := NewLibrary([]*Overlay{
		{Domain: "b", RPCs: map[string]*RPCContract{
			"svc/One": settled(&RPCContract{Failures: []Failure{{Reason: "x"}}}),
		}},
		{Domain: "a", RPCs: map[string]*RPCContract{
			"svc/Two":   settled(&RPCContract{Failures: []Failure{{Reason: "x"}}}),
			"svc/Three": settled(&RPCContract{Failures: []Failure{{Reason: "x"}, {Reason: "y"}}}),
		}},
	})
	report := MeasurePhase(lib, nil, "", PhaseAll)
	order := []string{}
	for _, r := range report.RPCs {
		order = append(order, r.Domain+" "+r.RPC)
	}
	want := []string{"a svc/Three", "a svc/Two", "b svc/One"}
	for i, w := range want {
		if i >= len(order) || order[i] != w {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestMeasureFiltersByDomain(t *testing.T) {
	lib := NewLibrary([]*Overlay{
		{Domain: "a", RPCs: map[string]*RPCContract{"svc/A": settled(&RPCContract{Failures: []Failure{{Reason: "x"}}})}},
		{Domain: "b", RPCs: map[string]*RPCContract{"svc/B": settled(&RPCContract{Failures: []Failure{{Reason: "x"}}})}},
	})
	report := MeasurePhase(lib, nil, "b", PhaseAll)
	if len(report.RPCs) != 1 || report.RPCs[0].Domain != "b" {
		t.Fatalf("rows = %+v, want only domain b", report.RPCs)
	}
	if report.TotalScore != 1 {
		t.Fatalf("total = %d, want 1", report.TotalScore)
	}
}

func qualityDetail(r QualityRPC) map[string]any {
	return map[string]any{
		"undocumented": r.UndocumentedFields, "unexplained": r.UnexplainedFailures,
		"todos": r.UnfilledTodos, "unwired_ids": r.UnwiredIDs, "unchecked_ids": r.UncheckedIDs,
		"undeclared_response": r.UndeclaredResponseFields, "no_failures": r.NoFailuresDeclared,
		"has_summary": r.HasSummary,
	}
}

func TestMeasureChargesAReadRPCWithNoProducer(t *testing.T) {
	c := &RPCContract{
		Summary:      "Read the marks for one book.",
		RequiresRole: []string{"ACCOUNTING"},
		Fields:       map[string]*FieldContract{"business_date": {Value: "2026-01-01"}},
	}
	got := measureNamed(t, "svc/FetchAssetDailyMark", c, nil)
	if !got.ReadWithNoProducer {
		t.Fatal("ReadWithNoProducer = false on a read whose chain would be one step long")
	}
	if got.Score != WeightReadWithNoProducer {
		t.Fatalf("score = %d, want %d", got.Score, WeightReadWithNoProducer)
	}
}

func TestMeasureReadWithNoProducer(t *testing.T) {
	const creates, reads = "creates the thing and returns its id", "reads the thing and returns it"
	accounting := []string{"ACCOUNTING"}
	read := func(note, noProducer string) *RPCContract {
		return &RPCContract{Summary: reads, RequiresRole: []string{"ADMIN"}, Required: []string{RequiredNone}, Note: note, NoProducer: noProducer}
	}
	for _, tc := range []struct {
		name, rpc  string
		c          *RPCContract
		requiredBy []string
		want       bool
		why        string
	}{
		{"needs", "svc/FetchAssetDailyMark", &RPCContract{Summary: creates, RequiresRole: accounting, Needs: []string{"svc/FreezeAssetDailyMark"}}, nil,
			false, "needs: ReadWithNoProducer = true, want the edge to count as a producer"},
		{"from", "svc/FetchAssetDailyMark", &RPCContract{Summary: creates, RequiresRole: accounting, Fields: map[string]*FieldContract{
			"id_book": {From: "svc/CreateBook->id_book", CheckedBy: CheckedByFK},
		}}, nil, false, "from: ReadWithNoProducer = true, want the edge to count as a producer"},
		{"same_as", "svc/FetchAssetDailyMark", &RPCContract{Summary: creates, RequiresRole: accounting, Fields: map[string]*FieldContract{
			"id_book": {SameAs: "svc/CreateBook->id_book", CheckedBy: CheckedByFK},
		}}, nil, false, "same_as: ReadWithNoProducer = true, want the edge to count as a producer"},
		{"alias from", "svc/FetchAssetDailyMark", &RPCContract{Summary: creates, RequiresRole: accounting, Aliases: map[string]*AliasContract{
			"quote": {Fields: map[string]*FieldContract{"id_asset": {From: "svc/CreateAsset->id_asset", CheckedBy: CheckedByFK}}},
		}}, nil, false, "alias from: ReadWithNoProducer = true, want the edge to count as a producer"},
		{"declared elsewhere", "svc/FetchAssetDailyMark", &RPCContract{Summary: creates, RequiresRole: accounting}, []string{"svc/FreezeAssetDailyMark"},
			false, "a producer that declares before: on this read must count"},
		{"a read as its own producer", "svc/FetchAssetDailyMark", &RPCContract{Summary: reads, RequiresRole: accounting, Needs: []string{"svc/FetchBook"},
			Fields: map[string]*FieldContract{"id_book": {From: "svc/ListBooks->id_book"}}}, []string{"svc/GetBook"},
			true, "a chain of reads produces no state — it must not satisfy the producer check"},
		{"no_producer saying why", "svc/FetchAssetDailyMark", &RPCContract{Summary: reads, RequiresRole: accounting,
			NoProducer: "reads a table seeded by migration 000020; no rpc on this surface writes it"}, nil,
			false, "no_producer saying why must spare the charge — same shape as the optional-id rule"},
		{"a todo in no_producer", "svc/FetchAssetDailyMark", &RPCContract{Summary: creates, RequiresRole: accounting, NoProducer: TodoMarker + ": why?"}, nil,
			true, "a TODO is not an explanation"},
		{"a general-purpose note", "svc/FetchThing", read("reads a table other domains populate", ""), nil,
			true, "a general-purpose note cleared the no-producer charge — that exemption costs one " +
				"sentence and leaves contract plan composing a one-step chain, which is the gap the term exists to find"},
		{"a no_producer migration", "svc/FetchThing", read("", "seeded by migration 000112, no rpc writes these rows"), nil,
			false, "no_producer did not clear the charge, so there is no way to declare a genuinely unproduced read"},
		{"a literal TODO in no_producer", "svc/FetchThing", read("", "TODO: why?"), nil, true, "a TODO in no_producer cleared the charge"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if measureNamed(t, tc.rpc, tc.c, tc.requiredBy).ReadWithNoProducer != tc.want {
				t.Fatal(tc.why)
			}
		})
	}
}

func TestMeasureChargesAnRPCThatDeclaresNoRequiresRole(t *testing.T) {
	c := &RPCContract{Summary: "reads the thing and returns it", Needs: []string{"svc/CreateBook"}}
	got := measureNamed(t, "svc/FetchAssetDailyMark", c, nil)
	if !got.MissingRequiresRole {
		t.Fatal("MissingRequiresRole = false on a contract that says nothing about roles")
	}
	if got.Score != WeightMissingRequiresRole {
		t.Fatalf("score = %d, want %d", got.Score, WeightMissingRequiresRole)
	}
}

func TestMeasureAcceptsTheNoneSentinelAsARoleDeclaration(t *testing.T) {
	c := &RPCContract{Summary: "reads the thing and returns it", Needs: []string{"svc/CreateBook"}, RequiresRole: []string{RoleNone}}
	got := measureNamed(t, "svc/FetchAssetDailyMark", c, nil)
	if got.MissingRequiresRole || got.Score != 0 {
		t.Fatalf("score = %d, want an explicit NONE to settle the question", got.Score)
	}
	if !c.DeclaresNoRole() {
		t.Fatal("DeclaresNoRole = false for requires_role: [NONE]")
	}
	if (&RPCContract{RequiresRole: []string{RoleNone, "ADMIN"}}).DeclaresNoRole() {
		t.Fatal("NONE alongside a real role is not a no-role declaration")
	}
}

func TestAHollowFieldEntryDoesNotCountAsDocumentation(t *testing.T) {
	c := settled(&RPCContract{Fields: map[string]*FieldContract{"name": {}}})
	if len(measureOne(t, c, []string{"name"}).UndocumentedFields) != 1 {
		t.Fatal("an empty fields entry counted as documenting the field — deleting the TODO text and " +
			"leaving the key behind would then score the same as answering it, which is the loophole " +
			"that lets an author descend the score without learning anything")
	}

	c = settled(&RPCContract{Fields: map[string]*FieldContract{"name": {Note: "the operator-visible label"}}})
	if len(measureOne(t, c, []string{"name"}).UndocumentedFields) != 0 {
		t.Fatal("an answered note did not document the field")
	}
}

func TestAPlaceholderIsNotAnExplanation(t *testing.T) {
	for _, junk := range []string{"x", "TBD", "FIXME", "XXX", "?", "N/A", "-", "unknown", "see above", ""} {
		if Explains(junk) {
			t.Errorf("%q counted as an explanation — a score an author can reach by typing one "+
				"character measures typing, not knowledge", junk)
		}
	}
	for _, real := range []string{
		"seeded by migration 000020, no rpc writes it",
		"decimal string, must be greater than zero",
	} {
		if !Explains(real) {
			t.Errorf("%q was rejected as an explanation", real)
		}
	}
}

func TestASummaryOfOneCharacterIsNotASummary(t *testing.T) {
	c := settled(&RPCContract{})
	c.Summary = "x"
	if measureOne(t, c, nil).HasSummary {
		t.Fatal("a one-character summary satisfied the summary term")
	}
	c.Summary = "creates an invoice and its first line"
	if !measureOne(t, c, nil).HasSummary {
		t.Fatal("a real summary was rejected")
	}
}
