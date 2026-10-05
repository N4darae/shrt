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
	"Chain.name":        "Names the run directory and safe spot. Defaults to the file name; differing from it is a lint warning, sharing it a lint error.",
	"Chain.description": "What state this chain reproduces, for the next reader.",
	"Chain.vars":        "`${vars.x}`; override with `-var x=y`. An undeclared var must come from `-var`, except `tag`, which is fresh each run.",
	"Chain.volatile":    "Response paths `shrt verify` and `shrt diff` mask in every step. Expectations still see the real value.",
	"Chain.unordered":   "Response lists `verify` compares as multisets, for an rpc that promises no order. No indices: `invoices.lines`.",
	"Chain.redact":      "Paths blanked everywhere in the run record and never compared, added to the config's. Only what must not be stored.",
	"Chain.steps":       "Ordered; never reordered, parallelised or skipped.",
	"Chain.kept_red":    "A known defect the chain shows on purpose. `run` exits 0 only while it fails exactly as pinned; never confirmed.",

	"Step.id":          "Unique; later steps reference it. Derived from the rpc name when omitted.",
	"Step.description": "Why this step is here and what its assertions mean.",
	"Step.call":        "`package.Service/Rpc`, `Service/Rpc`, or a bare unambiguous `Rpc`. A server stream records its first message as `messages.0`, within 5s; client and bidi streams are refused.",
	"Step.body":        "The request, validated against the proto request message before anything is sent.",
	"Step.headers":     "Per-step headers. Never `Authorization`: use `auth: <profile>`.",
	"Step.expect":      "Assertions on the response, one rule per entry.",
	"Step.export":      "`name: <response path>` with no `response.` prefix, e.g. `id_invoice: invoice.id_invoice`. Publishes `${exports.name}` and `${name}`. A name equal to a step id is refused; one exported twice is a warning.",
	"Step.auth":        "The auth profile for this step. `invalid` sends a never-issued token: the invalid-token probe.",
	"Step.skip_auth":   "Attach no auth header: the missing-token probe. A login step does not need it.",
	"Step.allow_fail":  "Go on past a transport refusal on a step with no expectations. Never waives a failed expectation or an `error`.",
	"Step.volatile":    "Volatile paths for this step only, added to the chain's.",
	"Step.unordered":   "Unordered lists for this step only, added to the chain's.",
	"Step.wait":        "A Go duration, at most `10m`, waited before sending, outside its latency.",

	"Expectation.path":      "Response path (`invoice.lines.0.amount_minor`) or a `transport.*` path. No `${...}`. A path the message lacks is a lint error. Names match ignoring case and separators.",
	"Expectation.equals":    "Compared as text: `1` matches `\"1\"`. May carry `${...}`; no arithmetic.",
	"Expectation.not_equal": "Present AND different; an absent path fails it. May carry `${...}`.",
	"Expectation.contains":  "Substring of the value's text. May carry `${...}`.",
	"Expectation.includes":  "Some item of the list matches: a map names item fields, a scalar is the item. May carry `${...}`.",
	"Expectation.exists":    "Whether the server SENT the path; see the second table below.",
	"Expectation.gt":        "Present and a number greater than this; an int64 as text is its number, an RFC3339 time its unix seconds. May carry `${...}`.",
	"Expectation.gte":       "As `gt`, greater than or equal.",
	"Expectation.lt":        "As `gt`, less than.",
	"Expectation.lte":       "As `gt`, less than or equal.",
	"Expectation.between":   "Exactly two inclusive bounds `[low, high]`, read as `gt` reads them.",
	"Expectation.within":    "`{of: X, by: N}`: at most N from X. The rule for clocks: `within: {of: \"${nowunix+3600}\", by: 5}`.",
	"Within.of":             "The value to be near, read as `gt` reads its bound. May carry `${...}`.",
	"Within.by":             "The largest distance allowed, a number.",
	"Expectation.not_empty": "Present and not `\"\"`, `0`, `false`, `[]` or `{}`; an int64 `\"0\"` is zero too.",

	"Overlay.apiVersion":  "`shrt/contract/v1`.",
	"Overlay.domain":      "Defaults to the file name.",
	"Overlay.description": "Domain-wide prose: the invariants and call order every rpc here shares.",
	"Overlay.failures":    "Failures every rpc in the domain inherits; `scope: all` shares one with every domain.",
	"Overlay.rpcs":        "Keyed by the fully qualified `package.Service/Rpc`.",

	"RPCContract.summary":       "Prose for people. Numbers a write moves go in `effects`.",
	"RPCContract.effects":       "What it does to numbers, keyed by field: `{balance: {increase: amount}}`, or `none`, `zero` (a create starts at 0), `per_item` (on a repeated request field: each item applied or refused alone).",
	"RPCContract.note":          "Free text. Spares no quality term.",
	"RPCContract.auth":          "The profile the rpc needs instead of the default; `plan` puts it on the step.",
	"RPCContract.requires_role": "Roles the caller must hold; `[NONE]`: no role gate. `plan` calls a gated rpc as each other profile, expecting denial.",
	"RPCContract.no_producer":   "Why no write here creates the rows this read returns: a seed, a feed.",
	"RPCContract.required":      "Fields the server rejects without, reads too. `[NONE]`: nothing. `[UNKNOWN]`: handler not found.",
	"RPCContract.needs":         "An rpc that must run first though no field consumes its output, e.g. the write creating what a list lists. A write that only reaches this rpc's `restore:` state also makes `plan` call it from the state before.",
	"RPCContract.before":        "The inverse of `needs`, declared in the prerequisite's domain. Plain rpc names only.",
	"RPCContract.fields":        "Per request field. Dotted keys nest; `lines.1.id_account` picks an entry, `lines.qty` applies to every entry.",
	"RPCContract.aliases":       "Per-instance overrides, so two aliased steps of one rpc differ.",
	"RPCContract.exports":       "Response paths worth exporting, and why.",
	"RPCContract.terminal":      "Response fields that deliberately have no consumer.",
	"RPCContract.soft_signals":  "Advisory response fields, neither success nor failure.",
	"RPCContract.failures":      "One entry per way this rpc refuses.",
	"RPCContract.source":        "Files read to determine all this, without line ranges.",
	"RPCContract.status":        "`draft`, or `verified` with a `verified_run`. An agent leaves it `draft`.",
	"RPCContract.verified_by":   "The person who verified it.",
	"RPCContract.verified_run":  "The run id that proved it.",

	"FieldContract.from":       "`<rpc>[@alias]->response_path`: the value comes from an earlier call's response, which orders the two.",
	"FieldContract.value":      "A fixed literal or template, e.g. `${uuid}` or `inv-${vars.tag}-a`. `value: \"0\"` marks a zero as deliberate.",
	"FieldContract.same_as":    "`<rpc>[@alias]->request_path`: must equal what an earlier call SENT. Exclusive with `from`.",
	"FieldContract.oneof":      "Mutual-exclusion group; one member carries a value.",
	"FieldContract.checked_by": "How the server validates the id, which decides whether a bad one is a named failure or an unnamed 500.",
	"FieldContract.note":       "Units, formats, constraints; `plan` reads uniqueness, normalisation and stated bounds from it.",

	"Effect.increase": "The request number it grows by: `amount`, or `lines.amount` per line, on the record a `from:`-wired id names.",
	"Effect.decrease": "As `increase`, shrinking.",
	"Effect.of":       "An id wired `from:` another write whose request holds the path: `{decrease: lines.amount, of: id_invoice}`. Not with `restore`.",
	"Effect.restore":  "The state from which it gives back what a decrease took: `{balance: {restore: POSTED}}`. From other states, nothing.",
	"Effect.sum":      "`<list>.<qty>`: the key sums qty times `times` over the lines.",
	"Effect.times":    "The price in the request of the record each line names: `{total: {sum: lines.qty, times: unit_price}}`.",

	"AliasContract.note":   "What makes this instance different.",
	"AliasContract.fields": "Field overrides for this instance only.",

	"Failure.code":           "The app code in the error envelope. Optional: a shape error has only a connect code.",
	"Failure.connect_code":   "The Connect code, e.g. `invalid_argument`.",
	"Failure.reason":         "The backend's own reason string, verbatim; for a shape or auth failure, a label you choose.",
	"Failure.message":        "The message text, when it is worth pinning.",
	"Failure.field":          "The request field at fault, or the field a uniqueness refusal is about when its reason does not name it.",
	"Failure.when":           "The condition that raises it. `plan` derives probes from it: uniqueness, a limit, a named state, not found, and `invalid_argument` clauses such as empty, zero or negative.",
	"Failure.unreachable":    "Why it cannot fire, e.g. only a body the proto cannot express reaches it. Kept out of coverage.",
	"Failure.unique":         "How a uniqueness refusal compares values, as data: `{case: ignore, trim: true}`. Wins over the prose.",
	"UniqueCompare.case":     "`ignore` adds a case variant expecting the refusal; `exact` adds none.",
	"UniqueCompare.trim":     "`true` adds the value padded with spaces expecting the refusal; `false` adds none.",
	"Failure.scope":          "Domain-level `failures:` only. `all` shares it with every domain: declare `unauthenticated` once.",
	"Failure.pending_deploy": "Declared, correct, and not yet deployed; carries the commit that will make it reachable.",

	"Config.target":      "Where chains run.",
	"Config.descriptor":  "Where the proto descriptor lives and what rebuilds it.",
	"Config.auth":        "The login call, declared once for the whole repo.",
	"Config.paths":       "Where chains, contracts, runs and safe spots live.",
	"Config.conventions": "How this backend names reads and reports its verdict. Every key optional.",
	"Config.latency":     "Slowdown detection against the safe spot's run: `floor_ms` more AND `ratio` times as long.",
	"Config.volatile":    "Volatile paths applied to every chain.",
	"Config.redact":      "Paths blanked in every run record. An explicit list REPLACES the defaults: `" + strings.Join(config.DefaultRedact(), "`, `") + "`. Known secrets are also scrubbed by value.",

	"Target.base_url":      "Scheme and host of the backend. Redirects are never followed.",
	"Target.host_override": "Sent as the `Host` header and TLS `ServerName` while connecting to `base_url`.",
	"Target.headers":       "Headers added to every request. Never the auth header when `auth` is declared.",
	"Target.timeout":       "Per-request timeout, e.g. `30s`. Default 30s. A call with no answer in time was still sent.",
	"Target.build_header":  "Response header carrying the server's build, stamped into runs as `build`.",

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
	"Record.replay_of":     "On a `verify` replay, the safe spot's run id.",
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
	"StepRecord.status":            "`passed`, `failed`, `error` or `skipped`.",
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
	"StepRecord.first_attempt":     "A read's first answer when it was a server error; the re-send is judged.",
	"StepRecord.token_refused":     "Each token refused at this step: fingerprint, issued, stated expiry, refused.",
	"StepRecord.auth_profile":      "The profile whose token the step carried: `default`, a profile name, `invalid`, or `none`.",
	"StepRecord.auth_principal":    "Digest of the account the profile logged in as, no secret in it; `verify` compares it.",
	"StepRecord.volatile":          "Step-level volatile patterns.",
	"StepRecord.unordered":         "The unordered lists the run applied to this step.",
	"StepRecord.drift":             "Under `validate_output`, the response did not match its message.",

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
	b.WriteString("Generated from the code by `go run ./distill`; do not edit. Loaders reject unknown keys, so a key\n")
	b.WriteString("not here does not exist. `+` in `req`: the key is always written out.\n\n")

	b.WriteString("## 1. Chain file — `.shrt/chains/<name>.yaml`\n\n")
	table := func(heading string, v any) {
		b.WriteString(heading)
		writeTable(&b, v)
	}
	table("", chain.Chain{})
	table("\n### Step\n\n", chain.Step{})
	table("\n### Expectation — exactly one rule per entry\n\n", chain.Expectation{})

	b.WriteString("\n### What each rule actually does\n\n")
	rules, err := exerciseRules()
	if err != nil {
		return nil, err
	}
	b.WriteString(rules)

	b.WriteString("\n### Reserved `transport.*` paths — the call's transport outcome\n\n")
	b.WriteString(exerciseTransport())
	table("\n### `kept_red[]` — a known defect the chain pins\n\n", chain.Pin{})

	b.WriteString("\n## 2. References — `${...}`\n\n")
	b.WriteString("- Resolved in `body`, `headers`, and an expectation's `equals`, `not_equal`, `contains` and numeric bounds. Never in `path`.\n")
	b.WriteString("- A whole-value reference keeps its JSON type; inside a longer string it is text, and only a scalar may be interpolated.\n")
	b.WriteString("- One known not to resolve (a later or missing step, an unset `${env.*}`, an undeclared field) is a lint error.\n")
	b.WriteString("- No escape for a literal `${`: pass it through `${env.NAME}` or `-var`.\n")
	b.WriteString("- `${nowunix}` is a STRING of digits.\n\n")
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
	writeJSONTable(&b, runner.Record{})
	b.WriteString("\n### Each entry of `steps`\n\n")
	writeJSONTable(&b, runner.StepRecord{})
	b.WriteString("\n### Each entry of a step's `expect`\n\n")
	writeJSONTable(&b, chain.ExpectResult{})

	b.WriteString("\n## 6. Safe spot — `.shrt/safespots/<chain>.json`\n\n")
	b.WriteString("Written only by `shrt confirm <chain> -approve`. A proposal waits in `.shrt/safespots/pending/<chain>.json`.\n\n")
	writeJSONTable(&b, store.SafeSpot{})

	b.WriteString("\n## 7. What `shrt verify` actually compares\n\n")
	rep, err := exerciseDiff()
	if err != nil {
		return nil, err
	}
	b.WriteString(rep)

	b.WriteString("\n## 8. Closed vocabularies\n\n")
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

	b.WriteString("\n- `" + runner.StatusError + "`: no answer to judge, usually nothing sent. Read the step's `error` before blaming the backend.\n")
	b.WriteString("- `" + runner.StatusSkipped + "`: a dry-run step, or a `-keep-going` step held back behind one that did not pass.\n")
	b.WriteString("- `\"drift\": true` is `" + runner.StatusFailed + "`: answered, but not as its message under `validate_output`. Rebuild the descriptor first.\n")

	b.WriteString("\n## 9. Commands\n\n")
	cli, err := exerciseCLI()
	if err != nil {
		return nil, err
	}
	b.WriteString(cli)

	b.WriteString("\n## 10. Volatile and redact patterns\n\n")
	b.WriteString(exerciseMasks())
	b.WriteString("\n`*` globs within a segment; `**.` reaches any depth. Matching folds case and separators.\n")
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
	b.WriteString("\nTwo rules on one entry are a lint error: write one rule per entry.\n\n")
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
	b.WriteString("`exists` reads what the server SENT. Below, it sent `{\"error\": {...}}` and nothing else:\n\n")
	b.WriteString("| expectation | rule fired | passes |\n|---|---|---|\n")
	for _, c := range cases {
		r := c.e.EvaluateIn(full, present)
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", c.label, r.Rule, yesNo(r.Passed))
	}
	b.WriteString("\nA proto3 scalar cannot tell unset from zero: assert the value. For a field inside a message not sent,\n")
	b.WriteString("assert the message `exists: false`.\n")
	return b.String()
}

func exerciseTransport() string {
	var b strings.Builder
	b.WriteString("A `" + chain.TransportPrefix + ".*` path reads the transport result, not the body. On a Connect error (HTTP 4xx/5xx)\n")
	b.WriteString("it is the only assertion that runs; the rest are `unevaluated` and fail.\n\n")
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
	b.WriteString("\n- Besides `volatile` paths, verify masks a value id- or timestamp-shaped on both sides (same kind of id, same unit,\n")
	fmt.Fprintf(&b, "  within 400 days of its run) and a value echoing a fixture name or `${uuid}`; here %d value(s). `-masked` lists them.\n", rep.Masked)
	b.WriteString("- A value lost under a volatile pattern (null, empty, gone) is still reported.\n")
	b.WriteString("- Ids are renamed consistently across the record, so a stale id is reported.\n")
	b.WriteString("- An `unordered` list is compared as a multiset; a `redact` path never.\n")
	b.WriteString("- First it compares what each step SENT and the chain. A changed input that explains every response change is\n")
	b.WriteString("  `drift with different input`, a changed chain `drift after a chain change`; anything else is a `regression`.\n")
	fmt.Fprintf(&b, "\nChange kinds `shrt verify` and `shrt diff` print: `%s` (a path the baseline had is gone), `%s`\n", diff.KindMissing, diff.KindUnexpected)
	fmt.Fprintf(&b, "(a path the baseline did not have), `%s` (same JSON type, different value), `%s` (different JSON\n", diff.KindChanged, diff.KindType)
	fmt.Fprintf(&b, "type), `%s` (a list or the step count has a different number of items), `%s` (a step id or rpc\n", diff.KindLength, diff.KindOrder)
	fmt.Fprintf(&b, "differs at that position), `%s` (the step's pass/fail status changed).\n", diff.KindStatus)
	b.WriteString("Steps pair by id, then by call and position: a renamed or inserted step is one change. A field respelt in\n")
	b.WriteString("case, as its JSON name, or as `${steps.a.response.x}` for `${a.x}` is no change.\n")
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
	var names []string
	for _, line := range strings.Split(root, "\n") {
		if m := commandLine.FindStringSubmatch(line); m != nil {
			names = append(names, "`"+m[1]+"`")
		}
	}
	var b strings.Builder
	b.WriteString("| command | subcommands |\n|---|---|\n")
	if len(names) > 0 {
		fmt.Fprintf(&b, "| `shrt` | %s |\n", strings.Join(names, ", "))
	}
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
			usage := strings.ReplaceAll(strings.TrimSpace(line[at+len(marker):]), "|", "\\|")
			fmt.Fprintf(&b, "| `shrt %s` | `%s` |\n", group, usage)
			break
		}
	}
	b.WriteString("\nEvery command prints its flags and exit codes with `-h`. A run id is accepted with or without `.json`.\n")
	return b.String(), nil
}

var goRunExit = regexp.MustCompile(`(?m)^exit status \d+\n?`)

var commandLine = regexp.MustCompile(`^  ([a-z]+) {2,}\S`)

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
