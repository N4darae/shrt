# GRAMMAR — every key shrt accepts

Generated from the code by `go run ./distill`; do not edit. Loaders reject unknown keys, so a key
not here does not exist. `+` in `req`: the key is always written out.

## 1. Chain file — `.shrt/chains/<name>.yaml`

| key | type | req | meaning |
|---|---|---|---|
| `apiVersion` | string | + | `shrt/v1`. Defaults to it when omitted. |
| `name` | string | + | Names the run directory and safe spot. Defaults to the file name; differing from it is a lint warning, sharing it a lint error. |
| `description` | string |  | What state this chain reproduces, for the next reader. |
| `vars` | map string → any |  | `${vars.x}`; override with `-var x=y`. An undeclared var must come from `-var`, except `tag`, which is fresh each run. |
| `volatile` | list of string |  | Response paths `shrt verify` and `shrt diff` mask in every step. Expectations still see the real value. |
| `unordered` | list of string |  | Response lists `verify` compares as multisets, for an rpc that promises no order. No indices: `invoices.lines`. |
| `redact` | list of string |  | Paths blanked everywhere in the run record and never compared, added to the config's. Only what must not be stored. |
| `steps` | list of step | + | Ordered; never reordered, parallelised or skipped. |
| `kept_red` | list of pin |  | A known defect the chain shows on purpose. `run` exits 0 only while it fails exactly as pinned; never confirmed. |

### Step

| key | type | req | meaning |
|---|---|---|---|
| `id` | string | + | Unique; later steps reference it. Derived from the rpc name when omitted. |
| `description` | string |  | Why this step is here and what its assertions mean. |
| `call` | string | + | `package.Service/Rpc`, `Service/Rpc`, or a bare unambiguous `Rpc`. A server stream records its first message as `messages.0`, within 5s; client and bidi streams are refused. |
| `body` | map string → any |  | The request, validated against the proto request message before anything is sent. |
| `headers` | map string → string |  | Per-step headers. Never `Authorization`: use `auth: <profile>`. |
| `expect` | list of expectation |  | Assertions on the response, one rule per entry. |
| `export` | map string → string |  | `name: <response path>` with no `response.` prefix, e.g. `id_invoice: invoice.id_invoice`. Publishes `${exports.name}` and `${name}`. A name equal to a step id is refused; one exported twice is a warning. |
| `auth` | string |  | The auth profile for this step. `invalid` sends a never-issued token: the invalid-token probe. |
| `skip_auth` | bool |  | Attach no auth header: the missing-token probe. A login step does not need it. |
| `allow_fail` | bool |  | Go on past a transport refusal on a step with no expectations. Never waives a failed expectation or an `error`. |
| `volatile` | list of string |  | Volatile paths for this step only, added to the chain's. |
| `unordered` | list of string |  | Unordered lists for this step only, added to the chain's. |
| `wait` | string |  | A Go duration, at most `10m`, waited before sending, outside its latency. |

### Expectation — exactly one rule per entry

| key | type | req | meaning |
|---|---|---|---|
| `path` | string | + | Response path (`invoice.lines.0.amount_minor`) or a `transport.*` path. No `${...}`. A path the message lacks is a lint error. Names match ignoring case and separators. |
| `equals` | any |  | Compared as text: `1` matches `"1"`. May carry `${...}`; no arithmetic. |
| `includes` | any |  | Some item of the list matches: a map names item fields, a scalar is the item. May carry `${...}`. |
| `not_equal` | any |  | Present AND different; an absent path fails it. May carry `${...}`. |
| `contains` | string |  | Substring of the value's text. May carry `${...}`. |
| `exists` | bool |  | Whether the server SENT the path; see the second table below. |
| `not_empty` | bool |  | Present and not `""`, `0`, `false`, `[]` or `{}`; an int64 `"0"` is zero too. |
| `gt` | any |  | Present and a number greater than this; an int64 as text is its number, an RFC3339 time its unix seconds. May carry `${...}`. |
| `gte` | any |  | As `gt`, greater than or equal. |
| `lt` | any |  | As `gt`, less than. |
| `lte` | any |  | As `gt`, less than or equal. |
| `between` | list of any |  | Exactly two inclusive bounds `[low, high]`, read as `gt` reads them. |
| `within` | within |  | `{of: X, by: N}`: at most N from X. The rule for clocks: `within: {of: "${nowunix+3600}", by: 5}`. |

### What each rule actually does

| expectation | rule fired | passes |
|---|---|---|
| `equals: OK` on `error.code` | `equals` | **yes** |
| `equals: NOPE` on `error.code` | `equals` | no |
| `equals: 0` on `count` (number vs text) | `equals` | **yes** |
| `not_equal: ""` on `id_deal` | `not_equal` | **yes** |
| `not_equal: x` on a path that is ABSENT | `not_equal — path not present in response` | no |
| `contains: d-` on `id_deal` | `contains` | **yes** |
| `gt: 1789123474` on `expires_at` (an int64, stored as text) | `gt` | **yes** |
| `between: [1789127000, 1789127100]` on `expires_at` | `between` | **yes** |
| `within: {of: 1789127074, by: 5}` on `expires_ms`, the same instant in milliseconds | `within` | no |
| `lte: 1789123474` on `created_at` (RFC3339, read as unix seconds) | `lte` | **yes** |
| `gt: 0` on `id_deal` (not a number) | `gt — the value is not a number or an RFC3339 time, so it cannot be compared` | no |
| `not_empty: true` on `id_deal` | `not_empty` | **yes** |
| `not_empty: true` on an empty list | `not_empty` | no |
| `not_empty: true` on the number 0 | `not_empty` | no |
| `not_empty: true` on an int64 at 0 (stored as the string `"0"`) | `not_empty` | no |
| `not_equal: ""` on an int64 at 0 | `not_equal` | no |
| `exists: true` on a path that is absent | `exists` | no |
| no rule at all | `invalid — expectation has no rule` | no |
| TWO rules on one entry: `equals: NOPE` **and** `not_empty: true` | `not_empty` | **yes** |

Two rules on one entry are a lint error: write one rule per entry.

`exists` reads what the server SENT. Below, it sent `{"error": {...}}` and nothing else:

| expectation | rule fired | passes |
|---|---|---|
| `exists: true` on `access_token` | `exists` | no |
| `exists: false` on `access_token` | `exists` | **yes** |
| `not_empty: true` on `access_token` | `not_empty` | no |
| `equals: ""` on `access_token` | `equals` | **yes** |
| `exists: true` on `error.code` | `exists` | **yes** |
| `equals: ""` on `user.name`, inside a message not sent | `equals` | no |
| `exists: false` on `user.name`, inside a message not sent | `exists` | **yes** |

A proto3 scalar cannot tell unset from zero: assert the value. For a field inside a message not sent,
assert the message `exists: false`.

### Reserved `transport.*` paths — the call's transport outcome

A `transport.*` path reads the transport result, not the body. On a Connect error (HTTP 4xx/5xx)
it is the only assertion that runs; the rest are `unevaluated` and fail.

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

A refused call whose `transport.*` assertions all hold is `passed`, with no `allow_fail`.

### `kept_red[]` — a known defect the chain pins

| key | type | req | meaning |
|---|---|---|---|
| `step` | string | + | The step the defect shows at. |
| `path` | string | + | An expectation path of that step that must fail against an answered response; one entry per failing expectation. |
| `got` | string |  | The value the failed expectation must have got, compared as text; `""` is absent, and a `${...}` reference resolves against the run. Omit to pin only where it fails. |

## 2. References — `${...}`

- Resolved in `body`, `headers`, and an expectation's `equals`, `not_equal`, `contains` and numeric bounds. Never in `path`.
- A whole-value reference keeps its JSON type; inside a longer string it is text, and only a scalar may be interpolated.
- One known not to resolve (a later or missing step, an unset `${env.*}`, an undeclared field) is a lint error.
- No escape for a literal `${`: pass it through `${env.NAME}` or `-var`.
- `${nowunix}` is a STRING of digits.

| reference | resolves to | which is |
|---|---|---|
| `${vars.book_code}` | `"BOOK-A"` | a chain var |
| `${vars.nested.qty}` | `7` | a dotted path inside a var |
| `${env.API_USER}` | `"someone"` | an environment variable; missing is an error, never `""` |
| `${create_deal.id_deal}` | `"d-9"` | a field of an earlier step's RESPONSE: write `${<step>.<path>}` |
| `${steps.create_deal.request.id_book}` | `"b-1"` | a field of an earlier step's REQUEST: write `${steps.<step>.request.<field>}`; or, inside `expect`, this step's own, to assert the response echoes what was sent |
| `${create_deal.deals.0.id_deal}` | `"d-9"` | a list index |
| `${now}` | `"2026-09-11T10:44:34Z"` | RFC3339, pinned here for reproducibility |
| `${nowunix}` | `"1789123474"` | unix seconds |
| `${nowunix+3600}` | `"1789127074"` | an hour ahead — a bounded-future `expires_at` without a literal that goes stale |
| `${now-86400}` | `"2026-09-10T10:44:34Z"` | the same offset in RFC3339; the unit is always SECONDS |
| `${today}` | `"1789084800"` | the UTC midnight of this run — a business date, already a multiple of 86400 |
| `${today-86400}` | `"1788998400"` | the business date before it |
| `${uuid}` | `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$` | fresh per reference — idempotency keys; verify and diff mask a response that only echoes it |
| `deal-${create_deal.id_deal}-x` | `"deal-d-9-x"` | interpolated inside a longer string, so the result is text |
| `${vars.ref_in_a_var}` | `"${uuid}"` | a var whose own value is `${uuid}` — handed back **VERBATIM**, never resolved. `lint` and `run` reject it |

`contract plan` and `chain new` write those two forms. Also accepted, for chains already written so: `${steps.create_deal.response.id_deal}` (the response field written out), `${exports.deal_id}` (a value an earlier step's `export:` named) and `${deal_id}` (the same export, bare), each `"d-9"`.

## 3. Contract overlay — `.shrt/contracts/<domain>.yaml`

| key | type | req | meaning |
|---|---|---|---|
| `apiVersion` | string | + | `shrt/contract/v1`. |
| `domain` | string | + | Defaults to the file name. |
| `description` | string |  | Domain-wide prose: the invariants and call order every rpc here shares. |
| `failures` | list of failure |  | Failures every rpc in the domain inherits; `scope: all` shares one with every domain. |
| `rpcs` | map string → rpccontract | + | Keyed by the fully qualified `package.Service/Rpc`. |

### Per rpc

| key | type | req | meaning |
|---|---|---|---|
| `summary` | string |  | Prose for people. Numbers a write moves go in `effects`. |
| `note` | string |  | Free text. Spares no quality term. |
| `auth` | string |  | The profile the rpc needs instead of the default; `plan` puts it on the step. |
| `requires_role` | list of string |  | Roles the caller must hold; `[NONE]`: no role gate. `plan` calls a gated rpc as each other profile, expecting denial. |
| `required` | list of string | + | Fields the server rejects without, reads too. `[NONE]`: nothing. `[UNKNOWN]`: handler not found. |
| `needs` | list of string |  | An rpc that must run first though no field consumes its output, e.g. the write creating what a list lists. A write that only reaches this rpc's `restore:` state also makes `plan` call it from the state before. |
| `no_producer` | string |  | Why no write here creates the rows this read returns: a seed, a feed. |
| `before` | list of string |  | The inverse of `needs`, declared in the prerequisite's domain. Plain rpc names only. |
| `fields` | map string → fieldcontract |  | Per request field. Dotted keys nest; `lines.1.id_account` picks an entry, `lines.qty` applies to every entry. |
| `aliases` | map string → aliascontract |  | Per-instance overrides, so two aliased steps of one rpc differ. |
| `effects` | map string → effect |  | What it does to numbers, keyed by field: `{balance: {increase: amount}}`, or `none`, `zero` (a create starts at 0), `per_item` (on a repeated request field: each item applied or refused alone). |
| `exports` | map string → string |  | Response paths worth exporting, and why. |
| `terminal` | map string → string |  | Response fields that deliberately have no consumer. |
| `soft_signals` | map string → string |  | Advisory response fields, neither success nor failure. |
| `failures` | list of failure |  | One entry per way this rpc refuses. |
| `source` | list of string |  | Files read to determine all this, without line ranges. |
| `status` | string | + | `draft`, or `verified` with a `verified_run`. An agent leaves it `draft`. |
| `verified_by` | string |  | The person who verified it. |
| `verified_run` | string |  | The run id that proved it. |

### `fields.<name>`

| key | type | req | meaning |
|---|---|---|---|
| `from` | string |  | `<rpc>[@alias]->response_path`: the value comes from an earlier call's response, which orders the two. |
| `value` | string |  | A fixed literal or template, e.g. `${uuid}` or `inv-${vars.tag}-a`. `value: "0"` marks a zero as deliberate. |
| `same_as` | string |  | `<rpc>[@alias]->request_path`: must equal what an earlier call SENT. Exclusive with `from`. |
| `oneof` | string |  | Mutual-exclusion group; one member carries a value. |
| `checked_by` | string |  | How the server validates the id, which decides whether a bad one is a named failure or an unnamed 500. |
| `note` | string |  | Units, formats, constraints; `plan` reads uniqueness, normalisation and stated bounds from it. |

### `aliases.<name>`

| key | type | req | meaning |
|---|---|---|---|
| `note` | string |  | What makes this instance different. |
| `fields` | map string → fieldcontract |  | Field overrides for this instance only. |

### `effects.<field>`

| key | type | req | meaning |
|---|---|---|---|
| `increase` | string |  | The request number it grows by: `amount`, or `lines.amount` per line, on the record a `from:`-wired id names. |
| `decrease` | string |  | As `increase`, shrinking. |
| `of` | string |  | An id wired `from:` another write whose request holds the path: `{decrease: lines.amount, of: id_invoice}`. Not with `restore`. |
| `restore` | string |  | The state from which it gives back what a decrease took: `{balance: {restore: POSTED}}`. From other states, nothing. |
| `sum` | string |  | `<list>.<qty>`: the key sums qty times `times` over the lines. |
| `times` | string |  | The price in the request of the record each line names: `{total: {sum: lines.qty, times: unit_price}}`. |

### `failures[]`

| key | type | req | meaning |
|---|---|---|---|
| `code` | int |  | The app code in the error envelope. Optional: a shape error has only a connect code. |
| `connect_code` | string |  | The Connect code, e.g. `invalid_argument`. |
| `reason` | string |  | The backend's own reason string, verbatim; for a shape or auth failure, a label you choose. |
| `message` | string |  | The message text, when it is worth pinning. |
| `field` | string |  | The request field at fault, or the field a uniqueness refusal is about when its reason does not name it. |
| `when` | string |  | The condition that raises it. `plan` derives probes from it: uniqueness, a limit, a named state, not found, and `invalid_argument` clauses such as empty, zero or negative. |
| `unreachable` | string |  | Why it cannot fire, e.g. only a body the proto cannot express reaches it. Kept out of coverage. |
| `scope` | string |  | Domain-level `failures:` only. `all` shares it with every domain: declare `unauthenticated` once. |
| `pending_deploy` | string |  | Declared, correct, and not yet deployed; carries the commit that will make it reachable. |
| `unique` | uniquecompare |  | How a uniqueness refusal compares values, as data: `{case: ignore, trim: true}`. Wins over the prose. |

### `failures[].unique`

| key | type | req | meaning |
|---|---|---|---|
| `case` | string |  | `ignore` adds a case variant expecting the refusal; `exact` adds none. |
| `trim` | bool |  | `true` adds the value padded with spaces expecting the refusal; `false` adds none. |

## 4. Config — `.shrt/config.yaml`

| key | type | req | meaning |
|---|---|---|---|
| `target` | target | + | Where chains run. |
| `descriptor` | descriptor | + | Where the proto descriptor lives and what rebuilds it. |
| `auth` | auth |  | The login call, declared once for the whole repo. |
| `paths` | paths | + | Where chains, contracts, runs and safe spots live. |
| `conventions` | conventions |  | How this backend names reads and reports its verdict. Every key optional. |
| `latency` | latency |  | Slowdown detection against the safe spot's run: `floor_ms` more AND `ratio` times as long. |
| `volatile` | list of string |  | Volatile paths applied to every chain. |
| `redact` | list of string |  | Paths blanked in every run record. An explicit list REPLACES the defaults: `**.*password`, `**.access_token`, `**.refresh_token`, `**.token`, `**.*secret`, `**.*pin`, `**.*pin_code`, `**.*passcode`, `**.*otp`, `**.api_key`, `**.authorization`. Known secrets are also scrubbed by value. |

### `target`

| key | type | req | meaning |
|---|---|---|---|
| `base_url` | string | + | Scheme and host of the backend. Redirects are never followed. |
| `host_override` | string |  | Sent as the `Host` header and TLS `ServerName` while connecting to `base_url`. |
| `headers` | map string → string |  | Headers added to every request. Never the auth header when `auth` is declared. |
| `timeout` | string |  | Per-request timeout, e.g. `30s`. Default 30s. A call with no answer in time was still sent. |
| `build_header` | string |  | Response header carrying the server's build, stamped into runs as `build`. |

### `descriptor`

| key | type | req | meaning |
|---|---|---|---|
| `file` | string | + | The compiled `FileDescriptorSet`. |
| `source` | string |  | What `shrt catalog build` compiles. |
| `binary` | string |  | The compiler, normally `buf`. |

### `auth`, and each entry of `auth.profiles`

| key | type | req | meaning |
|---|---|---|---|
| `call` | string | + | The login rpc. |
| `body` | map string → any | + | Its request body. `${env.X}` for credentials, never a literal; only `${env.*}`, `${uuid}` and clock forms resolve here. |
| `token_path` | string | + | Response path holding the token. |
| `expires_path` | string |  | Response path holding the expiry. Without it the token is refreshed only on a 401. |
| `header` | string |  | Defaults to `Authorization`. |
| `scheme` | string |  | Defaults to `Bearer`. |
| `skip_calls` | list of string |  | Calls that carry no token. Read only at the top level of `auth:`. |
| `leeway_seconds` | int |  | Re-login this many seconds before expiry. Defaults to 60. |
| `calls` | list of string |  | Glob patterns of calls this profile owns, e.g. `acme.partner.*`. |
| `profiles` | map string → auth |  | Named additional principals, each with its own token cache; a step picks one with `auth:`. |

### `paths`

| key | type | req | meaning |
|---|---|---|---|
| `chains` | string | + | Chain YAML directory. |
| `contracts` | string |  | Contract overlay directory, one file per domain; only top-level `.yaml`/`.yml` files load. |
| `runs` | string | + | Run record directory. |
| `safespots` | string | + | Safe spot directory. |

### `conventions`

| key | type | req | meaning |
|---|---|---|---|
| `read_only_prefixes` | list of string |  | Rpc-name prefixes that mean a read, matched at a word boundary. Default: Fetch, Get, List, Preview, Search, Read, Query, Find, Lookup, Describe, Show, Count, Export, Download, Retrieve, Watch, Subscribe. |
| `envelope_path` | string |  | Where a response reports its verdict. Default `error.code`. A path no response declares fails `run`. |
| `envelope_ok` | string |  | The `envelope_path` value that means success. Default `OK`. |
| `item_envelope_path` | string |  | Per-item verdict in a batch response, `<list>[].<path>` (e.g. `results[].error.code`); without it a batch refusing every line passes. |
| `code_fields` | list of string |  | Detail fields carrying the backend's own code, searched by `chain which -code`. Default `app_code`, `reason`, `error_code`; an explicit list replaces them. |
| `validate_output` | bool |  | True: a response that does not match its message fails the step (`drift`). Default false: undeclared fields are dropped with a warning. |

### `latency`

| key | type | req | meaning |
|---|---|---|---|
| `floor_ms` | int |  | Milliseconds slower than the safe spot's run before a step can be flagged. Default 250. |
| `ratio` | float64 |  | Times as slow as the safe spot's run before a step can be flagged. Default 3. |
| `remeasure` | int |  | How many times a slow read is re-sent before it is judged (0..10). Default 2. |
| `fail` | bool |  | True: a confirmed slowdown fails `verify`. Default false, a warning; `shrt init` writes `true`. |
| `off` | bool |  | True: no latency comparison. |

## 5. Run record — `.shrt/runs/<chain>/<run-id>.json`

| field | type | meaning |
|---|---|---|
| `format` | int | Record format version, written with the seal. |
| `run_id` | string | Timestamp-prefixed, e.g. `20260911T104434Z-e94560bd`; `-run latest` picks the newest. |
| `chain` | string | Chain name, which is also the run directory and the safe-spot key. |
| `chain_source` | string | Path the chain was loaded from. |
| `chain_digest` | string | Fingerprint of the chain file as it ran, name left out; `confirm -approve` and `-rename-from` compare it. |
| `target` | string | The base_url this ran against. |
| `build` | string | Which build answered: the `run -build` label, else `target.build_header`'s value. |
| `started_at` | time | UTC start time. |
| `duration_ms` | int | Whole-run wall time. |
| `status` | string | `passed`, `failed` or `error`; never `skipped`, which is a step status. |
| `dry_run` | bool | True for a `-dry-run` record, which is never saved. |
| `keep_going` | bool | True for a `-keep-going` run. |
| `replay_of` | string | On a `verify` replay, the safe spot's run id. |
| `vars` | map string → any | The resolved vars this run used; secrets show as `<redacted>`. |
| `exports` | map string → any | Everything any step exported. |
| `volatile` | list of string | Volatile patterns in force for the whole run, chain plus config. |
| `redacted` | list of string | Redact patterns in force. |
| `steps` | list of steprecord | One entry per step, in order. |
| `failure` | string | Why the run stopped, one line per step that did not pass. |
| `failed_steps` | list of string | Under `-keep-going`, every step that did not pass, in order. |
| `warning` | string | A run-level warning, e.g. no response carried `envelope_ok`. |
| `kept_red` | string | For a chain with `kept_red`: `as_pinned` (exit 0), `not_as_pinned` or `defect_gone` (exit 1). |
| `kept_red_note` | string | What `kept_red` found: the pins and what failed or held outside them. |
| `kept_red_new` | string | For `not_as_pinned`: each failure outside the pinned defect. |
| `kept_red_slow` | list of string | Steps flagged slow against the last run that failed as pinned. |
| `seal` | string | Checksum written with the record; commands refuse a record whose content no longer matches it. |

### Each entry of `steps`

| field | type | meaning |
|---|---|---|
| `index` | int | Position in the chain, from 1. |
| `id` | string | The step id. |
| `call` | string | As written in the chain. |
| `procedure` | string | The resolved `/package.Service/Rpc`. |
| `auth_profile` | string | The profile whose token the step carried: `default`, a profile name, `invalid`, or `none`. |
| `auth_principal` | string | Digest of the account the profile logged in as, no secret in it; `verify` compares it. |
| `auth_retry` | string | `resent`: answered unauthenticated, logged in again and re-sent. `not_resent`: a write that may have been performed was not re-sent. |
| `first_attempt` | attempt | A read's first answer when it was a server error; the re-send is judged. |
| `token_refused` | list of tokenrefusal | Each token refused at this step: fingerprint, issued, stated expiry, refused. |
| `status` | string | `passed`, `failed`, `error` or `skipped`. |
| `http_status` | int | Transport status. 200 with a non-OK envelope code is an in-band refusal. |
| `latency_ms` | int | Wall time of the call. |
| `latency_resent_ms` | list of int | Latencies of re-sends of a read that was slow against the safe spot's run. |
| `waited_ms` | int | How long the step waited before it was sent, from `wait`. |
| `request` | JSON | What was sent, after resolution and redaction. |
| `body_refs` | map string → string | Each body field filled from another step or export, with the reference as written; `verify` compares them to detect a rewired chain. |
| `headers` | map string → string | The step's own headers as sent; a credential-like header is stored as a digest. |
| `response` | JSON | What came back, re-encoded through the response message and redacted: proto names, every declared field at its zero value if omitted, int64 as a string. |
| `undeclared` | JSON | Response fields the descriptor does not declare, with the values sent. |
| `transport_error` | transporterror | The Connect error when the backend answered non-200; `transport.*` reads it. |
| `expect` | list of expectresult | One entry per expectation, with the rule that fired and whether it held. The runner adds `envelope` and `item_envelope` entries for an unpinned in-band refusal. |
| `exported` | map string → any | What this step published. |
| `error` | string | Why this step failed or could not run. |
| `warning` | string | Non-fatal runner note: stale descriptor, build change, an unasserted refusal. |
| `note` | string | Runner commentary, e.g. whether a login seeded a profile's token. |
| `volatile` | list of string | Step-level volatile patterns. |
| `unordered` | list of string | The unordered lists the run applied to this step. |
| `drift` | bool | Under `validate_output`, the response did not match its message. |

### Each entry of a step's `expect`

| field | type | meaning |
|---|---|---|
| `path` | string | The path asserted. |
| `rule` | string | Which rule fired. |
| `want` | any | The comparison value, after `${...}` resolution. |
| `got` | any | What was actually there. |
| `passed` | bool | Whether it held. |
| `detail` | string | Why not, when the rule itself was malformed. |

## 6. Safe spot — `.shrt/safespots/<chain>.json`

Written only by `shrt confirm <chain> -approve`. A proposal waits in `.shrt/safespots/pending/<chain>.json`.

| field | type | meaning |
|---|---|---|
| `chain` | string | Which chain this is the ground truth for. |
| `run_id` | string | The run a person approved. |
| `target` | string | Where that run happened. |
| `build` | string | The confirmed run's `build`. |
| `confirmed_by` | string | The email of the user who said yes. |
| `confirmed_at` | time | When it was approved. |
| `note` | string | What makes this run correct. |
| `proposed_by` | string | Who proposed it; `agent` unless `-by` named someone. |
| `proposed_at` | time | When it was proposed. |
| `supersedes` | string | The run id this replaced, under `-supersede`. |
| `volatile` | list of string | The volatile patterns approved with the run; `verify` fails a replay masked by any other. |
| `renamed` | list of rename | Each rename carried by `confirm <new> -rename-from <old>`. |
| `chain_digest` | string | The confirmed run's `chain_digest`. |
| `digest` | string | Fingerprint of the content and the approval; `verify` refuses a safe spot edited by hand. |
| `steps` | list of steprecord | The confirmed step records a replay is diffed against. |

## 7. What `shrt verify` actually compares

| what changed between the confirmed run and the replay | reported? |
|---|---|
| `qty` went from `1` to `2` | **yes** |
| `created_at` changed, and is `volatile` | no |
| `created_at` became null, and is `volatile` | **yes** |
| `note` did not change | no |
| `owner_id` changed, NOT volatile, but its name is id-shaped | no |
| `seen` changed, NOT volatile, but both values are timestamps | no |
| `lines` went from 2 items to 1 | **yes** |
| `qty` went from the STRING `"1"` to the NUMBER `1` | **yes** |

- Besides `volatile` paths, verify masks a value id- or timestamp-shaped on both sides (same kind of id, same unit,
  within 400 days of its run) and a value echoing a fixture name or `${uuid}`; here 2 value(s). `-masked` lists them.
- A value lost under a volatile pattern (null, empty, gone) is still reported.
- Ids are renamed consistently across the record, so a stale id is reported.
- An `unordered` list is compared as a multiset; a `redact` path never.
- First it compares what each step SENT and the chain. A changed input that explains every response change is
  `drift with different input`, a changed chain `drift after a chain change`; anything else is a `regression`.

Change kinds `shrt verify` and `shrt diff` print: `missing` (a path the baseline had is gone), `unexpected`
(a path the baseline did not have), `changed` (same JSON type, different value), `type` (different JSON
type), `length` (a list or the step count has a different number of items), `order` (a step id or rpc
differs at that position), `status` (the step's pass/fail status changed).
Steps pair by id, then by call and position: a renamed or inserted step is one change. A field respelt in
case, as its JSON name, or as `${steps.a.response.x}` for `${a.x}` is no change.

## 8. Closed vocabularies

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

- `error`: no answer to judge, usually nothing sent. Read the step's `error` before blaming the backend.
- `skipped`: a dry-run step, or a `-keep-going` step held back behind one that did not pass.
- `"drift": true` is `failed`: answered, but not as its message under `validate_output`. Rebuild the descriptor first.

## 9. Commands

| command | subcommands |
|---|---|
| `shrt` | `catalog`, `chain`, `confirm`, `contract`, `diff`, `doctor`, `gate`, `init`, `run`, `verify`, `version` |
| `shrt catalog` | `<build\|ls\|describe> [flags]` |
| `shrt chain` | `<new\|ls\|which\|lint\|slice\|pin\|hollow> [flags]` |
| `shrt contract` | `<init\|lint\|show\|plan\|status\|quality> [flags]` |

Every command prints its flags and exit codes with `-h`. A run id is accepted with or without `.json`.

## 10. Volatile and redact patterns

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

`*` globs within a segment; `**.` reaches any depth. Matching folds case and separators.
