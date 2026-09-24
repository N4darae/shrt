# GRAMMAR — every key shrt accepts

**Generated from the Go structs by `go run ./distill`. Do not edit.**
`go run ./distill -check` fails when a key added in code is missing here. Nothing in the module runs it for you, so run it before you commit a change to a key.

Why this file exists: the loaders reject unknown keys (`KnownFields(true)`), but only since
2026-09-11. Before that a misspelled key was silently dropped and `lint` still said `ok` —
`one_of:` instead of a real rule meant a step asserted nothing while reading as checked.
A key not in this file does not exist; the loader names it with its line and the nearest real key:
`unknown key "expects" at line 16 (did you mean "expect"?)`.

A `+` in the `req` column means the key has no `omitempty`: it is always written out, and a
scaffold leaves it present-but-empty rather than absent.

## 1. Chain file — `.shrt/chains/<name>.yaml`

| key | type | req | meaning |
|---|---|---|---|
| `apiVersion` | string | + | `shrt/v1`. Defaults to it when omitted. |
| `name` | string | + | Names the run directory and the safe spot. Defaults to the file name. |
| `description` | string |  | What state this chain reproduces, for the next reader. |
| `vars` | map string → any |  | Referenced as `${vars.x}`. Override per run with `-var x=y`. A chain that reads `${vars.x}` without declaring it here must be given `-var x=...`: `shrt run`, `run -dry-run` and `verify` refuse it before sending anything, naming each missing var. |
| `volatile` | list of string |  | Response paths masked when `shrt verify` diffs against the safe spot and when `shrt diff` compares two runs. Expectations still see the real value. |
| `redact` | list of string |  | Paths blanked in the run record: in each step's request and response, in `vars` and exports, and in each expectation's `want` and `got`. A value exported from a redacted path is also scrubbed wherever else it appears (see `redact` in §2). A redacted response value is blanked in the safe spot and in every replay alike, so `shrt verify` and `shrt diff` never compare it: `confirm` lists those fields under **Redacted, never compared by `shrt verify`**, and `verify` counts them and names each one (`redacted`, `redacted_paths` under `-json`) without failing. Redact only what must not be stored; a business field redacted here is a field no safe spot guards, so assert it in the chain if it matters. |
| `steps` | list of step | + | Ordered. Never reordered or parallelised, and never skipped except as `-keep-going` records it. |

### Step

| key | type | req | meaning |
|---|---|---|---|
| `id` | string | + | Unique; later steps reference it. Derived from the rpc name when omitted. |
| `description` | string |  | Why this step is here and what its assertions mean — the place to record a judgement a reader would otherwise re-derive from the body. |
| `call` | string | + | `package.Service/Rpc`, `Service/Rpc`, or a bare `Rpc` when unambiguous. |
| `body` | map string → any |  | Validated against the proto request message before anything is sent. |
| `headers` | map string → string |  | Per-step header overrides. |
| `expect` | list of expectation |  | Assertions on this step's response. Each entry needs exactly one rule, and nearly always a `path`: an entry with no `path` tests the whole response. |
| `export` | map string → string |  | `name: response.path`. Publishes `${exports.name}` and the bare `${name}`. A name equal to a step id is a lint error, since the bare `${name}` would then mean two things; a name another step also exports is a lint warning (`export-overwritten`, failed by `-strict`), since the later write silently replaces the earlier. |
| `auth` | string |  | Named auth profile from `.shrt/config.yaml`. Contradicts `skip_auth`; lint rejects both. The reserved value `invalid` sends a token the backend never issued, in the header and scheme of the profile that would otherwise cover the call, and never re-logs in on the 401 — the probe for "an invalid token is refused". Lint rejects it with `export`; `shrt run` refuses it when the config declares no auth. |
| `skip_auth` | bool |  | Attach no auth header. For the probe "a missing token is refused". A login step does not need it: a call to a configured login rpc (or one listed in `auth.skip_calls`) never carries a token, and its run record says `auth_profile: none`. |
| `allow_fail` | bool |  | Tolerate the backend REFUSING this call, when a later step depends on the attempt rather than the outcome: a step the backend refused at the transport level (a Connect error) does not stop the chain, provided it declares no expectation — a refusal leaves every expectation unevaluated, and an unevaluated expectation is never tolerated. An in-band refusal whose expectations hold is simply `passed` and needs no key. It covers nothing else — not a failed expectation, not an expectation a transport error left unevaluated, not drift, not a step whose status is `error` — and every one of those still stops the run. One more case slips through: a step answered 200 whose `export` path is missing is `failed` with no failed expectation, and `allow_fail` lets the chain go on past it. To see what lies beyond a step that does not pass, use `shrt run -keep-going`, not this key. A step MEANT to be refused at the transport level asserts which refusal with `transport.*` and needs no key. On a step that declares any expectation it does nothing, and `chain lint` warns `inert-allow-fail`, which `-strict` fails. |
| `volatile` | list of string |  | Volatile paths for this step only, added to the chain's. |

### Expectation — exactly one rule per entry (lint-enforced since 2026-09-11)

| key | type | req | meaning |
|---|---|---|---|
| `path` | string | + | JSON path into this step's own response, or one of the reserved `transport.*` paths (table below), which read the recorded transport result instead. `${...}` here is a lint ERROR: a path names a location, not a value. So is a path the response message has no field for, under every rule — `exists: false` included, since it could not fail. Field names match with case and separators folded (`qtyOnHand` reads `qty_on_hand`), and a path that matches only that way is a lint warning (`inexact-path`) naming the exact field; an `export` path is checked the same way. |
| `equals` | any |  | Compared as text, so `1` matches `"1"`. May carry `${...}`: an earlier step, or this step's own request (`${steps.<this>.request.<field>}`); this step's own response is a lint error. |
| `not_equal` | any |  | The path must be present AND differ. An absent path FAILS it, with `path not present in response` — use `exists: false` when absence is what you mean. May carry `${...}`. |
| `contains` | string |  | Substring of the value's text. May carry `${...}`. |
| `exists` | bool |  | Whether the server SENT the path. Read against the populated fields of the response, not the stored record, which materialises every declared field at its zero value. See the second table in §1. |
| `not_empty` | bool |  | Present and not `""`, `0`, `false`, `[]` or `{}`. `0` means zero of every numeric type, including an int64 or uint64, which the record stores as the string `"0"`. |

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
| `not_empty: true` on an int64 at 0 (stored as the string `"0"`) | `not_empty` | no |
| `not_equal: ""` on an int64 at 0 | `not_equal` | no |
| `exists: true` on a path that is absent | `exists` | no |
| no rule at all | `invalid — expectation has no rule` | no |
| TWO rules on one entry: `equals: NOPE` **and** `not_empty: true` | `not_empty` | **yes** |

Read the last six rows together. `not_empty` is false for `0` and `[]`, so it cannot stand in for
`exists`. Zero follows the field's proto type: an int64 or uint64 is stored as a JSON string, and
its `"0"` is zero exactly as an int32's `0` is; on any numeric field `""` in `equals` or `not_equal`
means that zero. A rule-less entry fails loudly rather than passing quietly. And the LAST row is the one
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
One that is known not to resolve is refused before anything is sent, by `shrt run` and as a lint
error: a step or export that does not exist or runs later, an unset `${env.*}`, and a field of an
earlier step's response that its response message does not declare (`${create_product.product.id_prodct}`,
lint kind `unproducible-reference`). If the field is real and new, the descriptor is stale: `shrt catalog build`.

There is no escape for a literal `${`: `$${x}` is a `$` followed by the resolved `${x}`. A value that
must carry `${` comes in through `${env.NAME}` or `-var name=...`, whose values are never resolved again.
Lint warns (`reference-syntax`) on forms that are accepted but do not do what they read as: spaces
inside the braces (`${ uuid }` resolves as `${uuid}`), a fractional clock offset (`${now+1.5}` resolves
as `${now+1}`), a path after `uuid` or a clock form (ignored), and a `${` never closed, which is sent as
literal text.

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
| `${steps.create_deal.request.id_book}` | `"b-1"` | a field of an earlier step's REQUEST, or inside `expect` this step's own, to assert the response echoes what was sent |
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
| `required` | list of string | + | Fields the server actually rejects without. proto3 has no `required`, so this comes from reading the backend. Applies to reads as well as writes. The literal `NONE`, alone, declares that the server rejects nothing; leaving the list empty on an rpc that takes request fields is scored as an omission, so silence and `NONE` cannot look the same. The literal `UNKNOWN`, alone, is the honest answer when the handler could not be found — it lints as a warning rather than an error, and is scored exactly as an empty list is, because not knowing must not be cheaper than knowing. A scaffold writes `['TODO: …']` here as a value, not a comment; that entry is not a field name, and it is read as an empty, unfilled `required` that lint warns on. |
| `needs` | list of string |  | An rpc that must run first but whose output no field consumes. |
| `no_producer` | string |  | Why no write rpc in this API puts the rows this read returns there — a seed, a migration, an external feed. It is the ONLY thing that spares the read-with-no-producer charge; a general-purpose `note` deliberately does not, because an exemption anyone can buy with one sentence measures nothing. Meaningless on a write rpc. |
| `before` | list of string |  | The inverse of `needs`, declarable from the side that owns the prerequisite. Takes rpc names only and always pulls in the unaliased node; to order an alias, list it in the dependent rpc's `needs:`. |
| `fields` | map string → fieldcontract |  | Per request field. Dotted keys reach into nested messages. After a repeated field an optional index picks one entry: `lines.1.id_product`. An unindexed key (`lines.qty`) applies to every entry, and an indexed key overrides it for that entry. `plan`, `contract show` and `chain new` build one entry per index up to the highest declared (at most 100). Lint errors on an index after a non-repeated field, a leading zero, or an index of 100 or more, and warns when entries below the highest index are declared by nothing. |
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
| `note` | string |  | Units, formats, constraints. Prose only: it never changes `plan` output, and it does not mark a scaffold zero as deliberate; `value: "0"` does. |

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
| `redact` | list of string |  | Paths blanked in every run record, and so never compared by `shrt verify` (see `redact` in §1). Credentials belong here. Without the key the defaults apply, and `shrt init` writes them out: `**.*password`, `**.access_token`, `**.refresh_token`, `**.token`, `**.*secret`, `**.*pin`, `**.*pin_code`, `**.*passcode`, `**.*otp`, `**.api_key`, `**.authorization`. A bool is never masked, and neither is an empty value (`""`, 0 of any numeric type — an int64's `"0"` included —, null, `[]`, `{}`): masking it would hide that nothing was sent. Paths are not the only guard: the runner also scrubs by VALUE, replacing with `<redacted>`, wherever it appears in the record (request, response, each expectation's `want`, `got` and detail, errors, warnings, notes, exports), every value an auth body reads from `${env.*}` (username included), every token a login returned, and every value a step exports from a redacted path, so the proposal report and the safe spot built from the record never carry them either. A value shorter than 4 characters is replaced only where it is the whole string. An explicit list REPLACES the defaults rather than adding to them, so a config written before a default was added does not get it — add the pattern by hand. |

### `target`

| key | type | req | meaning |
|---|---|---|---|
| `base_url` | string | + | Scheme and host of the backend. Redirects are never followed: a 3xx answer, to the login call or any other, is a transport error naming its `Location`, so no request body, credential or token is re-sent elsewhere. Point `base_url` at the final address. |
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
| `body` | map string → any | + | Its request body. `${env.X}` belongs here, never a literal credential. Resolved before any step runs, so only `${env.*}`, `${uuid}` and the clock forms work; `doctor` and `chain lint` reject `${vars.*}`, exports and step references. When a step of a chain runs under this profile and one of its `${env.*}` is unset, `shrt run` refuses the chain before sending anything and `chain lint` warns, since the login would fail after earlier steps had run. |
| `token_path` | string | + | Response path holding the token. |
| `expires_path` | string |  | Response path holding the expiry. Without it the token is refreshed only on a 401, and a 401 re-sends only a read (see `auth_retry` in §5): a write answered 401 fails its step. |
| `header` | string |  | Defaults to `Authorization`. |
| `scheme` | string |  | Defaults to `Bearer`. |
| `skip_calls` | list of string |  | Calls that carry no token. Read only at the top level of `auth:`, where it applies to every profile; inside an entry of `auth.profiles` it is accepted and ignored. |
| `leeway_seconds` | int |  | Re-login this many seconds before expiry. Defaults to 60. |
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
| `envelope_path` | string |  | JSON path at which a response reports its own verdict. Default `error.code`. Set it to MOVE the envelope, never to remove it: an explicit empty value is indistinguishable from an absent key and falls back to the default. A backend with no in-body envelope needs no setting — a response carrying no field of that name gets a scaffolded assertion on a real response field instead. A path set here that no response message in the descriptor declares fails `shrt run` before any traffic is sent, as `shrt doctor` fails it. |
| `envelope_ok` | string |  | The `envelope_path` value that means success. Default `OK`. A run in which responses carried the envelope but none carried this value ends with a `warning` naming the values seen; a batch whose items "refuse" with the very value the top-level envelope carries says to check this key. |
| `item_envelope_path` | string |  | Per-item verdict in a BATCH response, as `<list>[].<path>` (e.g. `results[].error.code`). A batch rpc can answer `OK` at the top level while refusing every line; without this the runner cannot see that, and a step asserting only the envelope passes having achieved nothing. Unset means the backend has no per-item envelope. Checked only on rpcs whose response message declares that list with that field, so a list of atomic receipts carrying no verdict is left alone. An item whose verdict is missing (its envelope unset or absent) is success when no item of that batch carries `envelope_ok` explicitly — a backend that writes an item's error only on refusal — and is reported like a refused item, as `(no verdict)`, when another item of the same batch does; a refusal the step pins with `equals`, `not_equal` or `contains` on that line's verdict path, or on one of that line's code fields (`conventions.code_fields`), is declared, not reported, while `exists` and `not_empty` declare nothing; a path no response message declares fails `shrt run` before any traffic is sent. |
| `code_fields` | list of string |  | Detail-field names that carry a backend's OWN numeric or symbolic code, searched by `shrt chain which -code`. Default `app_code`, `reason`, `error_code`. An explicit list REPLACES the defaults. The envelope's own leaf is not listed here — it follows `envelope_path`, so a deployment answering at `status.code` is searched there without any setting. Nothing enforces these names; a code this list cannot reach makes `chain which` answer "no chain asserts it" for a corpus that does. |
| `validate_output` | bool |  | When true, a response that does not match its proto message FAILS the step. Default false: the response is kept as sent and a warning is recorded, so a descriptor that has drifted from the deployed binary degrades quietly rather than failing every chain. Turn it on once your descriptor build and your deploy are in step. |

## 5. Run record — `.shrt/runs/<chain>/<run-id>.json`

The evidence file. `README.md`'s authority table points here for "does this code really fire",
so these are the fields that answer it. Reflected from `runner`, JSON names:

| field | type | meaning |
|---|---|---|
| `run_id` | string | Timestamp-prefixed, e.g. `20260911T104434Z-e94560bd`. `-run latest` picks the newest by that prefix; runs started in the same second are ordered by `started_at`, then by file time. |
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
| `volatile` | list of string | Volatile patterns in force for the whole run, chain plus config. Step-level patterns are on each step record. |
| `redacted` | list of string | Redact patterns in force. The values themselves are already masked in `request`/`response`. |
| `steps` | list of steprecord | One entry per step, in order. |
| `failure` | string | Why the run stopped, when it did. Under `-keep-going`, one line per step that did not pass; when a step could not connect to the target at all (connection refused, a dial or DNS failure — not a Connect error), every later step is recorded `skipped` unsent and this carries ONE line naming the unreachable target, instead of one per step. |
| `failed_steps` | list of string | Under `-keep-going`, the id of every step that did not pass, in order — failed, error, and skipped behind one of those. `status` is the FIRST such step's status, the same verdict the run would have had without the flag. |
| `warning` | string | A run-level warning. Today: every response carrying `conventions.envelope_path` had a value other than `conventions.envelope_ok` (refusals a step asserted with `equals`, and steps asserting `transport.*`, aside). When the values seen do not look like verdict codes it points at `envelope_path`; when some look like codes that are not refusals it points at `envelope_ok`; when all look like refusals (REJECTED, PERMISSION_DENIED, ...) it stays silent, since that is a refused principal, not a config problem. It names the values seen, quoting any that are not code-shaped. |

### Each entry of `steps`

| field | type | meaning |
|---|---|---|
| `index` | int | Position in the chain, from 1. |
| `id` | string | The step id. |
| `call` | string | As written in the chain. |
| `procedure` | string | The resolved `/package.Service/Rpc`. |
| `auth_profile` | string | The auth profile whose token this step carried: `default`, a name under `auth.profiles`, `invalid` for a step with `auth: invalid` (a token the backend never issued), or `none` when no token was attached (`skip_auth`, a login rpc, `auth.skip_calls`). Absent when the config declares no `auth:` block and in a dry run. Two steps that should act as one principal and show different values here are a principal swap. `shrt verify` compares it with the safe spot's value for the same step as part of the input (§7): a step that now runs under another profile is reported as `request differs ... auth_profile (default -> clerk)` and fails verify with `drift with different input`, even when every response matches. A record that does not say which profile ran (no `auth:` block, or recorded before this field) is not compared. |
| `auth_retry` | string | Set when the call was answered unauthenticated (HTTP 401, or `unauthenticated` at the envelope path) and the token dropped. `resent`: the call is a read (`conventions.read_only_prefixes`), so a fresh login was made and it was sent again; the backend received it twice and this record is the second answer. `not_resent`: it is not a read, so it was NOT sent again, because the backend may already have performed it and a second send could perform it twice; the step shows the refusal, and the next call logs in fresh. Both carry a `warning`. |
| `status` | string | `passed`, `failed`, `error`, or `skipped` — every step of a `-dry-run` that resolves and validates is `skipped`, and so is a `-keep-going` step that was not sent because it reads the response (`${steps.X…}`, `${X.…}`) or an export of a step X that did not pass; a reference to X's request does not hold it back, nor does a reference to a response field of an answered X whose failed expectations do not cover that field. Its `error` says what happened to X: a failed assertion, a refusal, or an error. A `-keep-going` step after one that could not connect to the target at all is `skipped` too, its `error` naming the unreachable target. |
| `http_status` | int | Transport status. 200 with a non-OK `error.code` in the body is the normal shape of a business refusal. |
| `latency_ms` | int | Per-step wall time. |
| `request` | JSON | What was sent, AFTER reference resolution and redaction. |
| `response` | JSON | What came back, RE-ENCODED through the response message and then redacted — not the wire bytes. Field names are the proto ones, every declared scalar and list field is present at its zero value if the server omitted it (an unset nested message is `null`, so assert `exists: false` on the message itself rather than on a path inside it), and an int64 is a JSON string whatever the server sent. That is what gives `shrt verify` a stable shape to diff across runs, and it is why a scalar's absence cannot be read out of this field: see the second table in §1 on `exists`. When the descriptor cannot decode the body it is stored as sent instead, and the step carries a `warning` saying so, so the shape of this field depends on descriptor freshness. |
| `transport_error` | transporterror | Set when the backend answered with a Connect error (any non-200) instead of a response message. `transport.code` and `transport.message` read it. |
| `expect` | list of expectresult | One entry per expectation, with the rule that fired and whether it held. Read this, not just the step status. The runner may append entries of its own: `item_envelope` for a batch line refused unannounced, and `envelope` for a step that declares expectations, was refused in-band (the envelope code is not `envelope_ok`), or answered with no verdict (the envelope absent or empty, though the response message declares it), and has no expectation pinning the verdict (an `equals` on the envelope, or a rule on the envelope or a `transport.*` path that would fail on a successful answer; `not_equal: ""` or `not_equal` a misspelt code holds on the refusal and on the ok value alike and pins nothing) — that step is `failed`, because the assertions that held read the zero values a refusal leaves. |
| `exported` | map string → any | What this step published. |
| `error` | string | Why this step failed or could not run. |
| `warning` | string | Non-fatal note from the runner: a stale descriptor, a build change mid-run, or a step that declares no expect but was refused in-band (the envelope code is not `envelope_ok`) or answered with no verdict, which stays `passed` with a warning saying so. |
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

Written only by `shrt confirm <chain> -approve`. Before that, `shrt confirm <chain> -note` leaves a proposal at
`.shrt/safespots/pending/<chain>.json` and its review report at `pending/<chain>.md`; neither is a safe spot,
and both are removed on approval or `-reject`.

| field | type | meaning |
|---|---|---|
| `chain` | string | Which chain this is the ground truth for. |
| `run_id` | string | The run a person approved. |
| `target` | string | Where that run happened. |
| `build` | string | The confirmed run's `build`, when it had one. `shrt verify` prints it beside the replay's. |
| `confirmed_by` | string | The email of the user who said yes. `shrt confirm -approve` refuses a `-by` that is not an email address. |
| `confirmed_at` | time | When. |
| `note` | string | What makes this run correct: the approver's `-note`, else the proposer's. |
| `proposed_by` | string | Who proposed the run with `shrt confirm -note`; `agent` unless `-by` named someone. |
| `proposed_at` | time | When it was proposed. |
| `supersedes` | string | The run id this replaced, when proposed with `-supersede`. |
| `volatile` | list of string | The volatile patterns approved with the run (config and chain), masked before comparison together with each step's own `volatile`. A replay masked with any other pattern, one added to the config or chain after approval, fails `shrt verify`, which names each such pattern and every value it hid, until a run under the wider mask is proposed with `-supersede` and approved. |
| `digest` | string | Fingerprint of the chain, run id, target, build, volatile patterns and full step records. `shrt verify` refuses a safe spot whose content no longer matches it (a hand edit): restore the file or re-approve with `-supersede`. A safe spot approved before 2026-09-24 carries the older digest of step ids, calls and responses, which is still checked. |
| `steps` | list of steprecord | The confirmed step records, which a replay is diffed against. |

## 7. What `shrt verify` actually compares

Produced by running `diff.Compare` on a fabricated safe spot and replay:

| what changed between the confirmed run and the replay | reported? |
|---|---|
| `qty` went from `1` to `2` | **yes** |
| `created_at` changed, and is `volatile` | no |
| `note` did not change | no |
| `owner_id` changed, NOT volatile, but its name is id-shaped | no |
| `seen` changed, NOT volatile, but both values are timestamps | no |
| `lines` went from 2 items to 1 | **yes** |
| `qty` went from the STRING `"1"` to the NUMBER `1` | **yes** |

The last row is reported as a `type` change rather than a `changed` one, and the report
names both kinds — `want=string "1" got=number 1` — because printing `want=1 got=1` reads
like a false positive. Until 2026-09-22 it was NOT reported at all: scalars were compared
as formatted text, so a money field that started arriving as a number instead of a string
passed `verify` silently. `1.0` against `1` is still clean, because JSON has no separate
integer type and both decode to the same float64.

Beyond `volatile`, verify also masks values that differ every run by shape: a changed value
whose field name is id- or timestamp-shaped (`id`, `*_id`, `id_*`, `*Id`, `*_at`, `*At`, `*_time`,
`token`, `idempotency_key`, ...), or where both values are timestamps or both are UUIDs, is not
reported; the report says how many it masked. In this example 2 value(s) were masked. A name alone
is not enough: both values must look alike, two non-zero numbers or two non-empty strings of the
same shape (`ord-819dac5f23ba` and `ord-0123456789ab`), including the same letters before the first
separator, so an id of another kind (`cus-...` became `prd-...`) is reported. An id that became `""`,
null, `0`, `undefined`, a number where a string was, or disappeared, is reported too. `shrt verify
-masked` lists every masked value, volatile or shape-masked, with its path and both values. Declare
a path `volatile` when its value changes every run without being id- or timestamp-shaped.

Before the responses, verify compares each step's recorded REQUEST with the safe spot's and prints
every difference first, as `request differs from the confirmed run at <step> <path> (a -> b)`. A
request value the chain builds from another step's output or from `${uuid}` / `${now}` differs
every run and is skipped; a literal, a `${vars.x}` or an `${env.X}` is input, and so is the step's
`auth_profile`: a step that now runs as another principal is reported at `<step> auth_profile` and fails
verify with `drift with different input` even when every response matches. When the input
differs, the response changes are reported as coming with different input, not as a backend
regression, and verify fails with `drift with different input` instead of `regression`.

Change kinds `shrt verify` and `shrt diff` print: `missing` (a path the baseline had is gone), `unexpected`
(a path the baseline did not have), `changed` (same JSON type, different value), `type` (different JSON
type), `length` (a list or the step count has a different number of items), `order` (a step id or rpc
differs at that position), `status` (the step's pass/fail status changed).

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

Two of these need care. **`error`** means the step produced no answer to judge. Usually no
request was sent: a reference or auth body would not resolve, or the login failed. Less often the request
went out and the transport failed, or the answer was not JSON, or its per-item verdict path could not be
read. Read the step's `error` before blaming the backend. **`skipped`**
is the status of every step in a `-dry-run` that resolves and validates, and of a `-keep-going` step held back because it reads a
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
  confirm    propose a passing run as its chain's safe spot, then approve or reject it once the user decides
  contract   author and use the curated RPC contracts agents write chains from
  diff       compare two recorded runs of a chain step by step, with no safe spot
  doctor     check this repo's .shrt/ installation: docs, descriptor, ignores, tokens, auth
  init       set up .shrt/ and the Claude agent kit in the current repo
  run        replay a chain against the target and record the result
  verify     replay a chain and diff it against its safe spot
  version    which build this is: version, commit, and the docs it carries

run 'shrt <command> -h' for command flags
```

| group | subcommands |
|---|---|
| `shrt catalog` | `<build\|ls\|describe> [flags]` |
| `shrt chain` | `<new\|ls\|which\|lint\|slice\|hollow> [flags]` |
| `shrt contract` | `<init\|lint\|show\|plan\|status\|quality> [flags]` |

Every command prints its own flags with `-h`. `run` and `verify` take `-var key=value`
(repeatable) and `-json`; `verify` also takes `-run <id>`, which re-diffs a recorded run
**without touching the backend** — the one way to investigate a drift on a live-run budget.
`verify` replays as `-keep-going` does, so every step a failure does not block is still compared; a
step held back behind one, or never reached by a recorded run that stopped early, is reported
`not_reached` rather than as a change of length, and the report names the first failing step.

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
separators and case (`namecase`), so one pattern covers `access_token` and `accessToken`
alike. That is what makes `**.*password` in `.shrt/config.yaml` cover every password-shaped
field whatever it is called — and why a redact pattern written without a `*` can silently
mask nothing while looking careful.
