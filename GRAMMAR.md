# GRAMMAR — every key shrt accepts

**Generated from the Go structs by `go run ./core_distillation/distill`. Do not edit.**
`scripts/check.sh` runs `-check`, so a key added in code and not here fails the gate.

Why this file exists: the loaders reject unknown keys (`KnownFields(true)`), but only since
2026-09-11. Before that a misspelled key was silently dropped and `lint` still said `ok` —
`one_of:` instead of a real rule meant a step asserted nothing while reading as checked.
A key not in this file does not exist.

A `+` in the `req` column means the key has no `omitempty`: it is always written out, and a
scaffold leaves it present-but-empty rather than absent.

## 1. Chain file — `.shrt/chains/<name>.yaml`

| key | type | req | meaning |
|---|---|---|---|
| `apiVersion` | string | + | `shrt/v1`. Defaults to it when omitted. |
| `name` | string | + | Names the run directory and the safe spot. Defaults to the file name. |
| `description` | string |  | What state this chain reproduces, for the next reader. |
| `vars` | map string → any |  | Referenced as `${vars.x}`. Override per run with `-var x=y`. |
| `volatile` | list of string |  | Response paths excluded from safe-spot diffing only. Expectations still see the real value. |
| `redact` | list of string |  | Response paths blanked in the run record. |
| `steps` | list of step | + | Ordered. Never reordered, skipped or parallelised. |

### Step

| key | type | req | meaning |
|---|---|---|---|
| `id` | string | + | Unique; later steps reference it. Derived from the rpc name when omitted. |
| `description` | string |  | Why this step is here and what its assertions mean — the place to record a judgement a reader would otherwise re-derive from the body. |
| `call` | string | + | `package.Service/Rpc`, `Service/Rpc`, or a bare `Rpc` when unambiguous. |
| `body` | map string → any |  | Validated against the proto request message before anything is sent. |
| `headers` | map string → string |  | Per-step header overrides. |
| `expect` | list of expectation |  | Assertions on this step's response. Each entry needs a `path` and exactly one rule. |
| `export` | map string → string |  | `name: response.path`. Publishes `${exports.name}` and the bare `${name}`. |
| `auth` | string |  | Named auth profile from `.shrt/config.yaml`. Contradicts `skip_auth`; lint rejects both. The reserved value `invalid` sends a token the backend never issued, in the header and scheme of the profile that would otherwise cover the call, and never re-logs in on the 401 — the probe for "an invalid token is refused". Lint rejects it with `export`; `shrt run` refuses it when the config declares no auth. |
| `skip_auth` | bool |  | Attach no auth header. For a login step, and for the probe "a missing token is refused". |
| `allow_fail` | bool |  | Tolerate the backend REFUSING this call, when a later step depends on the attempt rather than the outcome: a step the backend refused at the transport level (a Connect error) does not stop the chain, provided it declares no expectation — a refusal leaves every expectation unevaluated, and an unevaluated expectation is never tolerated. An in-band refusal whose expectations hold is simply `passed` and needs no key. It covers nothing else — not a failed expectation, not an expectation a transport error left unevaluated, not drift, not a step that was never sent (`error`) — and every one of those still stops the run. To see what lies beyond a step that does not pass, use `shrt run -keep-going`, not this key. A step MEANT to be refused at the transport level asserts which refusal with `transport.*` and needs no key. |
| `volatile` | list of string |  | Volatile paths for this step only, added to the chain's. |

### Expectation — one `path`, exactly one rule (lint-enforced since 2026-09-11)

| key | type | req | meaning |
|---|---|---|---|
| `path` | string | + | JSON path into this step's own response, or one of the reserved `transport.*` paths (table below), which read the recorded transport result instead. `${...}` here is a lint ERROR: a path names a location, not a value. So is a path the response message has no field for, under every rule — `exists: false` included, since it could not fail. |
| `equals` | any |  | Compared as text, so `1` matches `"1"`. May carry `${...}`. |
| `not_equal` | any |  | The path must be present AND differ. An absent path FAILS it, with `path not present in response` — use `exists: false` when absence is what you mean. May carry `${...}`. |
| `contains` | string |  | Substring of the value's text. May carry `${...}`. |
| `exists` | bool |  | Whether the server SENT the path. Read against the populated fields of the response, not the stored record, which materialises every declared field at its zero value. See the second table in §7. |
| `not_empty` | bool |  | Present and not `""`, `0`, `false`, `[]` or `{}`. |

### What each rule actually does

Produced by evaluating every rule against a fixture response, not by description:

| expectation | rule fired | passes |
|---|---|---|
| `equals: OK` on `error.code` | `equals` | **yes** |
| `equals: NOPE` on `error.code` | `equals` | no |
| `equals: 0` on `count` (number vs text) | `equals` | **yes** |
| `not_equal: ""` on `id_deal` | `not_equal` | **yes** |
| `not_equal: x` on a path that is ABSENT | `not_equal — path not present in response` | no |
| `contains: d-` on `id_deal` | `contains` | **yes** |
| `not_empty: true` on `id_deal` | `not_empty` | **yes** |
| `not_empty: true` on an empty list | `not_empty` | no |
| `not_empty: true` on the number 0 | `not_empty` | no |
| `exists: true` on a path that is absent | `exists` | no |
| no rule at all | `invalid — expectation has no rule` | no |
| TWO rules on one entry: `equals: NOPE` **and** `not_empty: true` | `not_empty` | **yes** |

Read the last four rows together. `not_empty` is false for `0` and `[]`, so it cannot stand in for
`exists`. A rule-less entry fails loudly rather than passing quietly. And the LAST row is the one
to remember: `Evaluate` is a fixed-precedence switch — `exists` > `not_empty` > `contains` >
`not_equal` > `equals` — so a second rule on one entry does not ADD a check, it REPLACES the one
you meant, and because the precedence runs weakest-first the entry still passes. `shrt chain lint`
rejects that and the rule-less entry since 2026-09-11; write one rule per entry, repeating the path.

`exists` is the one rule that does not read the stored response. A run record materialises every
field the output message declares, so a refusal that sent only `error` is STORED with
`access_token: ""` beside it — that is what keeps the shape stable for `shrt verify`. Asking
whether the server SENT a field has no answer in that view, so `exists` is evaluated against a
second encoding of the same body that keeps only the populated fields. Below, the server sent
`{"error": {...}}` and nothing else:

| expectation | rule fired | passes |
|---|---|---|
| `exists: true` on `access_token` | `exists` | no |
| `exists: false` on `access_token` | `exists` | **yes** |
| `not_empty: true` on `access_token` | `not_empty` | no |
| `equals: ""` on `access_token` | `equals` | **yes** |
| `exists: true` on `error.code` | `exists` | **yes** |

Until 2026-09-22 `exists` read the materialised view, where every scalar of a described message is
always present: `exists: true` could not fail and `exists: false` could not pass. A chain asserting
that a login returned a token passed against a login the server refused. On a proto3 scalar without
`optional`, the wire cannot distinguish an unset field from one set to its zero value, so `exists:
true` and `not_empty: true` now agree there; they still differ on a message (`{}` exists, is not
`not_empty`) and on a number, where `0` exists and `not_empty` is false.

### Reserved `transport.*` paths — the call's transport outcome

An expectation whose path starts with `transport.` reads the recorded transport result, not
the response body, so it needs no descriptor field and it is the ONLY assertion that runs when
the backend refuses with a Connect error (HTTP 401, body `{"code": "unauthenticated", …}`).
Every other assertion on a refused call is reported `unevaluated` and fails.

| path | reads |
|---|---|
| `transport.code` | The Connect error code of a refused call (`unauthenticated`, `permission_denied`, …), `http_<status>` when the error body is not Connect JSON, or `ok` when the call was answered 200. Always present. |
| `transport.http_status` | The HTTP status the backend answered with, as a number. Always present. |
| `transport.message` | The Connect error message of a refused call. ABSENT when the call succeeded, so `exists: false` asserts success at this layer. |

Evaluated against a 401 `unauthenticated` refusal and against a 200 answer:

| expectation | refused 401 | answered 200 |
|---|---|---|
| `transport.code` `equals: unauthenticated` | **yes** | no |
| `transport.http_status` `equals: 401` | **yes** | no |
| `transport.code` `equals: ok` | no | **yes** |
| `transport.message` `exists: false` | no | **yes** |
| `transport.message` `contains: token` | **yes** | no |

A refused call whose step carries at least one `transport.*` assertion, and whose assertions
all hold, is `passed`: the refusal is what the step said would happen, so the chain goes on
without `allow_fail`. A different refusal, or a success, fails it, with or without `allow_fail`.
Lint rejects any other name under `transport.`, and calls `exists: true` / `not_empty` on
`transport.code` or `transport.http_status` unfailable — every answered call has both.

## 2. References — `${...}`

Resolved in `body`, in `headers`, and in an expectation's `equals` / `not_equal` / `contains`.
**Not** in an expectation's `path`.

A reference that is the whole value keeps its JSON type; inside a longer string it is
interpolated as text. A reference that cannot resolve fails the step — it never becomes empty.

The `resolves to` column below is quoted where the value is a Go string, so the rows that
look numeric but are not stand out: `${nowunix}` resolves to a STRING of digits, never an
integer, so an expectation comparing it to a number-typed response field will not match.

Produced by resolving each form against a fixture scope:

| reference | resolves to | which is |
|---|---|---|
| `${vars.book_code}` | `"BOOK-A"` | a chain var |
| `${vars.nested.qty}` | `7` | a dotted path inside a var |
| `${env.API_USER}` | `"someone"` | an environment variable; missing is an error, never `""` |
| `${create_deal.id_deal}` | `"d-9"` | a field of an earlier step's RESPONSE |
| `${steps.create_deal.response.id_deal}` | `"d-9"` | the same, written out |
| `${steps.create_deal.request.id_book}` | `"b-1"` | a field of an earlier step's REQUEST |
| `${create_deal.deals.0.id_deal}` | `"d-9"` | a list index |
| `${exports.deal_id}` | `"d-9"` | a value an earlier step exported |
| `${deal_id}` | `"d-9"` | the same export, bare |
| `${now}` | `"2026-09-11T10:44:34Z"` | RFC3339, pinned here for reproducibility |
| `${nowunix}` | `"1789123474"` | unix seconds |
| `${nowunix+3600}` | `"1789127074"` | an hour ahead — a bounded-future `expires_at` without a literal that goes stale |
| `${now-86400}` | `"2026-09-10T10:44:34Z"` | the same offset in RFC3339; the unit is always SECONDS |
| `${today}` | `"1789084800"` | the UTC midnight of this run — a business date, already a multiple of 86400 |
| `${today-86400}` | `"1788998400"` | the business date before it |
| `${uuid}` | `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$` | fresh per reference — idempotency keys |
| `deal-${create_deal.id_deal}-x` | `"deal-d-9-x"` | interpolated inside a longer string, so the result is text |
| `${vars.ref_in_a_var}` | `"${uuid}"` | a var whose own value is `${uuid}` — handed back **VERBATIM**, never resolved. `lint` now rejects it |

## 3. Contract overlay — `.shrt/contracts/<domain>.yaml`

| key | type | req | meaning |
|---|---|---|---|
| `apiVersion` | string | + | `shrt/contract/v1`. |
| `domain` | string | + | Defaults to the file name. |
| `description` | string |  | Domain-wide prose: the invariants and call order every rpc here shares. |
| `failures` | list of failure |  | Inherited by every rpc in the domain. Envelope-wide codes (authn, authz) belong here, stated once. |
| `rpcs` | map string → rpccontract | + | Keyed by the fully qualified `package.Service/Rpc`. |

### Per rpc

| key | type | req | meaning |
|---|---|---|---|
| `summary` | string |  | What it does and when you would call it. |
| `note` | string |  | Free text about the rpc as a whole. It has NO mechanical effect on the score — it used to spare the no-producer charge, and no longer does, because an exemption any sentence can buy measures nothing. To declare that a read has no producer, use `no_producer`. |
| `auth` | string |  | Named auth profile this rpc needs when the default principal is the wrong one. `plan` writes it onto the step. |
| `requires_role` | list of string |  | Roles the caller must hold. A precondition, not a failure. The literal `NONE`, alone, declares that this rpc reaches no role gate; leaving the key out entirely is scored as an omission. |
| `required` | list of string | + | Fields the server actually rejects without. proto3 has no `required`, so this comes from reading the backend. Applies to reads as well as writes. The literal `NONE`, alone, declares that the server rejects nothing; leaving the list empty on an rpc that takes request fields is scored as an omission, so silence and `NONE` cannot look the same. The literal `UNKNOWN`, alone, is the honest answer when the handler could not be found — it lints as a warning rather than an error, and is scored exactly as an empty list is, because not knowing must not be cheaper than knowing. |
| `needs` | list of string |  | An rpc that must run first but whose output no field consumes. |
| `no_producer` | string |  | Why no write rpc in this API puts the rows this read returns there — a seed, a migration, an external feed. It is the ONLY thing that spares the read-with-no-producer charge; a general-purpose `note` deliberately does not, because an exemption anyone can buy with one sentence measures nothing. Meaningless on a write rpc. |
| `before` | list of string |  | The inverse of `needs`, declarable from the side that owns the prerequisite. |
| `fields` | map string → fieldcontract |  | Per request field. Dotted keys reach into nested messages. |
| `aliases` | map string → aliascontract |  | Per-instance overrides, so two aliased steps of one rpc differ. |
| `exports` | map string → string |  | Response paths worth exporting, and why. |
| `terminal` | map string → string |  | Response fields that deliberately have no consumer. Records the dead end instead of deleting it. |
| `soft_signals` | map string → string |  | Response fields carrying advisory information rather than success or failure. |
| `failures` | list of failure |  | One entry per way this rpc refuses. |
| `source` | list of string |  | Files read to determine all this, so a reviewer can re-check it. Strip any `:line-range` before resolving; ranges rot, paths do not. |
| `status` | string | + | `draft`, or `verified` with a `verified_run`. An agent leaves it `draft`. |
| `verified_by` | string |  | The person who verified it. |
| `verified_run` | string |  | The run id that proved it. |

### `fields.<name>`

| key | type | req | meaning |
|---|---|---|---|
| `from` | string |  | `<rpc>[@alias]->response_path`. This value comes from an earlier call, and declares the ordering edge. |
| `value` | string |  | A fixed literal or template, e.g. `${uuid}`. |
| `same_as` | string |  | `<rpc>[@alias]->request_path`. Must equal what an earlier call SENT. Mutually exclusive with `from`; `plan` rewrites both sites onto one generated var. |
| `oneof` | string |  | Mutual-exclusion group; at most one member may carry a value. The member that carries one is also the member `contract show` and `chain new` scaffold, in place of the proto group's first field. |
| `checked_by` | string |  | How the server validates the id, which decides whether a bad one is a named domain failure or an unnamed 500. |
| `note` | string |  | Units, formats, constraints. |

### `aliases.<name>`

| key | type | req | meaning |
|---|---|---|---|
| `note` | string |  | What makes this instance different. |
| `fields` | map string → fieldcontract |  | Field overrides for this instance only. |

### `failures[]`

| key | type | req | meaning |
|---|---|---|---|
| `code` | int |  | The app code in the error envelope. Optional: a shape error raised before the business logic has only a connect code. |
| `connect_code` | string |  | The Connect code, e.g. `invalid_argument`. |
| `reason` | string |  | The backend's own reason string, verbatim. Must match the `errmsg.New` site. |
| `message` | string |  | The message text, when it is worth pinning. |
| `field` | string |  | The request field at fault, for shape errors. |
| `when` | string |  | The condition that raises it. This is the one an author most often leaves vague. |
| `unreachable` | string |  | Declared but cannot fire, and why. Keeps it out of the coverage denominator without deleting the knowledge. The commonest honest use is a code NO shrt chain can observe: every request is validated against the descriptor before it is sent, so a refusal reachable only by a body the proto cannot express — a string where a message belongs, an enum value the descriptor does not know, a catch-all handler for a decode error — is out of reach for a descriptor-driven client and always will be. Say that here rather than leaving the code undeclared, or the coverage term charges you for a branch nothing can reach. |
| `pending_deploy` | string |  | Declared, correct, and not yet on the box. Carries the commit that will make it reachable. |

## 4. Config — `.shrt/config.yaml`

| key | type | req | meaning |
|---|---|---|---|
| `target` | target | + | Where chains run. |
| `descriptor` | descriptor | + | Where the proto descriptor lives and what rebuilds it. |
| `auth` | auth |  | The login call, declared once for the whole repo. |
| `paths` | paths | + | Where chains, runs and safe spots live. |
| `conventions` | conventions |  | Naming and envelope conventions of THIS backend. Every key optional. The envelope defaults are what shrt assumed before the block existed; the read-name default is wider than the five prefixes that used to be hard-coded. |
| `volatile` | list of string |  | Volatile paths applied to every chain. |
| `redact` | list of string |  | Paths blanked in every run record. Credentials belong here. Without the key the defaults apply, and `shrt init` writes them out: `**.*password`, `**.access_token`, `**.refresh_token`, `**.token`, `**.*secret`, `**.*pin`, `**.*pin_code`, `**.*passcode`, `**.*otp`, `**.api_key`, `**.authorization`. A bool is never masked. An explicit list REPLACES the defaults rather than adding to them, so a config written before a default was added does not get it — add the pattern by hand. |

### `target`

| key | type | req | meaning |
|---|---|---|---|
| `base_url` | string | + | Scheme and host of the backend. |
| `host_override` | string |  | Send this as the `Host` header and the TLS `ServerName`, while connecting to `base_url`'s address. For reaching a vhost by IP without disabling verification. |
| `headers` | map string → string |  | Headers added to every request. |
| `timeout` | string |  | Per-request timeout, e.g. `30s`. Defaults to 30s. |
| `build_header` | string |  | A response header in which the server reports its own build or version, e.g. `X-Server-Version`. Its value is stamped into each run record as `build`, and a value that changes mid-run is recorded as `old -> new` with a warning on the step that first saw it. `shrt run -build <label>` overrides it, and a label the header contradicts is warned about. Unset, a record says only which `base_url` answered, not which build. |

### `descriptor`

| key | type | req | meaning |
|---|---|---|---|
| `file` | string | + | The compiled `FileDescriptorSet`. |
| `source` | string |  | What `shrt catalog build` compiles, passed to the descriptor binary. |
| `binary` | string |  | The compiler, normally `buf`. |

### `auth`, and each entry of `auth.profiles`

| key | type | req | meaning |
|---|---|---|---|
| `call` | string | + | The login rpc. |
| `body` | map string → any | + | Its request body. `${env.X}` belongs here, never a literal credential. Resolved before any step runs, so only `${env.*}`, `${uuid}` and the clock forms work; `doctor` and `chain lint` reject `${vars.*}`, exports and step references. |
| `token_path` | string | + | Response path holding the token. |
| `expires_path` | string |  | Response path holding the expiry. Without it the token is refreshed only on a 401. |
| `header` | string |  | Defaults to `Authorization`. |
| `scheme` | string |  | Defaults to `Bearer`. |
| `skip_calls` | list of string |  | Calls this profile must not authenticate. |
| `leeway_seconds` | int |  | Re-login this many seconds before expiry. |
| `calls` | list of string |  | Glob patterns this profile owns, e.g. `acme.partner.*`. |
| `profiles` | map string → auth |  | Named additional principals. Each holds its own token cache. |

### `paths`

| key | type | req | meaning |
|---|---|---|---|
| `chains` | string | + | Chain YAML directory. |
| `contracts` | string |  | Curated contract overlay directory, one file per domain. Read it from here rather than assuming `.shrt/contracts`. |
| `runs` | string | + | Run record directory. |
| `safespots` | string | + | Safe spot directory. |

### `conventions`

| key | type | req | meaning |
|---|---|---|---|
| `read_only_prefixes` | list of string |  | Rpc-name prefixes that mean a call only reads. Decides which scaffold an rpc gets, whether it can produce an id for another rpc, and three quality terms. Default: Fetch, Get, List, Preview, Search, Read, Query, Find, Lookup, Describe, Show, Count, Export, Download, Retrieve. |
| `envelope_path` | string |  | JSON path at which a response reports its own verdict. Default `error.code`. Set it to MOVE the envelope, never to remove it: an explicit empty value is indistinguishable from an absent key and falls back to the default. A backend with no in-body envelope needs no setting — a response carrying no field of that name gets a scaffolded assertion on a real response field instead. |
| `envelope_ok` | string |  | The `envelope_path` value that means success. Default `OK`. |
| `item_envelope_path` | string |  | Per-item verdict in a BATCH response, as `<list>[].<path>` (e.g. `results[].error.code`). A batch rpc can answer `OK` at the top level while refusing every line; without this the runner cannot see that, and a step asserting only the envelope passes having achieved nothing. Unset means the backend has no per-item envelope. Checked only on rpcs whose response message declares that list with that field, so a list of atomic receipts carrying no verdict is left alone; a refusal the step pins with `equals`, `not_equal` or `contains` on that line's verdict path is declared, not reported; a path no response message declares fails `shrt run` before any traffic is sent. |
| `code_fields` | list of string |  | Detail-field names that carry a backend's OWN numeric or symbolic code, searched by `shrt chain which -code`. Default `app_code`, `reason`, `error_code`. An explicit list REPLACES the defaults. The envelope's own leaf is not listed here — it follows `envelope_path`, so a deployment answering at `status.code` is searched there without any setting. Nothing enforces these names; a code this list cannot reach makes `chain which` answer "no chain asserts it" for a corpus that does. |
| `validate_output` | bool |  | When true, a response that does not match its proto message FAILS the step. Default false: the response is kept as sent and a warning is recorded, so a descriptor that has drifted from the deployed binary degrades quietly rather than failing every chain. Turn it on once your descriptor build and your deploy are in step. |

## 5. Run record — `.shrt/runs/<chain>/<run-id>.json`

The evidence file. `README.md`'s authority table points here for "does this code really fire",
so these are the fields that answer it. Reflected from `core_distillation/runner`, JSON names:

| field | type | meaning |
|---|---|---|
| `run_id` | string | Timestamp-prefixed, e.g. `20260911T104434Z-e94560bd`. `-run latest` picks the alphabetically last file, which is the newest ONLY because of that prefix. |
| `chain` | string | Chain name, which is also the run directory and the safe-spot key. |
| `chain_source` | string | Path the chain was loaded from. |
| `target` | string | The base_url this ran against. A receipt quoted without it says nothing about which box answered. |
| `build` | string | Which build of the target answered: the `shrt run -build` label, else the value of `target.build_header`. Absent when neither is set, and then two builds behind one `target` are indistinguishable — do not compare such records across a deploy. |
| `started_at` | time | UTC start time. |
| `duration_ms` | int | Whole-run wall time. |
| `status` | string | `passed`, `failed` or `error` — never `skipped`; that is a step status. |
| `dry_run` | bool | True when `-dry-run` produced this record: every step resolved and validated, none was sent. `status` is still `passed` on success, so a gate that reads only `status` cannot tell a dry run from a real one — read this field too. Dry runs are never saved, so it is absent from every record under `.shrt/runs/`. |
| `keep_going` | bool | True when `shrt run -keep-going` produced this record: steps after a failure were still run, so a later red may be a consequence of an earlier one. |
| `vars` | map string → any | The resolved vars this run used, so a replay can be reproduced. |
| `exports` | map string → any | Everything any step exported. |
| `volatile` | list of string | Volatile patterns in force, chain plus step plus config. |
| `redacted` | list of string | Redact patterns in force. The values themselves are already masked in `request`/`response`. |
| `steps` | list of steprecord | One entry per step, in order. |
| `failure` | string | Why the run stopped, when it did. Under `-keep-going`, one line per step that did not pass. |
| `failed_steps` | list of string | Under `-keep-going`, the id of every step that did not pass, in order — failed, error, and skipped behind one of those. `status` is the FIRST such step's status, the same verdict the run would have had without the flag. |

### Each entry of `steps`

| field | type | meaning |
|---|---|---|
| `index` | int | Position in the chain, from 0. |
| `id` | string | The step id. |
| `call` | string | As written in the chain. |
| `procedure` | string | The resolved `/package.Service/Rpc`. |
| `auth_profile` | string | The auth profile whose token this step carried: `default`, a name under `auth.profiles`, or `none` when no token was attached (`skip_auth`, a login rpc, `auth.skip_calls`). Absent when the config declares no `auth:` block and in a dry run. Two steps that should act as one principal and show different values here are a principal swap. |
| `status` | string | `passed`, `failed`, `error`, or `skipped` — every step of a `-dry-run` is `skipped`, and so is a `-keep-going` step that was not sent because it reads (`${steps.X…}`, `${X.…}` or an export X declares) a step that did not pass; its `error` names that step. |
| `http_status` | int | Transport status. 200 with a non-OK `error.code` in the body is the normal shape of a business refusal. |
| `latency_ms` | int | Per-step wall time. |
| `request` | list of uint8 | What was sent, AFTER reference resolution and redaction. |
| `response` | list of uint8 | What came back, RE-ENCODED through the response message and then redacted — not the wire bytes. Field names are the proto ones, every declared field is present at its zero value if the server omitted it, and an int64 is a JSON string whatever the server sent. That is what gives `shrt verify` a stable shape to diff across runs, and it is why a scalar's absence cannot be read out of this field: see section 7 on `exists`. When the descriptor cannot decode the body it is stored as sent instead, and the step carries a `warning` saying so, so the shape of this field depends on descriptor freshness. |
| `transport_error` | transporterror | Set when the backend answered with a Connect error (any non-200) instead of a response message. `transport.code` and `transport.message` read it. |
| `expect` | list of expectresult | One entry per expectation, with the rule that fired and whether it held. Read this, not just the step status. |
| `exported` | map string → any | What this step published. |
| `error` | string | Why this step failed or could not run. |
| `warning` | string | Non-fatal note from the runner. |
| `note` | string | Runner commentary, e.g. that a login seeded a profile's token — or did NOT, because it sent other credentials than that profile's `body`. A login seeds a profile only when its request equals that profile's resolved body, so a chain that logs in as someone else never changes whose token later steps carry. |
| `volatile` | list of string | Step-level volatile patterns. |
| `drift` | bool | The response did not match its proto message while `conventions.validate_output` was on. The step is `failed`, not `error`: the request was sent and answered. No expectation was evaluated, so nothing in this step is evidence about the rpc — rebuild the descriptor first. `allow_fail` does not swallow a step carrying it. |

### Each entry of a step's `expect`

| field | type | meaning |
|---|---|---|
| `path` | string | The path asserted. |
| `rule` | string | Which rule actually fired — the one to read when two rules were written. |
| `want` | any | The comparison value, after `${...}` resolution. |
| `got` | any | What was actually there. |
| `passed` | bool | Whether it held. |
| `detail` | string | Why not, when the rule itself was malformed. |

## 6. Safe spot — `.shrt/safespots/<chain>.json`

| field | type | meaning |
|---|---|---|
| `chain` | string | Which chain this is the ground truth for. |
| `run_id` | string | The run a human confirmed. |
| `target` | string | Where that run happened. |
| `build` | string | The confirmed run's `build`, when it had one. `shrt verify` prints it beside the replay's. |
| `confirmed_by` | string | The person. `shrt confirm` refuses an empty one. |
| `confirmed_at` | time | When. |
| `note` | string | What makes this run correct. |
| `supersedes` | string | The run id this replaced, when promoted with `-supersede`. |
| `volatile` | list of string | Patterns masked before comparison. |
| `digest` | string | Fingerprint of the confirmed steps. |
| `steps` | list of steprecord | The confirmed step records, which a replay is diffed against. |

## 7. What `shrt verify` actually compares

Produced by running `diff.Compare` on a fabricated safe spot and replay:

| what changed between the confirmed run and the replay | reported? |
|---|---|
| `qty` went from `1` to `2` | **yes** |
| `created_at` changed, and is `volatile` | no |
| `note` did not change | no |
| `qty` went from the STRING `"1"` to the NUMBER `1` | **yes** |

The last row is reported as a `type` change rather than a `changed` one, and the report
names both kinds — `want=number 1 got=string "1"` — because printing `want=1 got=1` reads
like a false positive. Until 2026-09-22 it was NOT reported at all: scalars were compared
as formatted text, so a money field that started arriving as a number instead of a string
passed `verify` silently. `1.0` against `1` is still clean, because JSON has no separate
integer type and both decode to the same float64.

## 8. Closed vocabularies

Read from the constants themselves, so a renamed constant shows up here:

| where | allowed |
|---|---|
| chain `apiVersion` | `shrt/v1` |
| overlay `apiVersion` | `shrt/contract/v1` |
| `status` | `draft`, `verified` |
| `checked_by` | `fk`, `app_lookup`, `none` |
| `from` / `same_as` separator | `->` (legacy `#` still parses) |
| default auth profile | `default` |
| reserved `auth:` value (never a profile name) | `invalid` |
| run record status | `passed`, `failed`, `error` |
| STEP status | the three above, plus `skipped` |

Two of these need care. **`error`** means no request was sent for that step — an
unresolvable reference or auth body — so it says nothing about the backend. **`skipped`**
is the status of every step in a `-dry-run`, and of a `-keep-going` step held back because it reads a
step that did not pass. It is a STEP status only: the record itself is
never `skipped`.

A step carrying `"drift": true` is a third thing, and reading it as either of the above is the
mistake to avoid. It is `failed` because the request WAS sent and the backend DID
answer — but `conventions.validate_output` is on and the answer does not match the response
message, so the expectations were never evaluated and none of them is evidence about the rpc.
It means the descriptor and the deployed binary have drifted apart: rebuild with `shrt catalog
build` before reading it as a backend defect. `allow_fail` does not swallow it, because a
refusal is not what happened. Until 2026-09-22 this case was reported as `error`,
contradicting the sentence above it on the one path the documented remedy can reach.

## 9. Commands

Captured by running the binary, so a renamed command cannot survive here:

```
shrt — record, replay and verify internal API call chains

usage: shrt <command> [flags]

  catalog    build, list and describe the RPC surface
  chain      scaffold, list, search, lint, slice and audit chain definitions
  confirm    promote a recorded run to the safe spot for its chain (human only)
  contract   author and use the curated RPC contracts agents write chains from
  diff       compare two recorded runs of a chain step by step, with no safe spot
  doctor     check this repo's .shrt/ installation: docs, descriptor, ignores, tokens, auth
  init       set up .shrt/ and the Claude agent kit in the current repo
  run        replay a chain against the target and record the result
  verify     replay a chain and diff it against its safe spot
  version    which build this is: version, commit, and the docs it carries

run 'shrt <command> -h' for command flags
exit status 2
```

| group | subcommands |
|---|---|
| `shrt catalog` | `<build\|ls\|describe> [flags]` |
| `shrt chain` | `<new\|ls\|which\|lint\|slice\|hollow> [flags]` |
| `shrt contract` | `<init\|lint\|show\|plan\|status\|quality> [flags]` |

Every command prints its own flags with `-h`. `run` and `verify` take `-var key=value`
(repeatable) and `-json`; `verify` also takes `-run <id>`, which re-diffs a recorded run
**without touching the backend** — the one way to investigate a drift on a live-run budget.

## 10. Volatile and redact patterns

Produced by running `pathmask.Match(pattern, path)`:

| pattern | path | matches |
|---|---|---|
| `**.created_at` | `deals.0.created_at` | **yes** |
| `**.created_at` | `created_at` | **yes** |
| `**.created_at` | `deals.0.updated_at` | no |
| `deals.*.id_deal` | `deals.0.id_deal` | **yes** |
| `deals.*.id_deal` | `deals.0.legs.0.id_deal` | no |
| `deals.0.id_deal` | `deals.0.id_deal` | **yes** |
| `deals.0.id_deal` | `deals.1.id_deal` | no |
| `error.code` | `error.code` | **yes** |
| `error.code` | `error.details.0.code` | no |
| `**.*password` | `login.user_password` | **yes** |
| `**.*password` | `login.password` | **yes** |
| `**.access_token` | `auth.accessToken` | **yes** |
| `**.*pin` | `login.user_pin` | **yes** |
| `**.*pin` | `login.opinion` | no |

The last group is the one people miss: a `*` globs **inside** a segment, and matching folds
separators and case (`core_distillation/namecase`), so one pattern covers `access_token` and `accessToken`
alike. That is what makes `**.*password` in `.shrt/config.yaml` cover every password-shaped
field whatever it is called — and why a redact pattern written without a `*` can silently
mask nothing while looking careful.
