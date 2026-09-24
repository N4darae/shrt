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
	"Chain.name":        "Names the run directory and the safe spot. Defaults to the file name.",
	"Chain.description": "What state this chain reproduces, for the next reader.",
	"Chain.vars":        "Referenced as `${vars.x}`. Override per run with `-var x=y`. A chain that reads `${vars.x}` without declaring it here must be given `-var x=...`: `shrt run`, `run -dry-run` and `verify` refuse it before sending anything, naming each missing var.",
	"Chain.volatile":    "Response paths masked when `shrt verify` diffs against the safe spot and when `shrt diff` compares two runs. Expectations still see the real value.",
	"Chain.redact":      "Paths blanked in the run record: in each step's request and response, in `vars` and exports, and in each expectation's `want` and `got`. A value exported from a redacted path is also scrubbed wherever else it appears (see `redact` in §2). A redacted response value is blanked in the safe spot and in every replay alike, so `shrt verify` and `shrt diff` never compare it: `confirm` lists those fields under **Redacted, never compared by `shrt verify`**, and `verify` counts them and names each one (`redacted`, `redacted_paths` under `-json`) without failing. Redact only what must not be stored; a business field redacted here is a field no safe spot guards, so assert it in the chain if it matters.",
	"Chain.steps":       "Ordered. Never reordered or parallelised, and never skipped except as `-keep-going` records it.",
	"Chain.kept_red":    "Pins a known defect this chain is kept red on purpose to show: WHERE it fails (a step) and HOW (an expectation path on that step, optionally the value it got). `shrt run` then exits 0 when the chain fails exactly as pinned — every pinned expectation failed, with the pinned `got` when one is given, and no other step or expectation failed — and exits 1 when it fails anywhere else or differently (a regression in an earlier step, a pinned path that held, another value), or passes, which says the defect is gone. An `error` run still exits 3. The run record keeps `status: failed` and says which in `kept_red`, so such a run is never proposed as a safe spot. Each entry must name a step of this chain and a path one of its expectations asserts, or the chain does not load. A CI gate needs no list of red chains beside it: every `shrt run` must exit 0.",

	"Step.id":          "Unique; later steps reference it. Derived from the rpc name when omitted.",
	"Step.description": "Why this step is here and what its assertions mean — the place to record a judgement a reader would otherwise re-derive from the body.",
	"Step.call":        "`package.Service/Rpc`, `Service/Rpc`, or a bare `Rpc` when unambiguous.",
	"Step.body":        "Validated against the proto request message before anything is sent.",
	"Step.headers":     "Per-step header overrides. Not the auth header: when the config declares auth, a hand-written `Authorization` (or the covering profile's `header`) on a step an auth profile covers, or with `skip_auth`, is a lint error, and `shrt run` refuses the chain before sending anything, since the header would be overwritten by the profile's token, or would pin one principal into one step. Name the principal with `auth: <profile>`.",
	"Step.expect":      "Assertions on this step's response. Each entry needs exactly one rule, and nearly always a `path`: an entry with no `path` tests the whole response.",
	"Step.export":      "`name: response.path`. Publishes `${exports.name}` and the bare `${name}`. A name equal to a step id is a lint error, since the bare `${name}` would then mean two things; a name another step also exports is a lint warning (`export-overwritten`, failed by `-strict`), since the later write silently replaces the earlier.",
	"Step.auth":        "Named auth profile from `.shrt/config.yaml`. Contradicts `skip_auth`; lint rejects both. The reserved value `invalid` sends a token the backend never issued, in the header and scheme of the profile that would otherwise cover the call, and never re-logs in on the 401 — the probe for \"an invalid token is refused\". Lint rejects it with `export`; `shrt run` refuses it when the config declares no auth.",
	"Step.skip_auth":   "Attach no auth header. For the probe \"a missing token is refused\". A login step does not need it: a call to a configured login rpc (or one listed in `auth.skip_calls`) never carries a token, and its run record says `auth_profile: none`.",
	"Step.allow_fail":  "Tolerate the backend REFUSING this call, when a later step depends on the attempt rather than the outcome: a step the backend refused at the transport level (a Connect error) does not stop the chain, provided it declares no expectation — a refusal leaves every expectation unevaluated, and an unevaluated expectation is never tolerated. An in-band refusal whose expectations hold is simply `passed` and needs no key. It covers nothing else — not a failed expectation, not an expectation a transport error left unevaluated, not drift, not a step whose status is `error` — and every one of those still stops the run. One more case slips through: a step answered 200 whose `export` path is missing is `failed` with no failed expectation, and `allow_fail` lets the chain go on past it. To see what lies beyond a step that does not pass, use `shrt run -keep-going`, not this key. A step MEANT to be refused at the transport level asserts which refusal with `transport.*` and needs no key. On a step that declares any expectation it does nothing, and `chain lint` warns `inert-allow-fail`, which `-strict` fails.",
	"Step.volatile":    "Volatile paths for this step only, added to the chain's.",

	"Expectation.path":      "JSON path into this step's own response, or one of the reserved `transport.*` paths (table below), which read the recorded transport result instead. `${...}` here is a lint ERROR: a path names a location, not a value. So is a path the response message has no field for, under every rule — `exists: false` included, since it could not fail. Field names match with case and separators folded (`qtyOnHand` reads `qty_on_hand`), and a path that matches only that way is a lint warning (`inexact-path`) naming the exact field; an `export` path is checked the same way.",
	"Expectation.equals":    "Compared as text, so `1` matches `\"1\"`. May carry `${...}`: an earlier step, or this step's own request (`${steps.<this>.request.<field>}`); this step's own response is a lint error. No arithmetic is done: `${a.qty}+${b.qty}` is the text `0+5`, and against a numeric field `chain lint` warns `interpolated-arithmetic`, which `-strict` fails.",
	"Expectation.not_equal": "The path must be present AND differ. An absent path FAILS it, with `path not present in response` — use `exists: false` when absence is what you mean. May carry `${...}`.",
	"Expectation.contains":  "Substring of the value's text. May carry `${...}`.",
	"Expectation.exists":    "Whether the server SENT the path. Read against the populated fields of the response, not the stored record, which materialises every declared field at its zero value. See the second table in §1.",
	"Expectation.not_empty": "Present and not `\"\"`, `0`, `false`, `[]` or `{}`. `0` means zero of every numeric type, including an int64 or uint64, which the record stores as the string `\"0\"`.",

	"Overlay.apiVersion":  "`shrt/contract/v1`.",
	"Overlay.domain":      "Defaults to the file name.",
	"Overlay.description": "Domain-wide prose: the invariants and call order every rpc here shares.",
	"Overlay.failures":    "Inherited by every rpc in the domain. Envelope-wide codes (authn, authz) belong here, stated once.",
	"Overlay.rpcs":        "Keyed by the fully qualified `package.Service/Rpc`.",

	"RPCContract.summary":       "What it does and when you would call it.",
	"RPCContract.note":          "Free text about the rpc as a whole. It has NO mechanical effect on the score — it used to spare the no-producer charge, and no longer does, because an exemption any sentence can buy measures nothing. To declare that a read has no producer, use `no_producer`.",
	"RPCContract.auth":          "Named auth profile this rpc needs when the default principal is the wrong one. `plan` writes it onto the step.",
	"RPCContract.requires_role": "Roles the caller must hold. A precondition, not a failure. The literal `NONE`, alone, declares that this rpc reaches no role gate; leaving the key out entirely is scored as an omission.",
	"RPCContract.no_producer":   "Why no write rpc in this API puts the rows this read returns there — a seed, a migration, an external feed. It is the ONLY thing that spares the read-with-no-producer charge; a general-purpose `note` deliberately does not, because an exemption anyone can buy with one sentence measures nothing. Meaningless on a write rpc.",
	"RPCContract.required":      "Fields the server actually rejects without. proto3 has no `required`, so this comes from reading the backend. Applies to reads as well as writes. The literal `NONE`, alone, declares that the server rejects nothing; leaving the list empty on an rpc that takes request fields is scored as an omission, so silence and `NONE` cannot look the same. The literal `UNKNOWN`, alone, is the honest answer when the handler could not be found — it lints as a warning rather than an error, and is scored exactly as an empty list is, because not knowing must not be cheaper than knowing. A scaffold writes `['TODO: …']` here as a value, not a comment; that entry is not a field name, and it is read as an empty, unfilled `required` that lint warns on.",
	"RPCContract.needs":         "An rpc that must run first but whose output no field consumes.",
	"RPCContract.before":        "The inverse of `needs`, declarable from the side that owns the prerequisite. Takes rpc names only and always pulls in the unaliased node; to order an alias, list it in the dependent rpc's `needs:`.",
	"RPCContract.fields":        "Per request field. Dotted keys reach into nested messages. After a repeated field an optional index picks one entry: `lines.1.id_product`. An unindexed key (`lines.qty`) applies to every entry, and an indexed key overrides it for that entry. `plan`, `contract show` and `chain new` build one entry per index up to the highest declared (at most 100). Lint errors on an index after a non-repeated field, a leading zero, or an index of 100 or more, and warns when entries below the highest index are declared by nothing.",
	"RPCContract.aliases":       "Per-instance overrides, so two aliased steps of one rpc differ.",
	"RPCContract.exports":       "Response paths worth exporting, and why.",
	"RPCContract.terminal":      "Response fields that deliberately have no consumer. Records the dead end instead of deleting it.",
	"RPCContract.soft_signals":  "Response fields carrying advisory information rather than success or failure.",
	"RPCContract.failures":      "One entry per way this rpc refuses.",
	"RPCContract.source":        "Files read to determine all this, so a reviewer can re-check it. Strip any `:line-range` before resolving; ranges rot, paths do not.",
	"RPCContract.status":        "`draft`, or `verified` with a `verified_run`. An agent leaves it `draft`.",
	"RPCContract.verified_by":   "The person who verified it.",
	"RPCContract.verified_run":  "The run id that proved it.",

	"FieldContract.from":       "`<rpc>[@alias]->response_path`. This value comes from an earlier call, and declares the ordering edge.",
	"FieldContract.value":      "A fixed literal or template, e.g. `${uuid}`.",
	"FieldContract.same_as":    "`<rpc>[@alias]->request_path`. Must equal what an earlier call SENT. Mutually exclusive with `from`. `plan` keeps a producer value that is a single stable reference on both sites, points the consumer at `${steps.<producer>.request.<path>}` when the producer's value is a template (`sku-${vars.tag}`, `${uuid}`), and otherwise rewrites both sites onto one generated var.",
	"FieldContract.oneof":      "Mutual-exclusion group; at most one member may carry a value. The member that carries one is also the member `contract show` and `chain new` scaffold, in place of the proto group's first field.",
	"FieldContract.checked_by": "How the server validates the id, which decides whether a bad one is a named domain failure or an unnamed 500.",
	"FieldContract.note":       "Units, formats, constraints. Prose only: it never changes `plan` output, and it does not mark a scaffold zero as deliberate; `value: \"0\"` does.",

	"AliasContract.note":   "What makes this instance different.",
	"AliasContract.fields": "Field overrides for this instance only.",

	"Failure.code":           "The app code in the error envelope. Optional: a shape error raised before the business logic has only a connect code.",
	"Failure.connect_code":   "The Connect code, e.g. `invalid_argument`.",
	"Failure.reason":         "The backend's own reason string, verbatim. Must match the `errmsg.New` site.",
	"Failure.message":        "The message text, when it is worth pinning.",
	"Failure.field":          "The request field at fault, for shape errors.",
	"Failure.when":           "The condition that raises it. This is the one an author most often leaves vague.",
	"Failure.unreachable":    "Declared but cannot fire, and why. Keeps it out of the coverage denominator without deleting the knowledge. The commonest honest use is a code NO shrt chain can observe: every request is validated against the descriptor before it is sent, so a refusal reachable only by a body the proto cannot express — a string where a message belongs, an enum value the descriptor does not know, a catch-all handler for a decode error — is out of reach for a descriptor-driven client and always will be. Say that here rather than leaving the code undeclared, or the coverage term charges you for a branch nothing can reach.",
	"Failure.pending_deploy": "Declared, correct, and not yet on the box. Carries the commit that will make it reachable.",

	"Config.target":      "Where chains run.",
	"Config.descriptor":  "Where the proto descriptor lives and what rebuilds it.",
	"Config.auth":        "The login call, declared once for the whole repo.",
	"Config.paths":       "Where chains, runs and safe spots live.",
	"Config.conventions": "Naming and envelope conventions of THIS backend. Every key optional. The envelope defaults are what shrt assumed before the block existed; the read-name default is wider than the five prefixes that used to be hard-coded.",
	"Config.volatile":    "Volatile paths applied to every chain.",
	"Config.redact":      "Paths blanked in every run record, and so never compared by `shrt verify` (see `redact` in §1). Credentials belong here. Without the key the defaults apply, and `shrt init` writes them out: `" + strings.Join(config.DefaultRedact(), "`, `") + "`. A bool is never masked, and neither is an empty value (`\"\"`, 0 of any numeric type — an int64's `\"0\"` included —, null, `[]`, `{}`): masking it would hide that nothing was sent. Paths are not the only guard: the runner also scrubs by VALUE, replacing with `<redacted>`, wherever it appears in the record (request, response, each expectation's `want`, `got` and detail, errors, warnings, notes, exports), every auth body value in a field these patterns cover (the password, not the username or another login field they do not cover), every `${env.*}` value a step body reads into a field these patterns cover (an in-chain login's password), every token a login returned (a profile's login, or a step calling a login rpc, its `token_path` and every value its response holds under these patterns, however a later step reads it), and every value a step exports from a redacted path, so the proposal report and the safe spot built from the record never carry them either. A value shorter than 4 characters is replaced only where it is the whole string. An explicit list REPLACES the defaults rather than adding to them, so a config written before a default was added does not get it — add the pattern by hand.",

	"Target.base_url":      "Scheme and host of the backend. Redirects are never followed: a 3xx answer, to the login call or any other, is a transport error naming its `Location`, so no request body, credential or token is re-sent elsewhere. Point `base_url` at the final address.",
	"Target.host_override": "Send this as the `Host` header and the TLS `ServerName`, while connecting to `base_url`'s address. For reaching a vhost by IP without disabling verification.",
	"Target.headers":       "Headers added to every request. Not the auth header: when the config declares `auth`, an `Authorization` here (or any profile's `header`) is an error in `chain lint` and `shrt doctor`, and `shrt run` refuses to send anything, because it would ride on every call no profile covers (a login, `skip_auth`, `auth.skip_calls`) while the run record says `auth_profile: none`, and be overwritten on every call a profile covers. Name the principal with an auth profile instead.",
	"Target.timeout":       "Per-request timeout, e.g. `30s`. Defaults to 30s.",
	"Target.build_header":  "A response header in which the server reports its own build or version, e.g. `X-Server-Version`. Its value is stamped into each run record as `build`, and a value that changes mid-run is recorded as `old -> new` with a warning on the step that first saw it. `shrt run -build <label>` overrides it, and a label the header contradicts is warned about. Unset, a record says only which `base_url` answered, not which build.",

	"Auth.call":           "The login rpc.",
	"Auth.body":           "Its request body. `${env.X}` belongs here, never a literal credential. Resolved before any step runs, so only `${env.*}`, `${uuid}` and the clock forms work; `doctor` and `chain lint` reject `${vars.*}`, exports and step references. When a step of a chain runs under this profile and one of its `${env.*}` is unset, `shrt run` refuses the chain before sending anything and `chain lint` warns, since the login would fail after earlier steps had run.",
	"Auth.token_path":     "Response path holding the token.",
	"Auth.expires_path":   "Response path holding the expiry. Without it the token is refreshed only on a 401, and a 401 re-sends a read or a call whose cached token no call in the run had used yet (see `auth_retry` in §5): a write answered 401 with a token already accepted in the run is not re-sent, and its step is `error`.",
	"Auth.header":         "Defaults to `Authorization`.",
	"Auth.scheme":         "Defaults to `Bearer`.",
	"Auth.skip_calls":     "Calls that carry no token. Read only at the top level of `auth:`, where it applies to every profile; inside an entry of `auth.profiles` it is accepted and ignored.",
	"Auth.leeway_seconds": "Re-login this many seconds before expiry. Defaults to 60.",
	"Auth.calls":          "Glob patterns this profile owns, e.g. `acme.partner.*`.",
	"Auth.profiles":       "Named additional principals. Each holds its own token cache.",

	"Descriptor.file":   "The compiled `FileDescriptorSet`.",
	"Descriptor.source": "What `shrt catalog build` compiles, passed to the descriptor binary.",
	"Descriptor.binary": "The compiler, normally `buf`.",

	"Record.run_id":        "Timestamp-prefixed, e.g. `20260911T104434Z-e94560bd`. `-run latest` picks the newest by that prefix; runs started in the same second are ordered by `started_at`, then by file time.",
	"Record.chain":         "Chain name, which is also the run directory and the safe-spot key.",
	"Record.chain_source":  "Path the chain was loaded from.",
	"Record.dry_run":       "True when `-dry-run` produced this record: every step resolved and validated, none was sent. `status` is still `passed` on success, so a gate that reads only `status` cannot tell a dry run from a real one — read this field too. Dry runs are never saved, so it is absent from every record under `.shrt/runs/`.",
	"Record.target":        "The base_url this ran against. A receipt quoted without it says nothing about which box answered.",
	"Record.build":         "Which build of the target answered: the `shrt run -build` label, else the value of `target.build_header`. Absent when neither is set, and then two builds behind one `target` are indistinguishable — do not compare such records across a deploy.",
	"Record.started_at":    "UTC start time.",
	"Record.duration_ms":   "Whole-run wall time.",
	"Record.status":        "`passed`, `failed` or `error` — never `skipped`; that is a step status.",
	"Record.vars":          "The resolved vars this run used, so a replay can be reproduced. A var whose name a `redact` pattern covers is `<redacted>`, and so is the value of any var a step body reads into a field `redact` covers (`password: ${vars.pw}` with `-var pw=...`): that value is scrubbed by value from the whole record, so `shrt confirm` cannot show it either.",
	"Record.exports":       "Everything any step exported.",
	"Record.volatile":      "Volatile patterns in force for the whole run, chain plus config. Step-level patterns are on each step record.",
	"Record.redacted":      "Redact patterns in force. The values themselves are already masked in `request`/`response`.",
	"Record.steps":         "One entry per step, in order.",
	"Record.failure":       "Why the run stopped, when it did. Under `-keep-going`, one line per step that did not pass; when a step could not connect to the target at all (connection refused, a dial or DNS failure — not a Connect error), every later step is recorded `skipped` unsent and this carries ONE line naming the unreachable target, instead of one per step.",
	"Record.warning":       "A run-level warning. Today: every response carrying `conventions.envelope_path` had a value other than `conventions.envelope_ok` (refusals a step asserted with `equals`, and steps asserting `transport.*`, aside). When the values seen do not look like verdict codes it points at `envelope_path`; when some look like codes that are not refusals it points at `envelope_ok`; when all look like refusals (REJECTED, PERMISSION_DENIED, ...) it stays silent, since that is a refused principal, not a config problem. It names the values seen, quoting any that are not code-shaped.",
	"Record.seal":          "A checksum of this record that shrt writes with it. `shrt confirm` refuses to propose, and `-approve` to approve, a record whose content no longer matches it, or that has none (written before seals existed, or with the seal removed): run the chain again and propose the new run. It detects a hand edit, such as a failed step flipped to `passed`; it is not a signature, and cannot stop someone who recomputes it.",
	"Record.keep_going":    "True when `shrt run -keep-going` produced this record: steps after a failure were still run, so a later red may be a consequence of an earlier one.",
	"Record.kept_red":      "Set only for a chain with `kept_red` that was answered: `as_pinned` (it failed exactly as pinned; `shrt run` exits 0), `not_as_pinned` (it failed elsewhere or differently; exit 1) or `defect_gone` (it passed; exit 1). Absent on a dry run and on an `error` run.",
	"Record.kept_red_note": "What `kept_red` found: the pins, and for `not_as_pinned` each step or expectation that failed where nothing is pinned, each pinned path that held, and each pinned `got` that differed.",
	"Record.failed_steps":  "Under `-keep-going`, the id of every step that did not pass, in order — failed, error, and skipped behind one of those. `status` is the FIRST such step's status, the same verdict the run would have had without the flag.",

	"StepRecord.index":           "Position in the chain, from 1.",
	"StepRecord.id":              "The step id.",
	"StepRecord.call":            "As written in the chain.",
	"StepRecord.procedure":       "The resolved `/package.Service/Rpc`.",
	"StepRecord.status":          "`passed`, `failed`, `error`, or `skipped` — every step of a `-dry-run` that resolves and validates is `skipped`, and so is a `-keep-going` step that was not sent because it reads the response (`${steps.X…}`, `${X.…}`) or an export of a step X that did not pass; a reference to X's request does not hold it back, nor does a reference to a response field of an answered X whose failed expectations do not cover that field. Its `error` says what happened to X: a failed assertion, a refusal, or an error. A `-keep-going` step after one that could not connect to the target at all is `skipped` too, its `error` naming the unreachable target.",
	"StepRecord.http_status":     "Transport status. 200 with a non-OK `error.code` in the body is the normal shape of a business refusal.",
	"StepRecord.latency_ms":      "Per-step wall time.",
	"StepRecord.request":         "What was sent, AFTER reference resolution and redaction.",
	"StepRecord.response":        "What came back, RE-ENCODED through the response message and then redacted — not the wire bytes. Field names are the proto ones, every declared scalar and list field is present at its zero value if the server omitted it (an unset nested message is `null`, so assert `exists: false` on the message itself rather than on a path inside it), and an int64 is a JSON string whatever the server sent. That is what gives `shrt verify` a stable shape to diff across runs, and it is why a scalar's absence cannot be read out of this field: see the second table in §1 on `exists`. When the descriptor cannot decode the body it is stored as sent instead, and the step carries a `warning` saying so, so the shape of this field depends on descriptor freshness. A body kept as sent whose envelope field is not an object (`\"status\": \"SUCCESS\"`) has no verdict at the envelope path, and is judged as an absent verdict (see `expect`). A body that repeats a key (`status` twice, at any depth) fails the step without evaluating its expectations, since decoders disagree on which value counts; this field then holds the last value and `error` names the repeated key.",
	"StepRecord.transport_error": "Set when the backend answered with a Connect error (any non-200) instead of a response message. `transport.code` and `transport.message` read it.",
	"StepRecord.expect":          "One entry per expectation, with the rule that fired and whether it held. Read this, not just the step status. The runner may append entries of its own: `item_envelope` for a batch line refused unannounced, and `envelope` for a step that declares expectations, was refused in-band (the envelope code is not `envelope_ok`), or answered with no verdict (the envelope absent or empty, though the response message declares it), and has no expectation pinning the verdict (an `equals` on the envelope path itself, or a rule on the envelope path, one of its parents, or a `transport.*` path that would fail on a successful answer; path segments match case-insensitively, so `Status.Code` is `status.code`. A rule on a sibling such as `status.message` or `status.details.0.reason` pins nothing, whatever it asserts; `not_equal: \"\"` or `not_equal` a misspelt code holds on the refusal and on the ok value alike and pins nothing; and on an absent or empty verdict no `not_equal` pins it, since `not_equal: SUCCESS` holds on `\"\"` too) — that step is `failed`, because the assertions that held read the zero values a refusal leaves.",
	"StepRecord.exported":        "What this step published.",
	"StepRecord.error":           "Why this step failed or could not run.",
	"StepRecord.warning":         "Non-fatal note from the runner: a stale descriptor, a build change mid-run, or a step that declares no expect but was refused in-band (the envelope code is not `envelope_ok`) or answered with no verdict, which stays `passed` with a warning saying so.",
	"StepRecord.note":            "Runner commentary, e.g. that a login seeded a profile's token — or did NOT, because it sent other credentials than that profile's `body`. A login seeds a profile only when its request equals that profile's resolved body, so a chain that logs in as someone else never changes whose token later steps carry.",
	"StepRecord.auth_retry":      "Set when the call was answered unauthenticated (HTTP 401, or `unauthenticated` at the envelope path) and the token dropped. `resent`: a fresh login was made and the call sent again, because it is a read (`conventions.read_only_prefixes`), or because its token came from the on-disk cache and no call in this run had used it yet, so the backend refused it at authentication (a restart, a revoke) and did not perform it; this record is the second answer. `not_resent`: a write refused with a token the backend already accepted in this run was NOT sent again, because the backend may already have performed it and a second send could perform it twice; the next call logs in fresh. Both carry a `warning`. A step still refused authentication is `error`, not `failed`, unless it has `allow_fail`.",
	"StepRecord.auth_profile":    "The auth profile whose token this step carried: `default`, a name under `auth.profiles`, `invalid` for a step with `auth: invalid` (a token the backend never issued), or `none` when no token was attached (`skip_auth`, a login rpc, `auth.skip_calls`). Absent when the config declares no `auth:` block and in a dry run. The profile NAME is not the principal: which account the profile logged in as is `auth_principal`. Two steps that should act as one principal and show different values here are a principal swap. `shrt verify` compares it with the safe spot's value for the same step as part of the input (§7): a step that now runs under another profile is reported as `request differs ... auth_profile (default -> clerk)` and fails verify with `drift with different input`, even when every response matches. A record that does not say which profile ran (no `auth:` block, or recorded before this field) is not compared.",
	"StepRecord.auth_principal":  "Which principal the step's profile logged in as: a digest of the profile's login call and the resolved login-body fields `redact` does not cover (the username, not the password), so no secret is in it and a rotated password keeps it. Absent when no token was attached or the login body could not be resolved. `shrt verify` compares it like `auth_profile`: the same profile name logging in as another account (`API_USER=clerk` behind `default`) is reported as `request differs ... auth_principal` and fails verify with `drift with different input`. A record without it is not compared.",
	"StepRecord.volatile":        "Step-level volatile patterns.",
	"StepRecord.drift":           "The response did not match its proto message while `conventions.validate_output` was on. The step is `failed`, not `error`: the request was sent and answered. No expectation was evaluated, so nothing in this step is evidence about the rpc — rebuild the descriptor first. `allow_fail` does not swallow a step carrying it.",

	"Pin.step": "The step the defect shows at.",
	"Pin.path": "An expectation path of that step that must fail. List one entry per failing expectation; an expectation of the step on any other path must hold.",
	"Pin.got":  "The value the failed expectation must have got, compared as text. Omit it to pin only where the chain fails.",

	"ExpectResult.path":   "The path asserted.",
	"ExpectResult.rule":   "Which rule actually fired — the one to read when two rules were written.",
	"ExpectResult.want":   "The comparison value, after `${...}` resolution.",
	"ExpectResult.got":    "What was actually there.",
	"ExpectResult.passed": "Whether it held.",
	"ExpectResult.detail": "Why not, when the rule itself was malformed.",

	"SafeSpot.chain":        "Which chain this is the ground truth for.",
	"SafeSpot.run_id":       "The run a person approved.",
	"SafeSpot.target":       "Where that run happened.",
	"SafeSpot.build":        "The confirmed run's `build`, when it had one. `shrt verify` prints it beside the replay's.",
	"SafeSpot.confirmed_by": "The email of the user who said yes. `shrt confirm -approve` refuses a `-by` that is not an email address.",
	"SafeSpot.proposed_by":  "Who proposed the run with `shrt confirm -note`; `agent` unless `-by` named someone.",
	"SafeSpot.proposed_at":  "When it was proposed.",
	"SafeSpot.confirmed_at": "When.",
	"SafeSpot.note":         "What makes this run correct: the approver's `-note`, else the proposer's.",
	"SafeSpot.supersedes":   "The run id this replaced, when proposed with `-supersede`.",
	"SafeSpot.volatile":     "The volatile patterns approved with the run (config and chain), masked before comparison together with each step's own `volatile`. A replay masked with any other pattern, one added to the config or chain after approval, fails `shrt verify`, which names each such pattern and every value it hid, until a run under the wider mask is proposed with `-supersede` and approved.",
	"SafeSpot.digest":       "Fingerprint of the chain, run id, target, build, volatile patterns and full step records, and of the approval: `confirmed_by`, `confirmed_at`, `note`, `proposed_by`, `proposed_at` and `supersedes`. `shrt verify` refuses a safe spot whose content no longer matches it (a hand edit, including of who approved it): restore the file or re-approve with `-supersede`. Safe spots of an older kind are still checked on what their digest covers, and `shrt verify` says so: one approved before the approval was covered has a digest of the content only, so an edit of its approver is not caught until it is re-approved; one approved before 2026-09-24 carries the digest of step ids, calls and responses.",
	"SafeSpot.steps":        "The confirmed step records, which a replay is diffed against.",

	"Conventions.read_only_prefixes": "Rpc-name prefixes that mean a call only reads. Decides which scaffold an rpc gets, whether it can produce an id for another rpc, and three quality terms. Default: Fetch, Get, List, Preview, Search, Read, Query, Find, Lookup, Describe, Show, Count, Export, Download, Retrieve.",
	"Conventions.envelope_path":      "JSON path at which a response reports its own verdict. Default `error.code`. Set it to MOVE the envelope, never to remove it: an explicit empty value is indistinguishable from an absent key and falls back to the default. A backend with no in-body envelope needs no setting — a response carrying no field of that name gets a scaffolded assertion on a real response field instead. A path set here that no response message in the descriptor declares fails `shrt run` before any traffic is sent, as `shrt doctor` fails it.",
	"Conventions.envelope_ok":        "The `envelope_path` value that means success. Default `OK`. A run in which responses carried the envelope but none carried this value ends with a `warning` naming the values seen; a batch whose items \"refuse\" with the very value the top-level envelope carries says to check this key.",
	"Conventions.item_envelope_path": "Per-item verdict in a BATCH response, as `<list>[].<path>` (e.g. `results[].error.code`). A batch rpc can answer `OK` at the top level while refusing every line; without this the runner cannot see that, and a step asserting only the envelope passes having achieved nothing. Unset means the backend has no per-item envelope. Checked only on rpcs whose response message declares that list with that field, so a list of atomic receipts carrying no verdict is left alone. An item whose verdict is missing (its envelope unset or absent) is success when no item of that batch carries `envelope_ok` explicitly — a backend that writes an item's error only on refusal — and is reported like a refused item, as `(no verdict)`, when another item of the same batch does. A sibling that is refused does not count here, unlike the top-level rule: a refused line is exactly what a backend that writes errors only on refusal sends next to its unset successes, so it cannot tell a stripped verdict from a success; only an explicit `envelope_ok` shows that this backend writes a verdict for a success. An item whose envelope is present with an empty code (`status: {code: \"\"}`) did write a verdict and left it blank, as an empty top-level code does, and is reported as `(no verdict)` whenever another item of the batch carries a non-empty code, refused or ok; a refusal the step pins with `equals`, `not_equal` or `contains` on that line's verdict path, or on one of that line's code fields (`conventions.code_fields`), is declared, not reported, while `exists` and `not_empty` declare nothing; a path no response message declares fails `shrt run` before any traffic is sent.",
	"Conventions.code_fields":        "Detail-field names that carry a backend's OWN numeric or symbolic code, searched by `shrt chain which -code`. Default `app_code`, `reason`, `error_code`. An explicit list REPLACES the defaults. The envelope's own leaf is not listed here — it follows `envelope_path`, so a deployment answering at `status.code` is searched there without any setting. Nothing enforces these names; a code this list cannot reach makes `chain which` answer \"no chain asserts it\" for a corpus that does.",
	"Conventions.validate_output":    "When true, a response that does not match its proto message FAILS the step. Default false: the response is kept as sent and a warning is recorded, so a descriptor that has drifted from the deployed binary degrades quietly rather than failing every chain. Turn it on once your descriptor build and your deploy are in step.",

	"Paths.chains":    "Chain YAML directory.",
	"Paths.contracts": "Curated contract overlay directory, one file per domain. Read it from here rather than assuming `.shrt/contracts`. Only the `.yaml`/`.yml` files at its top level are loaded, each holding one YAML document; `shrt doctor` warns about overlay files in a subdirectory, and an rpc defined in two files is an error naming both, since one would silently replace the other.",
	"Paths.runs":      "Run record directory.",
	"Paths.safespots": "Safe spot directory.",
}

type row struct{ key, typ, req, note string }

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

func render() ([]byte, error) {
	var b strings.Builder
	b.WriteString("# GRAMMAR — every key shrt accepts\n\n")
	b.WriteString("**Generated from the Go structs by `go run ./distill`. Do not edit.**\n")
	b.WriteString("`go run ./distill -check` fails when a key added in code is missing here. Nothing in the module runs it for you, so run it before you commit a change to a key.\n\n")
	b.WriteString("Why this file exists: the loaders reject unknown keys (`KnownFields(true)`), but only since\n")
	b.WriteString("2026-09-11. Before that a misspelled key was silently dropped and `lint` still said `ok` —\n")
	b.WriteString("`one_of:` instead of a real rule meant a step asserted nothing while reading as checked.\n")
	b.WriteString("A key not in this file does not exist; the loader names it with its line and the nearest real key:\n")
	b.WriteString("`unknown key \"expects\" at line 16 (did you mean \"expect\"?)`.\n\n")
	b.WriteString("A `+` in the `req` column means the key has no `omitempty`: it is always written out, and a\n")
	b.WriteString("scaffold leaves it present-but-empty rather than absent.\n\n")

	b.WriteString("## 1. Chain file — `.shrt/chains/<name>.yaml`\n\n")
	writeTable(&b, "Chain", reflect.TypeOf(chain.Chain{}))
	b.WriteString("\n### Step\n\n")
	writeTable(&b, "Step", reflect.TypeOf(chain.Step{}))
	b.WriteString("\n### Expectation — exactly one rule per entry (lint-enforced since 2026-09-11)\n\n")
	writeTable(&b, "Expectation", reflect.TypeOf(chain.Expectation{}))

	b.WriteString("\n### What each rule actually does\n\n")
	b.WriteString("Produced by evaluating every rule against a fixture response, not by description:\n\n")
	rules, err := exerciseRules()
	if err != nil {
		return nil, err
	}
	b.WriteString(rules)

	b.WriteString("\n### Reserved `transport.*` paths — the call's transport outcome\n\n")
	b.WriteString(exerciseTransport())
	b.WriteString("\n### `kept_red[]` — a known defect the chain pins\n\n")
	writeTable(&b, "Pin", reflect.TypeOf(chain.Pin{}))

	b.WriteString("\n## 2. References — `${...}`\n\n")
	b.WriteString("Resolved in `body`, in `headers`, and in an expectation's `equals` / `not_equal` / `contains`.\n")
	b.WriteString("**Not** in an expectation's `path`.\n\n")
	b.WriteString("A reference that is the whole value keeps its JSON type; inside a longer string it is\n")
	b.WriteString("interpolated as text. A reference that cannot resolve fails the step — it never becomes empty.\n")
	b.WriteString("One that is known not to resolve is refused before anything is sent, by `shrt run` and as a lint\n")
	b.WriteString("error: a step or export that does not exist or runs later, an unset `${env.*}`, and a field of an\n")
	b.WriteString("earlier step's response that its response message does not declare (`${create_product.product.id_prodct}`,\n")
	b.WriteString("lint kind `unproducible-reference`). If the field is real and new, the descriptor is stale: `shrt catalog build`.\n\n")
	b.WriteString("There is no escape for a literal `${`: `$${x}` is a `$` followed by the resolved `${x}`. A value that\n")
	b.WriteString("must carry `${` comes in through `${env.NAME}` or `-var name=...`, whose values are never resolved again.\n")
	b.WriteString("Lint warns (`reference-syntax`) on forms that are accepted but do not do what they read as: spaces\n")
	b.WriteString("inside the braces (`${ uuid }` resolves as `${uuid}`), a fractional clock offset (`${now+1.5}` resolves\n")
	b.WriteString("as `${now+1}`), a path after `uuid` or a clock form (ignored), and a `${` never closed, which is sent as\n")
	b.WriteString("literal text.\n\n")
	b.WriteString("The `resolves to` column below is quoted where the value is a Go string, so the rows that\n")
	b.WriteString("look numeric but are not stand out: `${nowunix}` resolves to a STRING of digits, never an\n")
	b.WriteString("integer, so an expectation comparing it to a number-typed response field will not match.\n\n")
	b.WriteString("Produced by resolving each form against a fixture scope:\n\n")
	refs, err := exerciseRefs()
	if err != nil {
		return nil, err
	}
	b.WriteString(refs)

	b.WriteString("\n## 3. Contract overlay — `.shrt/contracts/<domain>.yaml`\n\n")
	writeTable(&b, "Overlay", reflect.TypeOf(contract.Overlay{}))
	b.WriteString("\n### Per rpc\n\n")
	writeTable(&b, "RPCContract", reflect.TypeOf(contract.RPCContract{}))
	b.WriteString("\n### `fields.<name>`\n\n")
	writeTable(&b, "FieldContract", reflect.TypeOf(contract.FieldContract{}))
	b.WriteString("\n### `aliases.<name>`\n\n")
	writeTable(&b, "AliasContract", reflect.TypeOf(contract.AliasContract{}))
	b.WriteString("\n### `failures[]`\n\n")
	writeTable(&b, "Failure", reflect.TypeOf(contract.Failure{}))

	b.WriteString("\n## 4. Config — `.shrt/config.yaml`\n\n")
	writeTable(&b, "Config", reflect.TypeOf(config.Config{}))
	b.WriteString("\n### `target`\n\n")
	writeTable(&b, "Target", reflect.TypeOf(config.Target{}))
	b.WriteString("\n### `descriptor`\n\n")
	writeTable(&b, "Descriptor", reflect.TypeOf(config.Descriptor{}))
	b.WriteString("\n### `auth`, and each entry of `auth.profiles`\n\n")
	writeTable(&b, "Auth", reflect.TypeOf(config.Auth{}))
	b.WriteString("\n### `paths`\n\n")
	writeTable(&b, "Paths", reflect.TypeOf(config.Paths{}))
	b.WriteString("\n### `conventions`\n\n")
	writeTable(&b, "Conventions", reflect.TypeOf(config.Conventions{}))

	b.WriteString("\n## 5. Run record — `.shrt/runs/<chain>/<run-id>.json`\n\n")
	b.WriteString("The evidence file. `README.md`'s authority table points here for \"does this code really fire\",\n")
	b.WriteString("so these are the fields that answer it. Reflected from `runner`, JSON names:\n\n")
	writeJSONTable(&b, "Record", reflect.TypeOf(runner.Record{}))
	b.WriteString("\n### Each entry of `steps`\n\n")
	writeJSONTable(&b, "StepRecord", reflect.TypeOf(runner.StepRecord{}))
	b.WriteString("\n### Each entry of a step's `expect`\n\n")
	writeJSONTable(&b, "ExpectResult", reflect.TypeOf(chain.ExpectResult{}))

	b.WriteString("\n## 6. Safe spot — `.shrt/safespots/<chain>.json`\n\n")
	b.WriteString("Written only by `shrt confirm <chain> -approve`. Before that, `shrt confirm <chain> -note` leaves a proposal at\n")
	b.WriteString("`.shrt/safespots/pending/<chain>.json` and its review report at `pending/<chain>.md`; neither is a safe spot,\n")
	b.WriteString("and both are removed on approval or `-reject`.\n\n")
	writeJSONTable(&b, "SafeSpot", reflect.TypeOf(store.SafeSpot{}))

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

	b.WriteString("\nTwo of these need care. **`" + runner.StatusError + "`** means the step produced no answer to judge. Usually no\n")
	b.WriteString("request was sent: a reference or auth body would not resolve, or the login failed. Less often the request\n")
	b.WriteString("went out and the transport failed, or the answer was not JSON, or its per-item verdict path could not be\n")
	b.WriteString("read. Read the step's `error` before blaming the backend. **`" + runner.StatusSkipped + "`**\n")
	b.WriteString("is the status of every step in a `-dry-run` that resolves and validates, and of a `-keep-going` step held back because it reads a\n")
	b.WriteString("step that did not pass. It is a STEP status only: the record itself is\n")
	b.WriteString("never `" + runner.StatusSkipped + "`.\n")
	b.WriteString("\nA step carrying `\"drift\": true` is a third thing, and reading it as either of the above is the\n")
	b.WriteString("mistake to avoid. It is `" + runner.StatusFailed + "` because the request WAS sent and the backend DID\n")
	b.WriteString("answer — but `conventions.validate_output` is on and the answer does not match the response\n")
	b.WriteString("message, so the expectations were never evaluated and none of them is evidence about the rpc.\n")
	b.WriteString("It means the descriptor and the deployed binary have drifted apart: rebuild with `shrt catalog\n")
	b.WriteString("build` before reading it as a backend defect. `allow_fail` does not swallow it, because a\n")
	b.WriteString("refusal is not what happened. Until 2026-09-22 this case was reported as `" + runner.StatusError + "`,\n")
	b.WriteString("contradicting the sentence above it on the one path the documented remedy can reach.\n")

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
	b.WriteString("\nThe last group is the one people miss: a `*` globs **inside** a segment, and matching folds\n")
	b.WriteString("separators and case (`namecase`), so one pattern covers `access_token` and `accessToken`\n")
	b.WriteString("alike. That is what makes `**.*password` in `.shrt/config.yaml` cover every password-shaped\n")
	b.WriteString("field whatever it is called — and why a redact pattern written without a `*` can silently\n")
	b.WriteString("mask nothing while looking careful.\n")
	return []byte(b.String()), nil
}

func writeTable(b *strings.Builder, name string, t reflect.Type) {
	rows := []row{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		key := parts[0]
		if key == "" || key == "-" {
			continue
		}
		req := ""
		if !strings.Contains(tag, "omitempty") {
			req = "+"
		}
		rows = append(rows, row{key, yamlType(f.Type), req, notes[name+"."+key]})
	}
	b.WriteString("| key | type | req | meaning |\n|---|---|---|---|\n")
	for _, r := range rows {
		note := r.note
		if note == "" {
			note = "**UNDOCUMENTED — a key exists in code with no entry in `distill/main.go`**"
		}
		fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n", r.key, r.typ, r.req, note)
	}
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
		"error":   map[string]any{"code": "OK"},
		"id_deal": "d-1",
		"rows":    []any{},
		"count":   float64(0),
		"total":   "0",
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
		verdict := "no"
		if r.Passed {
			verdict = "**yes**"
		}
		detail := r.Rule
		if r.Detail != "" {
			detail = r.Rule + " — " + r.Detail
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", c.label, detail, verdict)
	}
	b.WriteString("\nRead the last six rows together. `not_empty` is false for `0` and `[]`, so it cannot stand in for\n")
	b.WriteString("`exists`. Zero follows the field's proto type: an int64 or uint64 is stored as a JSON string, and\n")
	b.WriteString("its `\"0\"` is zero exactly as an int32's `0` is; on any numeric field `\"\"` in `equals` or `not_equal`\n")
	b.WriteString("means that zero. A rule-less entry fails loudly rather than passing quietly. And the LAST row is the one\n")
	b.WriteString("to remember: `Evaluate` is a fixed-precedence switch — `exists` > `not_empty` > `contains` >\n")
	b.WriteString("`not_equal` > `equals` — so a second rule on one entry does not ADD a check, it REPLACES the one\n")
	b.WriteString("you meant, and because the precedence runs weakest-first the entry still passes. `shrt chain lint`\n")
	b.WriteString("rejects that and the rule-less entry since 2026-09-11; write one rule per entry, repeating the path.\n")
	b.WriteString("\n")
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
	}
	var b strings.Builder
	b.WriteString("`exists` is the one rule that does not read the stored response. A run record materialises every\n")
	b.WriteString("field the output message declares, so a refusal that sent only `error` is STORED with\n")
	b.WriteString("`access_token: \"\"` beside it — that is what keeps the shape stable for `shrt verify`. Asking\n")
	b.WriteString("whether the server SENT a field has no answer in that view, so `exists` is evaluated against a\n")
	b.WriteString("second encoding of the same body that keeps only the populated fields. Below, the server sent\n")
	b.WriteString("`{\"error\": {...}}` and nothing else:\n\n")
	b.WriteString("| expectation | rule fired | passes |\n|---|---|---|\n")
	for _, c := range cases {
		r := c.e.EvaluateIn(full, present)
		verdict := "no"
		if r.Passed {
			verdict = "**yes**"
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", c.label, r.Rule, verdict)
	}
	b.WriteString("\nUntil 2026-09-22 `exists` read the materialised view, where every scalar of a described message is\n")
	b.WriteString("always present: `exists: true` could not fail and `exists: false` could not pass. A chain asserting\n")
	b.WriteString("that a login returned a token passed against a login the server refused. On a proto3 scalar without\n")
	b.WriteString("`optional`, the wire cannot distinguish an unset field from one set to its zero value, so `exists:\n")
	b.WriteString("true` and `not_empty: true` now agree there; they still differ on a message (`{}` exists, is not\n")
	b.WriteString("`not_empty`) and on a number, where `0` exists and `not_empty` is false.\n")
	return b.String()
}

func exerciseTransport() string {
	var b strings.Builder
	b.WriteString("An expectation whose path starts with `" + chain.TransportPrefix + ".` reads the recorded transport result, not\n")
	b.WriteString("the response body, so it needs no descriptor field and it is the ONLY assertion that runs when\n")
	b.WriteString("the backend refuses with a Connect error (HTTP 401, body `{\"code\": \"unauthenticated\", …}`).\n")
	b.WriteString("Every other assertion on a refused call is reported `unevaluated` and fails.\n\n")
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
	b.WriteString("\nA refused call whose step carries at least one `transport.*` assertion, and whose assertions\n")
	b.WriteString("all hold, is `passed`: the refusal is what the step said would happen, so the chain goes on\n")
	b.WriteString("without `allow_fail`. A different refusal, or a success, fails it, with or without `allow_fail`.\n")
	b.WriteString("Lint rejects any other name under `transport.`, and calls `exists: true` / `not_empty` on\n")
	b.WriteString("`transport.code` or `transport.http_status` unfailable — every answered call has both.\n")
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
		verdict := "no"
		if pathmask.Match(c.pattern, c.path) {
			verdict = "**yes**"
		}
		fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", c.pattern, c.path, verdict)
	}
	return b.String()
}

func writeJSONTable(b *strings.Builder, name string, t reflect.Type) {
	b.WriteString("| field | type | meaning |\n|---|---|---|\n")
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		key := strings.Split(tag, ",")[0]
		if key == "" || key == "-" {
			continue
		}
		note := notes[name+"."+key]
		if note == "" {
			note = "**UNDOCUMENTED — a field exists in code with no entry in `distill/main.go`**"
		}
		fmt.Fprintf(b, "| `%s` | %s | %s |\n", key, yamlType(f.Type), note)
	}
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
		},
		Volatile: []string{"**.created_at"},
	}
	rec := &runner.Record{
		RunID: "REPLAY",
		Steps: []*runner.StepRecord{
			step("a", "S/A", runner.StatusPassed, `{"qty":2,"created_at":"T1","note":"x","owner_id":"u-2","seen":"2026-09-02T11:30:00Z","lines":[1]}`),
			step("b", "S/B", runner.StatusPassed, `{"qty":1}`),
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
	fmt.Fprintf(&b, "| `note` did not change | %s |\n", yesNo(seen["a|note"]))
	fmt.Fprintf(&b, "| `owner_id` changed, NOT volatile, but its name is id-shaped | %s |\n", yesNo(seen["a|owner_id"]))
	fmt.Fprintf(&b, "| `seen` changed, NOT volatile, but both values are timestamps | %s |\n", yesNo(seen["a|seen"]))
	fmt.Fprintf(&b, "| `lines` went from 2 items to 1 | %s |\n", yesNo(seen["a|lines"]))
	fmt.Fprintf(&b, "| `qty` went from the STRING `\"1\"` to the NUMBER `1` | %s |\n", yesNo(seen["b|qty"]))
	b.WriteString("\nThe last row is reported as a `type` change rather than a `changed` one, and the report\n")
	b.WriteString("names both kinds — `want=string \"1\" got=number 1` — because printing `want=1 got=1` reads\n")
	b.WriteString("like a false positive. Until 2026-09-22 it was NOT reported at all: scalars were compared\n")
	b.WriteString("as formatted text, so a money field that started arriving as a number instead of a string\n")
	b.WriteString("passed `verify` silently. `1.0` against `1` is still clean, because JSON has no separate\n")
	b.WriteString("integer type and both decode to the same float64.\n")
	b.WriteString("\nBeyond `volatile`, verify also masks values that differ every run by shape: a changed value\n")
	b.WriteString("whose field name is id- or timestamp-shaped (`id`, `*_id`, `id_*`, `*Id`, `*_at`, `*At`, `*_time`,\n")
	b.WriteString("`token`, `idempotency_key`, ...), or where both values are timestamps or both are UUIDs, is not\n")
	fmt.Fprintf(&b, "reported; the report says how many it masked. In this example %d value(s) were masked. A name alone\n", rep.Masked)
	b.WriteString("is not enough: both values must look alike, two non-zero numbers or two non-empty strings of the\n")
	b.WriteString("same shape (`ord-819dac5f23ba` and `ord-0123456789ab`), including the same letters before the first\n")
	b.WriteString("separator, so an id of another kind (`cus-...` became `prd-...`) is reported. An id that became `\"\"`,\n")
	b.WriteString("null, `0`, an all-zero id (`cus-0`, `cus-000000000000`, the nil UUID), `undefined`, a number where a\n")
	b.WriteString("string was, or disappeared, is reported too. Masking an id is a renaming, and it must be consistent\n")
	b.WriteString("across the whole record: each id of the safe spot must become one value in the new run, wherever it\n")
	b.WriteString("appears, and no two ids of the safe spot may become the same one. An order whose `id_customer` no\n")
	b.WriteString("longer names the customer the run created, or an id unchanged in one step and renamed in another,\n")
	b.WriteString("is reported as `changed` at that path, naming the step and path that set the renaming. The same\n")
	b.WriteString("renaming is applied inside every other string before it is compared: a message that names a renamed\n")
	b.WriteString("id (`not enough stock for prd-847c…` against `… prd-b770…`) is equal, and counted with the masked\n")
	b.WriteString("ids, when the only difference is that id (a string id of at least 4 characters the renaming mapped\n")
	b.WriteString("one-to-one); the rest of the text must match exactly. `shrt diff` and `confirm`'s warning apply it too. A list whose\n")
	b.WriteString("order is not stable across runs pairs ids by position, so declare it `volatile`. `shrt verify\n")
	b.WriteString("-masked` lists every masked value, volatile or shape-masked, with its path and both values. Declare\n")
	b.WriteString("a path `volatile` when its value changes every run without being id- or timestamp-shaped.\n")
	b.WriteString("\nA value under a `redact` path (§1, §4) is blanked to `<redacted>` in the safe spot and the replay\n")
	b.WriteString("alike, so it is never compared: a change there is invisible to verify. Verify does not fail on it;\n")
	b.WriteString("it counts those values and names each one (`N redacted response value(s) ... never compared`), and\n")
	b.WriteString("`shrt confirm` lists them before approval, so redact only what must not be stored. A value under no\n")
	b.WriteString("redact path that held a secret the run knew (a credential or token it sent) is scrubbed by value to\n")
	b.WriteString("`<redacted>` just the same; verify and confirm name it apart, as `scrubbed by value` (`scrubbed_paths`\n")
	b.WriteString("in `-json`), so a field the backend echoes a credential into is not mistaken for a redact pattern.\n")
	b.WriteString("\nBefore the responses, verify compares each step's recorded REQUEST with the safe spot's and prints\n")
	b.WriteString("every difference first, as `request differs from the confirmed run at <step> <path> (a -> b)`. A\n")
	b.WriteString("request value the chain builds from another step's output or from `${uuid}` / `${now}` differs\n")
	b.WriteString("every run and is skipped; a literal, a `${vars.x}` or an `${env.X}` is input, and so is the step's\n")
	b.WriteString("`auth_profile`: a step that now runs as another principal is reported at `<step> auth_profile` and fails\n")
	b.WriteString("verify with `drift with different input` even when every response matches. So is the chain's list of\n")
	b.WriteString("steps: a step removed, added, moved or pointed at another rpc since approval is printed as `chain differs\n")
	b.WriteString("from the confirmed run at <step> ...`, and the change of step count it causes is not a regression.\n")
	b.WriteString("An expectation added, removed or edited since approval (its path, its rule, or a literal value; a\n")
	b.WriteString("`${...}` value is compared as resolved, so a `-var` read only by expectations is not an edit) is\n")
	b.WriteString("printed as `chain differs from the confirmed run at <step> expect (...)`; it explains a status change\n")
	b.WriteString("at that step and nothing else. When vars and the chain file both differ, verify names both.\n")
	b.WriteString("A fixture name (a string that interpolates a var inside other text, `sku-${vars.tag}`) and a request\n")
	b.WriteString("value under a `volatile` path are listed on one line and are NOT different input, so a fresh `-var tag`\n")
	b.WriteString("compares like with like; a response value that only echoes the new fixture name is masked and counted,\n")
	b.WriteString("by `shrt diff` and `confirm`'s drift warning as well. The comparison is made against what an echo\n")
	b.WriteString("of THIS run's input would read: a response value that still carries the confirmed run's fixture name\n")
	b.WriteString("(or a renamed id) while this run sent another, such as the old customer's email or a refusal naming\n")
	b.WriteString("the old sku, is reported as `changed` with `want` the renamed value, even though it equals the safe spot.\n")
	b.WriteString("Any other request difference is input and explains the response changes at its step and after it:\n")
	b.WriteString("when every response change comes at or after the first step whose input differs, they are reported as\n")
	b.WriteString("coming with different input and verify fails with `drift with different input`; a change at an earlier\n")
	b.WriteString("step fails verify with `regression`. A `-var` that changes no request value is not input.\n")
	fmt.Fprintf(&b, "\nChange kinds `shrt verify` and `shrt diff` print: `%s` (a path the baseline had is gone), `%s`\n", diff.KindMissing, diff.KindUnexpected)
	fmt.Fprintf(&b, "(a path the baseline did not have), `%s` (same JSON type, different value), `%s` (different JSON\n", diff.KindChanged, diff.KindType)
	fmt.Fprintf(&b, "type), `%s` (a list or the step count has a different number of items), `%s` (a step id or rpc\n", diff.KindLength, diff.KindOrder)
	fmt.Fprintf(&b, "differs at that position), `%s` (the step's pass/fail status changed).\n", diff.KindStatus)
	b.WriteString("`shrt verify` pairs the safe spot's steps with the run's by step id when every id is unique, so a\n")
	b.WriteString("step removed from the middle is one `missing` change at `step`, an added one one `unexpected`, the\n")
	b.WriteString("steps after it are still compared with their own records, and `order` at `steps` is printed once,\n")
	b.WriteString("only when the steps both runs have come in another order. The input line counts chain changes and\n")
	b.WriteString("request values apart (`1 chain change(s) since the safe spot's run ...`).\n")
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
	b.WriteString("\nEvery command prints its own flags with `-h`. `run` and `verify` take `-var key=value`\n")
	b.WriteString("(repeatable) and `-json`; `verify` also takes `-run <id>`, which re-diffs a recorded run\n")
	b.WriteString("**without touching the backend** — the one way to investigate a drift on a live-run budget.\n")
	b.WriteString("`verify` replays as `-keep-going` does, so every step a failure does not block is still compared; a\n")
	b.WriteString("step held back behind one, or never reached by a recorded run that stopped early, is reported\n")
	b.WriteString("`not_reached` rather than as a change of length, and the report names the first failing step.\n")
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
