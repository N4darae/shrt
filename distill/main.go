package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

var target, rerun = layout()

func layout() (string, string) {
	if st, err := os.Stat("core_distillation"); err == nil && st.IsDir() {
		return "core_distillation/GRAMMAR.md", "go run ./core_distillation/distill"
	}
	return "GRAMMAR.md", "go run ./distill"
}

var notes = map[string]string{
	"Chain.apiVersion":  "`shrt/v1`. Defaults to it when omitted.",
	"Chain.name":        "Names the run directory and the safe spot. Defaults to the file name; a different name is a lint warning, and two files with one name are a lint error.",
	"Chain.description": "What state this chain reproduces, for the next reader.",
	"Chain.vars":        "Referenced as `${vars.x}`; override per run with `-var x=y`. A var the chain reads without declaring must be given with `-var`, or `run` and `verify` refuse before sending; the exception is `tag`, which gets a fresh value each run, recorded in the run's `vars`.",
	"Chain.volatile":    "Response paths `shrt verify` and `shrt diff` mask in every step. Expectations still see the real value.",
	"Chain.unordered":   "Response lists `shrt verify` compares as a multiset in every step, for an rpc that promises no order. Name the list without indices (`items`, `invoices.lines`).",
	"Chain.redact":      "Paths blanked in the run record (requests, responses, vars, exports, `want`/`got`) and so never compared by `verify`, added to the config's. Redact only what must not be stored.",
	"Chain.steps":       "Ordered; never reordered, parallelised or skipped.",
	"Chain.kept_red":    "Pins a known defect the chain shows on purpose: the step and expectation path that fail, optionally the value got. `shrt run` goes past every failure and exits 0 only while the chain fails exactly as pinned; such a chain is never confirmed.",

	"Step.id":          "Unique; later steps reference it. Derived from the rpc name when omitted.",
	"Step.description": "Why this step is here and what its assertions mean.",
	"Step.call":        "`package.Service/Rpc`, `Service/Rpc`, or a bare `Rpc` when unambiguous. A server-streaming rpc records its first message as `messages.0` (a `WatchInvoice` step asserts `messages.0.status.code`) and stops reading, within 5s; client- and bidi-streaming rpcs are refused.",
	"Step.body":        "The request, validated against the proto request message before anything is sent.",
	"Step.headers":     "Per-step headers. Never the auth header: use `auth: <profile>`; a hand-written `Authorization` is a lint error and `run` refuses it.",
	"Step.expect":      "Assertions on this step's response. Each entry holds exactly one rule and nearly always a `path`.",
	"Step.export":      "`name: <path in the response>`, e.g. `id_invoice: invoice.id_invoice` (no `response.` prefix). Publishes `${exports.name}` and the bare `${name}`. A name equal to a step id is refused by lint and run; one another step also exports is a warning (`export-overwritten`).",
	"Step.auth":        "Auth profile from `.shrt/config.yaml` for this step. `invalid` sends a token the backend never issued and never logs in again: the invalid-token probe. Contradicts `skip_auth`.",
	"Step.skip_auth":   "Attach no auth header: the missing-token probe. A login step does not need it.",
	"Step.allow_fail":  "Let the chain go on past a transport refusal on a step with no expectations. It never waives a failed expectation or an `error` step; with expectations it does nothing (`inert-allow-fail`).",
	"Step.volatile":    "Volatile paths for this step only, added to the chain's.",
	"Step.unordered":   "Unordered lists for this step only, added to the chain's. Each must name a repeated field of this step's response.",
	"Step.wait":        "A Go duration (`25s`, at most `10m`) waited before the step is sent, outside its latency. For behaviour that needs time to pass, such as a session that must outlive an age.",

	"Expectation.path":      "JSON path into this step's response (`invoice.lines.0.amount_minor`), or a reserved `transport.*` path. No `${...}`; a path the response message has no field for is a lint error, and `run` refuses the chain before sending. Field names match case- and separator-insensitively.",
	"Expectation.equals":    "Compared as text, so `1` matches `\"1\"`. May carry `${...}` (an earlier step, or this step's own request). No arithmetic is done.",
	"Expectation.not_equal": "Present AND different; an absent path fails it. May carry `${...}`.",
	"Expectation.contains":  "Substring of the value's text. May carry `${...}`.",
	"Expectation.includes":  "The path holds a list and at least one item matches: a map names item fields compared as `equals`, a scalar is compared with the item. Order and other items do not matter. May carry `${...}`.",
	"Expectation.exists":    "Whether the server SENT the path, read against the populated fields, not the stored record (see the second table below).",
	"Expectation.gt":        "Present and a number greater than this. An int64 stored as text is its number and an RFC3339 time is its unix seconds. May carry `${...}`, e.g. `${nowunix+3600}`.",
	"Expectation.gte":       "As `gt`, greater than or equal.",
	"Expectation.lt":        "As `gt`, less than.",
	"Expectation.lte":       "As `gt`, less than or equal.",
	"Expectation.between":   "Exactly two inclusive bounds `[low, high]`, read as `gt` reads them.",
	"Expectation.within":    "`{of: X, by: N}`: present and at most N from X, read as `gt` reads them. The rule for clock values: `expires_at within: {of: \"${nowunix+3600}\", by: 5}`.",
	"Within.of":             "The value to be near, read as `gt` reads its bound. May carry `${...}`.",
	"Within.by":             "The largest distance allowed, a number.",
	"Expectation.not_empty": "Present and not `\"\"`, `0`, `false`, `[]` or `{}`; an int64 `\"0\"` is zero too.",

	"Overlay.apiVersion":  "`shrt/contract/v1`.",
	"Overlay.domain":      "Defaults to the file name.",
	"Overlay.description": "Domain-wide prose: the invariants and call order every rpc here shares.",
	"Overlay.failures":    "Failures every rpc in the domain inherits (authn, authz), stated once. With `scope: all` every rpc of every domain inherits it.",
	"Overlay.rpcs":        "Keyed by the fully qualified `package.Service/Rpc`.",

	"RPCContract.summary":       "Prose for people: what it does and when you would call it. What a write does to numbers goes in `effects`.",
	"RPCContract.effects":       "What this rpc does to numbers, as data `plan` asserts, keyed by the number's field name: `{balance: {increase: amount}}`. Or a word: `none` (leaves it alone), `zero` (a create starts it at 0), `per_item` (keyed by a repeated request field: each item is applied or refused alone). A stated key wins over the prose.",
	"RPCContract.note":          "Free text about the rpc. It spares no quality term; use `no_producer` for a read with no producer.",
	"RPCContract.auth":          "Auth profile this rpc needs when the default principal is the wrong one; `plan` writes it onto the step.",
	"RPCContract.requires_role": "Roles the caller must hold. `[NONE]` says no role gate; leaving the key out is scored as an omission. With auth configured, `plan` calls a gated rpc as each other profile, expecting the denial.",
	"RPCContract.no_producer":   "Why no write in this API creates the rows this read returns (a seed, a migration, a feed). The only thing that spares the no-producer charge.",
	"RPCContract.required":      "Fields the server rejects without, read from the backend; reads too. `[NONE]`: it rejects nothing. `[UNKNOWN]`: the handler could not be found (a warning, scored as empty).",
	"RPCContract.needs":         "An rpc that must run first but whose output no field consumes, e.g. the write that creates what a list lists. `plan` makes it hold for every entity the step touches; a slice counts an rpc whose effects increase a field this one increases as meeting it. A write that only takes the record to the state this rpc's `restore:` names is a way to reach that state, not a precondition: `plan` also calls this rpc on a record left in the state before it, item-count probes included, unless a failure refuses that state.",
	"RPCContract.before":        "The inverse of `needs`, declared by the prerequisite's own domain. Takes rpc names only and pulls in the unaliased rpc.",
	"RPCContract.fields":        "Per request field. Dotted keys reach nested messages; after a repeated field an index picks one entry (`lines.1.id_account`), and an unindexed key (`lines.qty`) applies to every entry.",
	"RPCContract.aliases":       "Per-instance overrides, so two aliased steps of one rpc differ.",
	"RPCContract.exports":       "Response paths worth exporting, and why.",
	"RPCContract.terminal":      "Response fields that deliberately have no consumer.",
	"RPCContract.soft_signals":  "Response fields carrying advisory information rather than success or failure.",
	"RPCContract.failures":      "One entry per way this rpc refuses.",
	"RPCContract.source":        "Files read to determine all this, without line ranges.",
	"RPCContract.status":        "`draft`, or `verified` with a `verified_run`. An agent leaves it `draft`.",
	"RPCContract.verified_by":   "The person who verified it.",
	"RPCContract.verified_run":  "The run id that proved it.",

	"FieldContract.from":       "`<rpc>[@alias]->response_path`: the value comes from an earlier call's response, which orders the two.",
	"FieldContract.value":      "A fixed literal or template, e.g. `${uuid}` or `inv-${vars.tag}-a`. `value: \"0\"` marks a zero as deliberate.",
	"FieldContract.same_as":    "`<rpc>[@alias]->request_path`: must equal what an earlier call SENT. Exclusive with `from`.",
	"FieldContract.oneof":      "Mutual-exclusion group; at most one member carries a value, and that member is the one scaffolded.",
	"FieldContract.checked_by": "How the server validates the id, which decides whether a bad one is a named failure or an unnamed 500.",
	"FieldContract.note":       "Units, formats, constraints. `plan` reads `unique`, normalisation (`stored lowercased`, `trimmed`) and a stated minimum or maximum from it.",

	"Effect.increase": "The request number it grows by: `amount`, or `lines.amount` for each line. The record moved is the one a `from:`-wired id names whose response carries the key.",
	"Effect.decrease": "As `increase`, shrinking.",
	"Effect.of":       "An id wired `from:` another write: the path is read from that record's request, one move per line, e.g. `{decrease: lines.amount, of: id_invoice}`. Only with `increase` or `decrease`; `restore` takes none.",
	"Effect.restore":  "The state from which this write gives back what a decrease took, e.g. `{balance: {restore: POSTED}}`. From any other state it gives back nothing, so the write is valid there too.",
	"Effect.sum":      "`<list>.<qty>`: the key is the sum over the lines of qty times `times`; a 64-bit key also gets a line past 2^32.",
	"Effect.times":    "The price in the request of the record each line names: `{total: {sum: lines.qty, times: unit_price}}`.",

	"AliasContract.note":   "What makes this instance different.",
	"AliasContract.fields": "Field overrides for this instance only.",

	"Failure.code":           "The app code in the error envelope. Optional: a shape error has only a connect code.",
	"Failure.connect_code":   "The Connect code, e.g. `invalid_argument`.",
	"Failure.reason":         "The backend's own reason string, verbatim; for a shape or auth failure, a label you choose.",
	"Failure.message":        "The message text, when it is worth pinning.",
	"Failure.field":          "The request field at fault, or the field a uniqueness refusal is about when its reason does not name it.",
	"Failure.when":           "The condition that raises it, written as a condition. `plan` derives probes from it: uniqueness, shortage or limit, a named state, not found, and `invalid_argument` clauses such as empty, zero or negative.",
	"Failure.unreachable":    "Declared but cannot fire, and why; kept out of coverage. E.g. a refusal only a body the proto cannot express would reach.",
	"Failure.unique":         "How a uniqueness refusal compares values, as data: `{case: ignore, trim: true}`. Wins over the prose.",
	"UniqueCompare.case":     "`ignore` adds a case variant expecting the refusal; `exact` adds none.",
	"UniqueCompare.trim":     "`true` adds the value padded with spaces expecting the refusal; `false` adds none.",
	"Failure.scope":          "Only in an overlay's domain-level `failures:`. `all` shares it with every rpc of every domain (declare `unauthenticated` once, in `auth.yaml`).",
	"Failure.pending_deploy": "Declared, correct, and not yet deployed; carries the commit that will make it reachable.",

	"Config.target":      "Where chains run.",
	"Config.descriptor":  "Where the proto descriptor lives and what rebuilds it.",
	"Config.auth":        "The login call, declared once for the whole repo.",
	"Config.paths":       "Where chains, contracts, runs and safe spots live.",
	"Config.conventions": "How this backend names reads and reports its verdict. Every key optional.",
	"Config.latency":     "Latency regression detection in `verify` and in `run` of a chain with a safe spot. A step is slow when it took at least `floor_ms` more AND `ratio` times as long as in the safe spot's run.",
	"Config.volatile":    "Volatile paths applied to every chain.",
	"Config.redact":      "Paths blanked in every run record and never compared by `verify`; credentials belong here. An explicit list REPLACES the defaults: `" + strings.Join(config.DefaultRedact(), "`, `") + "`. Known secrets are also scrubbed by value wherever they appear.",

	"Target.base_url":      "Scheme and host of the backend. Redirects are never followed.",
	"Target.host_override": "Sent as the `Host` header and TLS `ServerName` while connecting to `base_url`.",
	"Target.headers":       "Headers added to every request. Never the auth header when `auth` is declared.",
	"Target.timeout":       "Per-request timeout, e.g. `30s`. Default 30s. A call with no answer in time was still sent.",
	"Target.build_header":  "A response header carrying the server's build (`X-Server-Version`), stamped into each run record as `build`. `run -build <label>` overrides it.",

	"Auth.call":           "The login rpc.",
	"Auth.body":           "Its request body. `${env.X}` for credentials, never a literal; only `${env.*}`, `${uuid}` and clock forms resolve here.",
	"Auth.token_path":     "Response path holding the token.",
	"Auth.expires_path":   "Response path holding the expiry. Without it the token is refreshed only on a 401.",
	"Auth.header":         "Defaults to `Authorization`.",
	"Auth.scheme":         "Defaults to `Bearer`.",
	"Auth.skip_calls":     "Calls that carry no token. Read only at the top level of `auth:`.",
	"Auth.leeway_seconds": "Re-login this many seconds before expiry. Defaults to 60.",
	"Auth.calls":          "Glob patterns of calls this profile owns, e.g. `acme.partner.*`.",
	"Auth.profiles":       "Named additional principals, each with its own token cache; a step picks one with `auth:`.",

	"Descriptor.file":   "The compiled `FileDescriptorSet`.",
	"Descriptor.source": "What `shrt catalog build` compiles.",
	"Descriptor.binary": "The compiler, normally `buf`.",

	"Record.format":        "Record format version, written with the seal.",
	"Record.run_id":        "Timestamp-prefixed, e.g. `20260911T104434Z-e94560bd`; `-run latest` picks the newest.",
	"Record.chain":         "Chain name, which is also the run directory and the safe-spot key.",
	"Record.chain_source":  "Path the chain was loaded from.",
	"Record.chain_digest":  "Fingerprint of the chain file as it ran, name left out; `confirm -approve` and `-rename-from` compare it.",
	"Record.dry_run":       "True for a `-dry-run` record, which is never saved.",
	"Record.target":        "The base_url this ran against.",
	"Record.build":         "Which build answered: the `run -build` label, else `target.build_header`'s value.",
	"Record.started_at":    "UTC start time.",
	"Record.duration_ms":   "Whole-run wall time.",
	"Record.status":        "`passed`, `failed` or `error`; never `skipped`, which is a step status.",
	"Record.vars":          "The resolved vars this run used; secrets show as `<redacted>`.",
	"Record.exports":       "Everything any step exported.",
	"Record.volatile":      "Volatile patterns in force for the whole run, chain plus config.",
	"Record.redacted":      "Redact patterns in force.",
	"Record.steps":         "One entry per step, in order.",
	"Record.failure":       "Why the run stopped, one line per step that did not pass.",
	"Record.warning":       "A run-level warning, e.g. no response carried `envelope_ok`.",
	"Record.seal":          "Checksum written with the record; commands refuse a record whose content no longer matches it.",
	"Record.replay_of":     "On a `verify` replay: the safe spot's run id. `shrt diff <chain>` skips one recorded right after a run.",
	"Record.keep_going":    "True for a `-keep-going` run.",
	"Record.kept_red":      "For a chain with `kept_red`: `as_pinned` (exit 0), `not_as_pinned` or `defect_gone` (exit 1).",
	"Record.kept_red_note": "What `kept_red` found: the pins and what failed or held outside them.",
	"Record.kept_red_slow": "Steps flagged slow against the last run that failed as pinned.",
	"Record.kept_red_new":  "For `not_as_pinned`: each failure outside the pinned defect.",
	"Record.failed_steps":  "Under `-keep-going`, every step that did not pass, in order.",

	"StepRecord.index":             "Position in the chain, from 1.",
	"StepRecord.id":                "The step id.",
	"StepRecord.call":              "As written in the chain.",
	"StepRecord.procedure":         "The resolved `/package.Service/Rpc`.",
	"StepRecord.status":            "`passed`, `failed`, `error`, or `skipped` (a dry-run step, or a `-keep-going` step held back behind one that did not pass).",
	"StepRecord.http_status":       "Transport status. 200 with a non-OK envelope code is an in-band refusal.",
	"StepRecord.latency_ms":        "Wall time of the call.",
	"StepRecord.latency_resent_ms": "Latencies of re-sends of a read that was slow against the safe spot's run.",
	"StepRecord.waited_ms":         "How long the step waited before it was sent, from `wait`.",
	"StepRecord.request":           "What was sent, after resolution and redaction.",
	"StepRecord.headers":           "The step's own headers as sent; a credential-like header is stored as a digest.",
	"StepRecord.response":          "What came back, re-encoded through the response message and redacted: proto names, every declared field at its zero value if omitted, int64 as a string.",
	"StepRecord.body_refs":         "Each body field filled from another step or export, with the reference as written; `verify` compares them to detect a rewired chain.",
	"StepRecord.transport_error":   "The Connect error when the backend answered non-200; `transport.*` reads it.",
	"StepRecord.expect":            "One entry per expectation, with the rule that fired and whether it held. The runner adds `envelope` and `item_envelope` entries for an unpinned in-band refusal.",
	"StepRecord.exported":          "What this step published.",
	"StepRecord.error":             "Why this step failed or could not run.",
	"StepRecord.undeclared":        "Response fields the descriptor does not declare, with the values sent.",
	"StepRecord.warning":           "Non-fatal runner note: stale descriptor, build change, an unasserted refusal.",
	"StepRecord.note":              "Runner commentary, e.g. whether a login seeded a profile's token.",
	"StepRecord.auth_retry":        "`resent`: answered unauthenticated, logged in again and re-sent. `not_resent`: a write that may have been performed was not re-sent.",
	"StepRecord.first_attempt":     "A read's first answer when it was a server error: the read is re-sent once and judged on the answer; the failure stays a FINDING.",
	"StepRecord.token_refused":     "Each token refused at this step: fingerprint, when issued, stated expiry, when refused. A cached token refused on first use is re-minted without a line; repeated early refusal is a `FINDING`.",
	"StepRecord.auth_profile":      "The profile whose token the step carried: `default`, a profile name, `invalid`, or `none`.",
	"StepRecord.auth_principal":    "Digest of the account the profile logged in as, no secret in it; `verify` compares it.",
	"StepRecord.volatile":          "Step-level volatile patterns.",
	"StepRecord.unordered":         "The unordered lists the run applied to this step.",
	"StepRecord.drift":             "With `validate_output` on, the response did not match its message: `failed`, and no expectation was evaluated. Rebuild the descriptor first.",

	"Pin.step": "The step the defect shows at.",
	"Pin.path": "An expectation path of that step that must fail against an answered response; one entry per failing expectation.",
	"Pin.got":  "The value the failed expectation must have got, compared as text; `\"\"` is absent, and a `${...}` reference resolves against the run. Omit to pin only where it fails.",

	"ExpectResult.path":   "The path asserted.",
	"ExpectResult.rule":   "Which rule fired.",
	"ExpectResult.want":   "The comparison value, after `${...}` resolution.",
	"ExpectResult.got":    "What was actually there.",
	"ExpectResult.passed": "Whether it held.",
	"ExpectResult.detail": "Why not, when the rule itself was malformed.",

	"SafeSpot.chain":        "Which chain this is the ground truth for.",
	"SafeSpot.run_id":       "The run a person approved.",
	"SafeSpot.target":       "Where that run happened.",
	"SafeSpot.build":        "The confirmed run's `build`.",
	"SafeSpot.confirmed_by": "The email of the user who said yes.",
	"SafeSpot.proposed_by":  "Who proposed it; `agent` unless `-by` named someone.",
	"SafeSpot.proposed_at":  "When it was proposed.",
	"SafeSpot.confirmed_at": "When it was approved.",
	"SafeSpot.note":         "What makes this run correct.",
	"SafeSpot.supersedes":   "The run id this replaced, under `-supersede`.",
	"SafeSpot.renamed":      "Each rename carried by `confirm <new> -rename-from <old>`.",
	"SafeSpot.chain_digest": "The confirmed run's `chain_digest`.",
	"SafeSpot.volatile":     "The volatile patterns approved with the run; `verify` fails a replay masked by any other.",
	"SafeSpot.digest":       "Fingerprint of the content and the approval; `verify` refuses a safe spot edited by hand.",
	"SafeSpot.steps":        "The confirmed step records a replay is diffed against.",

	"Latency.floor_ms":               "Milliseconds slower than the safe spot's run before a step can be flagged. Default 250.",
	"Latency.ratio":                  "Times as slow as the safe spot's run before a step can be flagged. Default 3.",
	"Latency.remeasure":              "How many times a slow read is re-sent before it is judged (0..10). Default 2.",
	"Latency.fail":                   "True: a confirmed slowdown fails `verify`. Default false, a warning; `shrt init` writes `true`.",
	"Latency.off":                    "True: no latency comparison.",
	"Conventions.read_only_prefixes": "Rpc-name prefixes that mean a read, matched at a word boundary. Default: Fetch, Get, List, Preview, Search, Read, Query, Find, Lookup, Describe, Show, Count, Export, Download, Retrieve, Watch, Subscribe.",
	"Conventions.envelope_path":      "Where a response reports its verdict. Default `error.code`. A path no response declares fails `run`.",
	"Conventions.envelope_ok":        "The `envelope_path` value that means success. Default `OK`.",
	"Conventions.item_envelope_path": "Per-item verdict in a batch response, `<list>[].<path>` (e.g. `results[].error.code`); without it a batch refusing every line passes.",
	"Conventions.code_fields":        "Detail fields carrying the backend's own code, searched by `chain which -code`. Default `app_code`, `reason`, `error_code`; an explicit list replaces them.",
	"Conventions.validate_output":    "True: a response that does not match its message fails the step (`drift`). Default false: undeclared fields are dropped with a warning.",

	"Paths.chains":    "Chain YAML directory.",
	"Paths.contracts": "Contract overlay directory, one file per domain; only top-level `.yaml`/`.yml` files load.",
	"Paths.runs":      "Run record directory.",
	"Paths.safespots": "Safe spot directory.",
}

func main() {
	check := flag.Bool("check", false, "compare against the committed file and exit non-zero on drift")
	flag.Parse()

	out, err := render()
	if err != nil {
		fmt.Fprintln(os.Stderr, "distill:", err)
		os.Exit(1)
	}
	if *check {
		have, err := os.ReadFile(target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "distill -check:", err)
			os.Exit(1)
		}
		if !bytes.Equal(have, out) {
			fmt.Fprintf(os.Stderr, "distill -check: %s is stale — the code moved and the distillation did not.\n", target)
			fmt.Fprintln(os.Stderr, "run:", rerun)
			os.Exit(1)
		}
		fmt.Printf("distill: %s matches the code\n", target)
		return
	}
	if err := os.WriteFile(target, out, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "distill:", err)
		os.Exit(1)
	}
	fmt.Printf("distill: wrote %s (%d bytes)\n", target, len(out))
}

var undocumented []string

func render() ([]byte, error) {
	undocumented = nil
	var b strings.Builder
	b.WriteString("# GRAMMAR — every key shrt accepts\n\n")
	b.WriteString("Generated from the Go structs by `go run ./distill`; do not edit. The loaders reject unknown keys,\n")
	b.WriteString("so a key not in this file does not exist: `unknown key \"expects\" at line 16 (did you mean \"expect\"?)`.\n")
	b.WriteString("A `+` in the `req` column means the key is always written out (no `omitempty`).\n\n")

	b.WriteString("## 1. Chain file — `.shrt/chains/<name>.yaml`\n\n")
	table := func(heading string, v any) {
		b.WriteString(heading)
		writeTable(&b, v)
	}
	table("", chain.Chain{})
	table("\n### Step\n\n", chain.Step{})
	table("\n### Expectation — exactly one rule per entry\n\n", chain.Expectation{})

	b.WriteString("\n### What each rule actually does\n\n")
	b.WriteString("Produced by evaluating each rule against a fixture response:\n\n")
	rules, err := exerciseRules()
	if err != nil {
		return nil, err
	}
	b.WriteString(rules)

	b.WriteString("\n### Reserved `transport.*` paths — the call's transport outcome\n\n")
	b.WriteString(exerciseTransport())
	table("\n### `kept_red[]` — a known defect the chain pins\n\n", chain.Pin{})

	b.WriteString("\n## 2. References — `${...}`\n\n")
	b.WriteString("Resolved in `body`, `headers`, and an expectation's `equals`, `not_equal`, `contains` and numeric\n")
	b.WriteString("bounds; never in `path`. A reference that is the whole value keeps its JSON type; inside a longer\n")
	b.WriteString("string it is text, and only a scalar may be interpolated so. A reference that cannot resolve fails\n")
	b.WriteString("the step, and one known not to resolve (a later or missing step, an unset `${env.*}`, a field the\n")
	b.WriteString("producing message does not declare) is a lint error and refused before sending. There is no escape\n")
	b.WriteString("for a literal `${`; pass such a value through `${env.NAME}` or `-var`. `${nowunix}` resolves to a\n")
	b.WriteString("STRING of digits.\n\n")
	b.WriteString("Produced by resolving each form against a fixture scope:\n\n")
	refs, err := exerciseRefs()
	if err != nil {
		return nil, err
	}
	b.WriteString(refs)

	table("\n## 3. Contract overlay — `.shrt/contracts/<domain>.yaml`\n\n", contract.Overlay{})
	table("\n### Per rpc\n\n", contract.RPCContract{})
	table("\n### `fields.<name>`\n\n", contract.FieldContract{})
	table("\n### `aliases.<name>`\n\n", contract.AliasContract{})
	table("\n### `effects.<field>`\n\n", contract.Effect{})
	table("\n### `failures[]`\n\n", contract.Failure{})
	table("\n### `failures[].unique`\n\n", contract.UniqueCompare{})

	table("\n## 4. Config — `.shrt/config.yaml`\n\n", config.Config{})
	table("\n### `target`\n\n", config.Target{})
	table("\n### `descriptor`\n\n", config.Descriptor{})
	table("\n### `auth`, and each entry of `auth.profiles`\n\n", config.Auth{})
	table("\n### `paths`\n\n", config.Paths{})
	table("\n### `conventions`\n\n", config.Conventions{})
	table("\n### `latency`\n\n", config.Latency{})

	b.WriteString("\n## 5. Run record — `.shrt/runs/<chain>/<run-id>.json`\n\n")
	b.WriteString("The evidence file; the JSON names below are the ones in the file.\n\n")
	writeJSONTable(&b, runner.Record{})
	b.WriteString("\n### Each entry of `steps`\n\n")
	writeJSONTable(&b, runner.StepRecord{})
	b.WriteString("\n### Each entry of a step's `expect`\n\n")
	writeJSONTable(&b, chain.ExpectResult{})

	b.WriteString("\n## 6. Safe spot — `.shrt/safespots/<chain>.json`\n\n")
	b.WriteString("Written only by `shrt confirm <chain> -approve`. A proposal waits in\n")
	b.WriteString("`.shrt/safespots/pending/<chain>.json` with its report beside it until approved or rejected.\n\n")
	writeJSONTable(&b, store.SafeSpot{})

	b.WriteString("\n## 7. What `shrt verify` actually compares\n\n")
	b.WriteString("Produced by running `diff.Compare` on a fabricated safe spot and replay:\n\n")
	rep, err := exerciseDiff()
	if err != nil {
		return nil, err
	}
	b.WriteString(rep)

	b.WriteString("\n## 8. Closed vocabularies\n\n")
	b.WriteString("Read from the constants themselves, so a renamed constant shows up here:\n\n")
	b.WriteString("| where | allowed |\n|---|---|\n")
	fmt.Fprintf(&b, "| chain `apiVersion` | `%s` |\n", chain.APIVersion)
	fmt.Fprintf(&b, "| overlay `apiVersion` | `%s` |\n", contract.OverlayAPIVersion)
	fmt.Fprintf(&b, "| `status` | `%s`, `%s` |\n", contract.StatusDraft, contract.StatusVerified)
	fmt.Fprintf(&b, "| `checked_by` | `%s`, `%s`, `%s` |\n", contract.CheckedByFK, contract.CheckedByAppLookup, contract.CheckedByNone)
	fmt.Fprintf(&b, "| `from` / `same_as` separator | `%s` (legacy `%s` still parses) |\n", contract.RefSeparator, contract.LegacyRefSeparator)
	fmt.Fprintf(&b, "| default auth profile | `%s` |\n", config.DefaultAuthProfile)
	fmt.Fprintf(&b, "| reserved `auth:` value (never a profile name) | `%s` |\n", config.InvalidTokenProfile)
	fmt.Fprintf(&b, "| run record status | `%s`, `%s`, `%s` |\n", runner.StatusPassed, runner.StatusFailed, runner.StatusError)
	fmt.Fprintf(&b, "| STEP status | the three above, plus `%s` |\n", runner.StatusSkipped)

	b.WriteString("\n**`" + runner.StatusError + "`** means the step produced no answer to judge; usually nothing was sent. Read\n")
	b.WriteString("the step's `error` before blaming the backend. **`" + runner.StatusSkipped + "`** is a step status only: a\n")
	b.WriteString("dry-run step, or a `-keep-going` step held back behind one that did not pass. A step with\n")
	b.WriteString("`\"drift\": true` is `" + runner.StatusFailed + "`: it was sent and answered, but did not match its message\n")
	b.WriteString("under `validate_output`, so no expectation was evaluated; rebuild the descriptor before reading it\n")
	b.WriteString("as a backend defect.\n")

	b.WriteString("\n## 9. Commands\n\n")
	b.WriteString("Captured by running the binary, so a renamed command cannot survive here:\n\n")
	cli, err := exerciseCLI()
	if err != nil {
		return nil, err
	}
	b.WriteString(cli)

	b.WriteString("\n## 10. Volatile and redact patterns\n\n")
	b.WriteString("Produced by running `pathmask.Match(pattern, path)`:\n\n")
	b.WriteString(exerciseMasks())
	b.WriteString("\nA `*` globs inside a segment and matching folds separators and case, so `**.*password` covers\n")
	b.WriteString("`user_password` and `userPassword` alike. `**.` reaches any depth; without it a pattern matches\n")
	b.WriteString("only that exact path.\n")
	if len(undocumented) > 0 {
		return nil, fmt.Errorf("%d key(s) or field(s) exist in code with no entry in distill/main.go's notes, so GRAMMAR.md would ship them undocumented: %s",
			len(undocumented), strings.Join(undocumented, ", "))
	}
	return []byte(b.String()), nil
}

func writeTable(b *strings.Builder, v any) {
	t := reflect.TypeOf(v)
	b.WriteString("| key | type | req | meaning |\n|---|---|---|---|\n")
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		if key, _, _ := strings.Cut(tag, ","); key != "" && key != "-" {
			req := "+"
			if strings.Contains(tag, "omitempty") {
				req = ""
			}
			fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n", key, yamlType(f.Type), req, noteFor(t.Name(), key))
		}
	}
}

func writeJSONTable(b *strings.Builder, v any) {
	t := reflect.TypeOf(v)
	b.WriteString("| field | type | meaning |\n|---|---|---|\n")
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if key, _, _ := strings.Cut(f.Tag.Get("json"), ","); key != "" && key != "-" {
			fmt.Fprintf(b, "| `%s` | %s | %s |\n", key, yamlType(f.Type), noteFor(t.Name(), key))
		}
	}
}

func noteFor(name, key string) string {
	note := notes[name+"."+key]
	if note == "" {
		undocumented = append(undocumented, name+"."+key)
	}
	return note
}

func yamlType(t reflect.Type) string {
	if t == reflect.TypeOf(json.RawMessage{}) {
		return "JSON"
	}
	switch t.Kind() {
	case reflect.Ptr:
		return yamlType(t.Elem())
	case reflect.Slice:
		return "list of " + yamlType(t.Elem())
	case reflect.Map:
		return "map " + yamlType(t.Key()) + " → " + yamlType(t.Elem())
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int32, reflect.Int64:
		return "int"
	case reflect.Interface:
		return "any"
	case reflect.Struct:
		return strings.ToLower(t.Name())
	default:
		return t.Kind().String()
	}
}

func exerciseRules() (string, error) {
	resp := map[string]any{
		"error":      map[string]any{"code": "OK"},
		"id_deal":    "d-1",
		"rows":       []any{},
		"count":      float64(0),
		"total":      "0",
		"expires_at": "1789127074",
		"expires_ms": "1789127074000",
		"created_at": "2026-09-11T10:44:34Z",
	}
	cases := []struct {
		label string
		e     chain.Expectation
		kind  string
	}{
		{"`equals: OK` on `error.code`", chain.Expectation{Path: "error.code", Equals: "OK"}, ""},
		{"`equals: NOPE` on `error.code`", chain.Expectation{Path: "error.code", Equals: "NOPE"}, ""},
		{"`equals: 0` on `count` (number vs text)", chain.Expectation{Path: "count", Equals: "0"}, ""},
		{"`not_equal: \"\"` on `id_deal`", chain.Expectation{Path: "id_deal", NotEqual: ""}, ""},
		{"`not_equal: x` on a path that is ABSENT", chain.Expectation{Path: "nope", NotEqual: "x"}, ""},
		{"`contains: d-` on `id_deal`", chain.Expectation{Path: "id_deal", Contains: "d-"}, ""},
		{"`gt: 1789123474` on `expires_at` (an int64, stored as text)", chain.Expectation{Path: "expires_at", Gt: "1789123474"}, "int64"},
		{"`between: [1789127000, 1789127100]` on `expires_at`", chain.Expectation{Path: "expires_at", Between: []any{"1789127000", "1789127100"}}, "int64"},
		{"`within: {of: 1789127074, by: 5}` on `expires_ms`, the same instant in milliseconds", chain.Expectation{Path: "expires_ms", Within: &chain.Within{Of: "1789127074", By: 5}}, "int64"},
		{"`lte: 1789123474` on `created_at` (RFC3339, read as unix seconds)", chain.Expectation{Path: "created_at", Lte: "1789123474"}, ""},
		{"`gt: 0` on `id_deal` (not a number)", chain.Expectation{Path: "id_deal", Gt: 0}, ""},
		{"`not_empty: true` on `id_deal`", chain.Expectation{Path: "id_deal", NotEmpty: true}, ""},
		{"`not_empty: true` on an empty list", chain.Expectation{Path: "rows", NotEmpty: true}, ""},
		{"`not_empty: true` on the number 0", chain.Expectation{Path: "count", NotEmpty: true}, "int32"},
		{"`not_empty: true` on an int64 at 0 (stored as the string `\"0\"`)", chain.Expectation{Path: "total", NotEmpty: true}, "int64"},
		{"`not_equal: \"\"` on an int64 at 0", chain.Expectation{Path: "total", NotEqual: ""}, "int64"},
		{"`exists: true` on a path that is absent", chain.Expectation{Path: "nope", Exists: boolp(true)}, ""},
		{"no rule at all", chain.Expectation{Path: "id_deal"}, ""},
		{"TWO rules on one entry: `equals: NOPE` **and** `not_empty: true`", chain.Expectation{Path: "error.code", Equals: "NOPE", NotEmpty: true}, ""},
	}
	var b strings.Builder
	b.WriteString("| expectation | rule fired | passes |\n|---|---|---|\n")
	for _, c := range cases {
		r := c.e.EvaluateTyped(resp, resp, c.kind)
		detail := r.Rule
		if r.Detail != "" {
			detail = r.Rule + " — " + r.Detail
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", c.label, detail, yesNo(r.Passed))
	}
	b.WriteString("\n`not_empty` is false for `0` and `[]`, so it cannot stand in for `exists`; an int64 `\"0\"` is zero. A\n")
	b.WriteString("rule-less entry fails. Two rules on one entry are a lint error: write one rule per entry.\n\n")
	b.WriteString(existsPresence())
	return b.String(), nil
}

func existsPresence() string {
	full := map[string]any{
		"error":        map[string]any{"code": "unauthenticated", "message": "no"},
		"access_token": "",
		"expires_at":   "0",
		"user":         nil,
	}
	present := map[string]any{
		"error": map[string]any{"code": "unauthenticated", "message": "no"},
	}
	cases := []struct {
		label string
		e     chain.Expectation
	}{
		{"`exists: true` on `access_token`", chain.Expectation{Path: "access_token", Exists: boolp(true)}},
		{"`exists: false` on `access_token`", chain.Expectation{Path: "access_token", Exists: boolp(false)}},
		{"`not_empty: true` on `access_token`", chain.Expectation{Path: "access_token", NotEmpty: true}},
		{"`equals: \"\"` on `access_token`", chain.Expectation{Path: "access_token", Equals: ""}},
		{"`exists: true` on `error.code`", chain.Expectation{Path: "error.code", Exists: boolp(true)}},
		{"`equals: \"\"` on `user.name`, inside a message not sent", chain.Expectation{Path: "user.name", Equals: ""}},
		{"`exists: false` on `user.name`, inside a message not sent", chain.Expectation{Path: "user.name", Exists: boolp(false)}},
	}
	var b strings.Builder
	b.WriteString("`exists` reads what the server SENT, not the stored record, which holds every declared field at\n")
	b.WriteString("its zero value. Below, the server sent `{\"error\": {...}}` and nothing else:\n\n")
	b.WriteString("| expectation | rule fired | passes |\n|---|---|---|\n")
	for _, c := range cases {
		r := c.e.EvaluateIn(full, present)
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", c.label, r.Rule, yesNo(r.Passed))
	}
	b.WriteString("\nA proto3 scalar without `optional` cannot tell unset from zero on the wire; assert the value. A field\n")
	b.WriteString("inside a message the server did not send has no zero value: assert the message `exists: false`.\n")
	return b.String()
}

func exerciseTransport() string {
	var b strings.Builder
	b.WriteString("A `" + chain.TransportPrefix + ".*` path reads the recorded transport result, not the body. It is the only\n")
	b.WriteString("assertion that runs on a Connect error (HTTP 4xx/5xx); every other one is `unevaluated` and fails.\n\n")
	b.WriteString("| path | reads |\n|---|---|\n")
	for _, p := range chain.TransportFieldNames() {
		fmt.Fprintf(&b, "| `%s` | %s |\n", p, chain.TransportFields[strings.TrimPrefix(p, chain.TransportPrefix+".")])
	}
	refused := chain.TransportOutcome(401, "unauthenticated", "token rejected")
	answered := chain.TransportOutcome(200, "", "")
	cases := []struct {
		label string
		e     chain.Expectation
	}{
		{"`transport.code` `equals: unauthenticated`", chain.Expectation{Path: "transport.code", Equals: "unauthenticated"}},
		{"`transport.http_status` `equals: 401`", chain.Expectation{Path: "transport.http_status", Equals: 401}},
		{"`transport.code` `equals: " + chain.TransportOK + "`", chain.Expectation{Path: "transport.code", Equals: chain.TransportOK}},
		{"`transport.message` `exists: false`", chain.Expectation{Path: "transport.message", Exists: boolp(false)}},
		{"`transport.message` `contains: token`", chain.Expectation{Path: "transport.message", Contains: "token"}},
	}
	b.WriteString("\nEvaluated against a 401 `unauthenticated` refusal and against a 200 answer:\n\n")
	b.WriteString("| expectation | refused 401 | answered 200 |\n|---|---|---|\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", c.label, yesNo(c.e.Evaluate(refused).Passed), yesNo(c.e.Evaluate(answered).Passed))
	}
	b.WriteString("\nA refused call whose `transport.*` assertions all hold is `passed`, with no `allow_fail`.\n")
	return b.String()
}

var uuidShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func exerciseRefs() (string, error) {
	sc := chain.ReferenceExampleScope()
	var b strings.Builder
	b.WriteString("| reference | resolves to | which is |\n|---|---|---|\n")
	for _, f := range chain.ReferenceExamples {
		v, err := sc.ResolveValue(f.Ref)
		if err != nil {
			return "", fmt.Errorf("reference %s no longer resolves: %w", f.Ref, err)
		}
		shown := fmt.Sprintf("`%v`", v)
		if str, ok := v.(string); ok {
			shown = fmt.Sprintf("`%q`", str)
		}
		if f.Ref == "${uuid}" {
			s, _ := v.(string)
			if !uuidShape.MatchString(s) {
				return "", fmt.Errorf("${uuid} produced %q, which is not a v4 uuid", s)
			}
			shown = "`" + uuidShape.String() + "`"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", f.Ref, shown, f.Meaning)
	}
	older, values := []string{}, map[string]bool{}
	for _, f := range chain.OlderReferenceExamples {
		v, err := sc.ResolveValue(f.Ref)
		if err != nil {
			return "", fmt.Errorf("reference %s no longer resolves: %w", f.Ref, err)
		}
		older = append(older, fmt.Sprintf("`%s` (%s)", f.Ref, f.Meaning))
		values[fmt.Sprintf("`%q`", v)] = true
	}
	if len(values) != 1 {
		return "", fmt.Errorf("the older reference spellings resolve to %d values, not one", len(values))
	}
	for v := range values {
		fmt.Fprintf(&b, "\n`contract plan` and `chain new` write those two forms. Also accepted, for chains already written so: %s and %s, each %s.\n",
			strings.Join(older[:len(older)-1], ", "), older[len(older)-1], v)
	}
	return b.String(), nil
}

func boolp(v bool) *bool { return &v }

func exerciseMasks() string {
	cases := []struct{ pattern, path string }{
		{"**.created_at", "deals.0.created_at"},
		{"**.created_at", "created_at"},
		{"**.created_at", "deals.0.updated_at"},
		{"deals.*.id_deal", "deals.0.id_deal"},
		{"deals.*.id_deal", "deals.0.legs.0.id_deal"},
		{"deals.0.id_deal", "deals.0.id_deal"},
		{"deals.0.id_deal", "deals.1.id_deal"},
		{"error.code", "error.code"},
		{"error.code", "error.details.0.code"},
		{"**.*password", "login.user_password"},
		{"**.*password", "login.password"},
		{"**.access_token", "auth.accessToken"},
		{"**.*pin", "login.user_pin"},
		{"**.*pin", "login.opinion"},
	}
	var b strings.Builder
	b.WriteString("| pattern | path | matches |\n|---|---|---|\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", c.pattern, c.path, yesNo(pathmask.Match(c.pattern, c.path)))
	}
	return b.String()
}

func exerciseDiff() (string, error) {
	step := func(id, call, status, body string) *runner.StepRecord {
		return &runner.StepRecord{ID: id, Call: call, Status: status, Response: json.RawMessage(body)}
	}
	spot := &store.SafeSpot{
		Chain: "demo", RunID: "SPOT",
		Steps: []*runner.StepRecord{
			step("a", "S/A", runner.StatusPassed, `{"qty":1,"created_at":"T0","note":"x","owner_id":"u-1","seen":"2026-09-01T10:00:00Z","lines":[1,2]}`),
			step("b", "S/B", runner.StatusPassed, `{"qty":"1"}`),
			step("c", "S/C", runner.StatusPassed, `{"created_at":"2026-09-01T10:00:00Z"}`),
		},
		Volatile: []string{"**.created_at"},
	}
	rec := &runner.Record{
		RunID: "REPLAY",
		Steps: []*runner.StepRecord{
			step("a", "S/A", runner.StatusPassed, `{"qty":2,"created_at":"T1","note":"x","owner_id":"u-2","seen":"2026-09-02T11:30:00Z","lines":[1]}`),
			step("b", "S/B", runner.StatusPassed, `{"qty":1}`),
			step("c", "S/C", runner.StatusPassed, `{"created_at":null}`),
		},
	}
	rep := diff.Compare(spot, rec)
	var b strings.Builder
	b.WriteString("| what changed between the confirmed run and the replay | reported? |\n|---|---|\n")
	seen := map[string]bool{}
	for _, c := range rep.Changes {
		seen[c.Step+"|"+c.Path] = true
	}
	fmt.Fprintf(&b, "| `qty` went from `1` to `2` | %s |\n", yesNo(seen["a|qty"]))
	fmt.Fprintf(&b, "| `created_at` changed, and is `volatile` | %s |\n", yesNo(seen["a|created_at"]))
	fmt.Fprintf(&b, "| `created_at` became null, and is `volatile` | %s |\n", yesNo(seen["c|created_at"]))
	fmt.Fprintf(&b, "| `note` did not change | %s |\n", yesNo(seen["a|note"]))
	fmt.Fprintf(&b, "| `owner_id` changed, NOT volatile, but its name is id-shaped | %s |\n", yesNo(seen["a|owner_id"]))
	fmt.Fprintf(&b, "| `seen` changed, NOT volatile, but both values are timestamps | %s |\n", yesNo(seen["a|seen"]))
	fmt.Fprintf(&b, "| `lines` went from 2 items to 1 | %s |\n", yesNo(seen["a|lines"]))
	fmt.Fprintf(&b, "| `qty` went from the STRING `\"1\"` to the NUMBER `1` | %s |\n", yesNo(seen["b|qty"]))
	b.WriteString("\nBesides `volatile` paths, verify masks a changed value that is id- or timestamp-shaped on both\n")
	b.WriteString("sides (same kind of id, same unit of time, within 400 days of its run), and a value that only\n")
	fmt.Fprintf(&b, "echoes a fixture name or a `${uuid}` the chain sent; here %d value(s) were masked. A value lost\n", rep.Masked)
	b.WriteString("under a volatile pattern (null, empty, gone) is still reported. `-masked` lists every masked value.\n")
	b.WriteString("Ids are renamed consistently across the record, so a stale id is reported. A list declared\n")
	b.WriteString("`unordered` is compared as a multiset. A value under a `redact` path is never compared. Before the\n")
	b.WriteString("responses, verify compares what each step SENT and the chain itself with the safe spot's run: a\n")
	b.WriteString("changed input gives `drift with different input`, a changed chain `drift after a chain change`, when\n")
	b.WriteString("it explains every response change; anything else is a `regression`.\n")
	fmt.Fprintf(&b, "\nChange kinds `shrt verify` and `shrt diff` print: `%s` (a path the baseline had is gone), `%s`\n", diff.KindMissing, diff.KindUnexpected)
	fmt.Fprintf(&b, "(a path the baseline did not have), `%s` (same JSON type, different value), `%s` (different JSON\n", diff.KindChanged, diff.KindType)
	fmt.Fprintf(&b, "type), `%s` (a list or the step count has a different number of items), `%s` (a step id or rpc\n", diff.KindLength, diff.KindOrder)
	fmt.Fprintf(&b, "differs at that position), `%s` (the step's pass/fail status changed).\n", diff.KindStatus)
	b.WriteString("Steps are paired by id, then by call and position, so a renamed or inserted step is one change.\n")
	b.WriteString("References and paths are compared in one canonical spelling, so a field respelt in case, as its\n")
	b.WriteString("JSON name or as `${steps.a.response.x}` for `${a.x}` is no change.\n")
	return b.String(), nil
}

func yesNo(v bool) string {
	if v {
		return "**yes**"
	}
	return "no"
}

func exerciseCLI() (string, error) {
	root, err := runShrt()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("```\n" + strings.TrimSpace(root) + "\n```\n\n")
	subs := []struct{ group, usage string }{}
	for _, group := range []string{"catalog", "chain", "contract"} {
		out, err := runShrt(group)
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(out, "\n") {
			marker := "usage: shrt " + group + " "
			at := strings.Index(line, marker)
			if at < 0 {
				continue
			}
			usage := strings.TrimSpace(line[at+len(marker):])
			usage = strings.ReplaceAll(usage, "|", "\\|")
			subs = append(subs, struct{ group, usage string }{group, usage})
			break
		}
	}
	b.WriteString("| group | subcommands |\n|---|---|\n")
	for _, s := range subs {
		fmt.Fprintf(&b, "| `shrt %s` | `%s` |\n", s.group, s.usage)
	}
	b.WriteString("\nEvery command prints its flags and exit codes with `-h`. A run id is accepted with or without `.json`.\n")
	return b.String(), nil
}

var goRunExit = regexp.MustCompile(`(?m)^exit status \d+\n?`)

func runShrt(args ...string) (string, error) {
	full := append([]string{"run", "./cmd/shrt"}, args...)
	cmd := exec.Command("go", full...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()
	text := goRunExit.ReplaceAllString(out.String(), "")
	if text == "" {
		return "", fmt.Errorf("shrt %v produced no usage output", args)
	}
	return text, nil
}
