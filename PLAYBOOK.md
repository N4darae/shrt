# PLAYBOOK

Procedures. Keys live in `GRAMMAR.md`; do not restate them here, look them up there.

---

## 1. Compose a chain — the fast path (a contract exists)

```bash
shrt contract show DealActionService/ConfirmDeal      # read before you generate
shrt contract plan DealActionService/ConfirmDeal      # preview the order
shrt contract plan DealActionService/ConfirmDeal -write
```

`plan` walks `needs`, `before`, `from` and `same_as` transitively, topologically sorts them, and
emits a chain with every `${...}` already wired. Ahead of that chain it prints a header you must
read — and the header is written as YAML **comments**, because everything after it is the chain
file. This is the complete header of a short plan, captured 2026-09-17 from
`shrt contract plan acme.pricing.dailybook.v1.DailyBookSummaryActionService/CloseBookDay`; the
`ConfirmDeal` plan above prints 20 `# note:` lines, 22 `# ` lines in all:

```
# order: CreateBook -> RunDayEnd -> CloseBookDay
# note: step create_book: code is required and has no usable value — fill it
# note: step create_book: name is required and has no usable value — fill it
# note: step create_book: caller must hold role MANAGER or ACCOUNTING or ADMIN
# note: step run_day_end: business_date is required and has no usable value — fill it
# note: step run_day_end: caller must hold role MANAGER or ACCOUNTING or ADMIN
# note: step close_book_day: caller must hold role MANAGER or ACCOUNTING or ADMIN
# 3 required field(s) carry no test data yet — chain lint ERRORs on each until filled. The plan derives order and wiring; the values are yours.
```

- **The `# order:` line is the claim to check.** `plan` wires what the contracts say to wire; it
  cannot tell that a movement in one asset should not discharge an obligation in another. A plan
  that lints clean and runs green can still reproduce a meaningless state. Grep it as
  `'^# order:'` — the `# ` is part of the line, and so is the single space after `note:`. That
  is the printed form; with `-write` the same lines go to the terminal as `wrote <path>`, then
  `  order: ...` and `  note: ...`, indented two spaces with no `#`, so grep `'^  order:'` there.
- **`plan` emits ten kinds of `# note:` and only one of them is test data you owe** — the
  `is required and has no usable value — fill it` kind. Read the others rather than skimming past:
  `has no contract, its body is a bare scaffold`, `wants X but that rpc is not in the plan`,
  `caller must hold role`, and above all `required is an unfilled TODO, so this plan cannot say what
  the server rejects without — treat the body as unverified`. That last one means the plan is
  guessing, which is the opposite of test data you owe. Ten was the count on 2026-09-17 and it
  grows whenever `plan` learns to say something new, so read every note that is not the
  fill-it kind rather than matching the ones listed here.
- **In a repo whose hook rejects new comment lines, use `-write`.** The printed plan's header is
  `# ` lines, so pasting or redirecting stdout into a chain file adds comment lines the hook
  refuses. `-write` writes the chain with no comment lines and prints the header to the terminal
  instead (the `  order:` / `  note:` form above).
- **Compose a whole flow from the contract, not one rpc at a time:**
  `shrt contract plan ConfirmOrder FetchOrder ListOrders CancelOrder@confirmed -write`. Each target
  may carry `@alias`, and the aliased step is built with that alias's field overrides (an undeclared
  alias is refused with the list of declared ones). All targets go into one chain in dependency
  order, and a node reached twice appears once.
- If you find yourself adding a step by hand, the contract is missing an edge — fix the contract,
  then re-plan. That is the difference between composing one chain and making every future chain
  compose itself.

## 2. Compose a chain — no contract yet

```bash
shrt catalog ls -filter <word>
shrt catalog describe acme.billing.invoice.v1.InvoiceService/CreateInvoice
shrt chain new -name invoice-happy-path AuthService/Login InvoiceService/CreateInvoice
```

Then write the contract for what you just learned (§7). A chain hand-built and thrown away teaches
nothing; the same work written into the overlay composes the next ten chains.

## 3. Fill the bodies

`plan` reports a field as unfilled when its value is one the server will reject, which is more than
"empty": the zero enum member (`*_UNSPECIFIED`), a `"0"` int64 placeholder, a zero number, and a
repeated field whose entries are all blank all count as **no usable value**.

**`shrt chain lint` is deliberately laxer than `plan` about one case, so do not read a green lint
as "the header is dealt with".** Both sides apply the same usable-value test, at
different strictness: `plan` reads a body **it just generated**, where a `"0"` can
only be the scaffold's own filler, so it says fill it; `lint` reads a body **a human edited**,
where `"0"` may be meant — `short_limit_qty: "0"` in
`dealing-createdeal-positionlimit-enforcement-probe` is a real limit of zero, and an int64 zero and
the scaffold's filler are the same bytes. Everything that cannot be deliberate — `""`, the zero
enum member, an empty or all-blank list — both sides reject. So the plan header is the only thing
that will ever tell you about a leftover numeric zero, and it does so for every field: a field in
`required:` gets `is required and has no usable value — fill it`, and any other field still holding
the scaffold's 0 gets the softer `still carries the scaffold's numeric zero and is not in required`.
A field `note:` does not silence it: a note describes the field, not your test data. Say a 0 is
deliberate with `value: "0"`; `from:`, `same_as:` or listing the field in `required:` also silence
it. `required: [lines]` does not silence `lines.qty`. Nothing downstream repeats either note.

Three habits that keep a chain re-runnable:

| need | write |
|---|---|
| an idempotency key | `${uuid}` — never a literal, or the second run collides with the first |
| a value two steps must share | one chain var, referenced twice — never two literals that a later edit can desynchronise |
| a name or code that must be fresh per run | `${vars.tag}` interpolated, and pass `-var tag=...` at run time |

The `-var` habit is what lets one chain run twice on the same box without tripping a uniqueness
constraint, and it is why a sweep over the corpus generates a random tag per chain. `shrt run`
refuses a `-var` the chain never reads (a mistyped name would otherwise silently collapse every
run onto one key), so a sweep passes `-var tag=...` only to the chains that read `${vars.tag}`:
check with `grep -l 'vars.tag' .shrt/chains/*.yaml`, or read the refusal, which lists the vars the
chain does read.

## 3b. Tell shrt how YOUR backend answers

`.shrt/config.yaml` carries a `conventions:` block. Six keys, all optional, and the defaults
describe a Connect-style backend that reports its verdict at `error.code` with `OK` meaning success.
The block below is NOT the defaults: it is an example for a backend that answers
`{"status": {"code": "SUCCESS"}}`, with every key set to show its shape. It carries no comment
lines, so it pastes into a repo whose hook rejects them:

```yaml
conventions:
  read_only_prefixes: [Fetch, Get, List, Query, Read]
  envelope_path: status.code
  envelope_ok: SUCCESS
  item_envelope_path: results[].error.code
  code_fields: [app_code, reason, error_code]
  validate_output: true
```

Key by key: `read_only_prefixes` says which rpc names are reads; `envelope_path` is where a
response states its verdict, and `envelope_ok` the value there meaning success;
`item_envelope_path` is a BATCH rpc's per-item verdict; `code_fields` are the detail fields
`chain which -code` searches; and with `validate_output: true` a response the descriptor rejects
FAILS its step.

**`item_envelope_path` is the one to check first on any backend with batch rpcs.** A batch call can
answer `OK` at the top level while refusing every line it was given; unset, a step asserting only the
envelope passes having achieved nothing, and `chain hollow` cannot see it either because the response
is not empty. Point it at the per-item verdict and the runner fails the step, naming each refused
item. Leave it unset only when your batch responses are genuinely atomic — the list is empty whenever
the envelope is non-OK — which is a property to verify rather than assume.

**Verify `item_envelope_path` once, against a response you know was refused, before trusting it.**
A path whose list resolves but whose item field does not now fails the step loudly — but a path that
matches nothing in a given response is silent by design, because most rpcs are not batches. Green is
only evidence once you have seen it go red.

**The check follows the descriptor, and a refusal you assert is not a surprise.** The runner applies
`item_envelope_path` only to an rpc whose response message declares that list with that field, so an
atomic batch whose receipts carry no verdict — `results[] {id_deal, ...}` — is left alone even when a
non-atomic rpc on the same backend answers `results[] {error, ...}`; one config serves both. A
negative step that pins a line's own verdict — `path: results.0.error.code, equals: not_found` — has
declared that refusal, and so has an `equals` on one of that line's code fields
(`results.0.error.details.0.app_code`); the runner reports only the lines the step did not pin. `exists` and
`not_empty` on that path declare nothing, since both hold for `OK` and for a refusal. A path that no
response message declares fails `shrt run` before any traffic is sent: a convention that can never
fire is a config error, not a quiet pass.

`GRAMMAR.md` §4 is the key table. `shrt init` prints every key when it writes the config, with its
default, except `item_envelope_path` and `validate_output`, which it prints with the value you
would set (their defaults are unset and `false`). When the descriptor's responses carry a
verdict field somewhere other than `error.code`, init prints that detected path instead, says the
default is not carried, and leaves `envelope_ok` for you to fill. It writes no comment lines into the file, so a repo whose pre-commit hook blocks new
comments can commit it as written.

## 4. Assert something that can fail

The examples in this section and §5 use the default envelope, `error.code` with `OK` for success.
Substitute your `conventions.envelope_path` and `conventions.envelope_ok` (§3b) wherever they appear.

A step whose only expectation is `error.code == OK` asserts that the server did not crash. Say what
the call *did*:

```yaml
expect:
  - path: error.code
    equals: OK
  - path: rows.0.qty_on_hand
    equals: ${vars.get_qty}                                   # against the input
  - path: total_cost_base
    equals: ${steps.before_the_deal.response.total_cost_base}  # against an earlier step
```

A `${...}` in `equals` / `not_equal` / `contains` resolves at run time, which is what lets a chain
state an invariant — *after == before*, *side A == side B*, *give + fees == get + margin* — instead
of a hand-typed number that only encodes what its author expected.

A `${...}` in `path` is a lint error: a path names a location in this step's own response, so there
is nothing for it to resolve to.

Rules and their exact behaviour are in `GRAMMAR.md` §1, whose truth table is generated by running
them rather than describing them. The one that catches people — `not_empty` written where `exists`
was meant — is `PITFALLS.md` §3.

**The read that passed and found nothing is the version of this you cannot see by reading the
chain.** `error.code == OK` on a read rpc — one whose name starts with a prefix in
`conventions.read_only_prefixes` (default list in `GRAMMAR.md`) — whose body came back empty
(every field at its zero value: `""`, `0`, `false`, `null`, `[]`, `{}`, or a list whose every item is
itself all zeros, like `products: [{}]`) is green about the opposite of what its author meant. `shrt chain hollow` reads the run records and
names every one:

```bash
shrt chain hollow            # exit 1 while any is unexplained, exit 2 if there are no records at all
```

Fix one by asserting what the read should have found. A probe that pins a non-OK envelope value (or
`not_equal` the OK value), and a read asserting `<list>.0 exists: false`, already say an empty body
is the answer and are not reported. For any other case where empty is right — a cap, a filter that
rejects a bad id — say so in `.shrt/hollow-allow.txt`, one line per step as
`<chain> <step-id> <reason>`; an entry without a reason is
refused. `PITFALLS.md` §24. The summary's `asserting only the envelope verdict` count is the reads
whose expectations touch nothing but the envelope; a refusal probe that pins a detail code field
under it (`error.details.0.app_code`, `reason`) asserts the refusal's detail and is not counted.

## 5. Probe one failure code

The unit of failure coverage is a `(rpc, app_code)` pair that some run record has actually
observed. No shrt command computes that coverage yet: there is no table of declared pairs against
observed ones. Approximate it one pair at a time with `shrt chain which -rpc R -code C`: an
`OBSERVED` line whose step passed is a covered pair, `asserted` alone means a chain claims it but
no local run has reached it, and an error means nothing asserts it — unless run records carried
the code on a step that asserts something else, which `chain which` then lists (§10). Loop it over
the `failures:` your contracts declare to get the whole list. To turn a declared code into an
observed one:

```yaml
    - id: confirm_twice
      description: |
        The second confirm must be refused. Names the code in the assertion so the receipt
        distinguishes "refused for the right reason" from "refused for any reason".
      call: acme.orders.order.v1.OrderActionService/ConfirmOrder
      body: {id_order: "${create_order.id_order}"}
      expect:
        - path: error.code
          not_equal: OK
        - path: error.details.0.app_code
          equals: "1242"
        - path: error.details.0.reason
          equals: OrderAlreadyConfirmed
```

`error.code` and `OK` above are the defaults. On your backend they are `conventions.envelope_path`
and `conventions.envelope_ok` — a backend answering `result.code: SUCCESS` writes `path: result.code`
and `not_equal: SUCCESS`.

- The step needs no `allow_fail`: an in-band refusal that matches these expectations passes on its
  own, and `allow_fail` on a step that declares expectations does nothing. `allow_fail` only tolerates a transport-level refusal on a step
  that asserts nothing: the step stays `failed` and the chain goes on past it. It never waives a
  failed or unevaluated expectation, and it never lets a chain run past a step whose status is
  `error`. To see what lies behind the first red, run
  `shrt run -keep-going <chain>`: every step still runs, a step that reads a failed step's
  response or exports is recorded `skipped` instead of being sent, and the run stays failed with
  every red step listed. One exception: when the failed step was answered and only its
  expectations failed, a step that reads a response field none of those failed expectations
  covers (the id, when the total was wrong) is still sent.
- Assert on `error.details.0.app_code` **and** `reason` — but **only for a named business failure**.
  `error.code` alone is a Connect code that a dozen unrelated refusals share.
- **A shape error has no `app_code` and no `reason` to assert on.** A refusal raised by request
  validation or by auth, before any business rule ran, usually carries only a code and a message:
  `details` comes back `[]`. Such failures can be a large share of the declared surface, so count
  the declared failures with no numeric `code` in your own contracts rather than assuming they are
  rare (in the development repo they were close to half):

  ```bash
  python3 -c 'import yaml,glob; fs=[f for p in glob.glob(".shrt/contracts/*.yaml") for d in [yaml.safe_load(open(p)) or {}] for f in (d.get("failures") or [])+[g for s in (d.get("rpcs") or {}).values() for g in (s.get("failures") or [])]]; print(sum(1 for f in fs if not f.get("code")), "of", len(fs))'
  ```

  Probe those on the message instead. **Where the message lives depends on the backend**, and the
  run record says which: read the failing step's `http_status`.

  - **200, verdict in the body** — the refusal is in your envelope. With the default
    `envelope_path: error.code` and a backend that puts the Connect code there:

    ```yaml
        expect:
          - path: error.code
            equals: invalid_argument
          - path: error.message
            contains: must be at most 128 characters
          - path: error.details.0.reason
            exists: false
    ```

    The last line is what stops the probe from passing for the wrong reason later, if the backend
    ever starts naming this failure.
  - **4xx, body `{"code": "invalid_argument", "message": "…"}`** — a Connect error, raised before
    any response message existed. There is no body for `error.*` to read: every body assertion is
    reported `unevaluated` and fails. Assert the transport outcome instead:

    ```yaml
        expect:
          - path: transport.code
            equals: invalid_argument
          - path: transport.message
            contains: must be at most 128 characters
    ```

    `transport.*` is reserved (GRAMMAR.md §1): `code` is the Connect code, or `ok` when the call
    was answered 200; `http_status` is the number; `message` is absent on success. A step whose
    `transport.*` assertions all hold on a refusal is `passed` — it needs no `allow_fail` — and a
    different refusal, or a success, fails it.
- **State the refusal on the envelope path, or on `transport.code` / `transport.http_status`,
  as well as the app code** — the first expectation is not decoration. Drop it and lint demands every required field the probe deliberately omits; why
  `allow_fail` and the app code do not buy that exemption is `PITFALLS.md` §13.

Before writing the probe, check the code is reachable at all: `unreachable:` and `pending_deploy:`
in the contract say it is not, and why.

## 6. Cross a principal boundary

Auth is a **chain-level** concern. `.shrt/config.yaml` declares the login once and the runner
attaches the header, re-logging in on expiry — a chain that passes today must not fail tomorrow
from an expired token, because that is a false fail, not a regression.

- A second kind of principal → declare it as a named profile in the config, then `auth: <profile>`
  on the step. Each profile holds its own token cache.
- A login step is optional. Write one only when the chain is *testing* login, or when the flow
  reads better with it — put it first; it needs no `skip_auth`, since a call to a configured login
  rpc never carries a token (its `auth_profile` is `none`), and its token seeds the cache so later
  steps do not log in twice. It seeds only a profile whose `body` it sent verbatim; a login as
  anyone else seeds nothing, and its `note` says so. Each step's `auth_profile` in the run record
  names the profile it ran under.
- Never `skip_auth` plus a hand-written `Authorization` header. That is the workaround profiles
  replaced, and lint rejects `skip_auth` and `auth` together.
- **Probe that a missing or invalid token is refused** with the two sanctioned forms, never a
  hand-written header:

  ```yaml
      - id: fetch_without_token
        call: acme.orders.order.v1.OrderService/FetchOrder
        body: {id_order: "${create_order.id_order}"}
        skip_auth: true
        expect:
          - path: transport.code
            equals: unauthenticated
          - path: transport.http_status
            equals: 401
      - id: fetch_with_bad_token
        call: acme.orders.order.v1.OrderService/FetchOrder
        body: {id_order: "${create_order.id_order}"}
        auth: invalid
        expect:
          - path: transport.code
            equals: unauthenticated
  ```

  `skip_auth: true` sends no token. `auth: invalid` sends a token the backend never issued, in the
  header and scheme of the profile that would otherwise cover the call, and does not re-login on
  the 401 — so the real token cache is untouched. `invalid` is reserved: no profile may take the
  name, lint rejects it with `export`, and `shrt run` refuses it when the config declares no auth.
  If your backend reports an unauthenticated caller in the body with HTTP 200 instead, assert the
  envelope path, not `transport.code`.
- Never hand-write the auth header on a step a profile covers: the middleware overwrites it, so the
  step runs as that profile's principal. lint rejects it and names the profile.

## 7. Author or extend a contract, in payoff order

```bash
shrt contract quality                  # what is actually missing — start here
shrt contract init <domain>            # scaffold; re-running keeps what you wrote
shrt contract lint
```

**A domain is the package segment after the organisation root, once the trailing version is
dropped** — usually the SECOND dotted segment of the service name. `DomainOf` drops version segments
such as `v1`, also drops a reverse-DNS root (`com`, `org`, `io`, …) when the package has at least
three segments left, and then reads `<org>.<domain>.…`; a one-segment package is its own domain.
Nothing about it is specific to any one backend. Wherever a surface groups
its privileged writes under a single segment, every one of them lands in THAT overlay, not in the
overlay of the thing they are about.

Worked example, measured on the backend this kit was first written for: `acme.admin.*` is one
domain no matter what it is about, so
`acme.admin.pricing.assetmark.v1.AssetMarkActionService/FreezeAssetDailyMark` lands in `admin.yaml`
and not in `pricing.yaml`. There, `shrt catalog ls -filter pricing` matched the string anywhere in
the name and returned **11**, while `shrt contract init pricing` scaffolded the **10** whose second
segment is `pricing`. The gap was neither a bug nor a miscount.

The lesson transfers even if your surface has no `admin` segment: an author who reads only their own
overlay will not see the rpcs that produce what it reads. **When a domain's writes look too few for
its reads, grep the catalog for the other segments before concluding anything.**

**`shrt contract status` counts entries; `shrt contract quality` measures them.** The CONTRACT
column of `status` goes to its ceiling the moment a scaffold lands — N-of-N, and 200/200 the day
after 76 empty scaffolds landed — so a full column says only that somebody ran `contract init`.
VERIFIED counts only entries a human set to `status: verified`, and scaffolds are written `draft`. The one thing they do tell you is when RPCS **exceeds** CONTRACT: that is an rpc
the descriptor knows and no overlay has an entry for, which is what a freshly rebuilt descriptor
surfaces after the backend adds a procedure (125 vs 124 on 2026-09-12, `ResolveInstrumentNames`).
`status` says all this in its own footer and carries the quality score in its GAPS and SCORE
columns, so you can start there; go to `quality` for the per-rpc detail, because an entry exists for
every covered rpc and an agent still cannot compose from one whose fields carry no `from` or whose
failures carry no `when`.

**REACHED is the one column that is not a restatement of CONTRACT**, and until 2026-09-22 nothing
defined it — a blind adopter read `5 of 6` and could not tell whether the missing one mattered. It
counts the rpcs appearing in some **multi-step** `shrt contract plan`, as the target or as a
dependency. An rpc below the count plans as a single step: nothing it needs is declared, and no
other entry names it as a producer. For a login, or a read that takes no id from anywhere, that is
correct and permanent. For a write that cannot run on its own it means a missing `needs:` or `from:`,
and the chain composed from that contract will be short by a step. `shrt contract status -gaps`
lists them as `no path to`; the column cannot tell the two apart and does not try. A streaming rpc
is never REACHED, because shrt is unary-only and no plan can call it; `-gaps` lists it as
`streaming … (out of scope)` instead. `-gaps` prints only the gap lines, not the table.

**The score measures OMISSION as well as vagueness, and it did not always.** Until 2026-09-12 every
term asked whether something *declared* was vague, so declaring nothing scored best of all: a bare
`shrt contract init` scaffold scored **0** and sat exactly at the gate's floor. `PITFALLS.md` §18 is
the receipt. What the pair of them measures now, and what each term is worth:

| # | weight | phase | term |
|---|---|---|---|
| 1 | 2 | happy | a request field in the descriptor not in `required:` and with no `fields:` entry that says anything (a `from`, `value`, `same_as`, `oneof`, `checked_by`, or a note of three words or more) |
| 2 | 2 | happy | an id field named in `fields:` or `required:` with no `from` / `same_as` / `value` — unless it is optional AND its `note:` says why |
| 3 | 2 | failure | a write rpc whose own `failures:` is empty; a domain-wide block does not satisfy it |
| 4 | 2 | happy | a missing `summary`, a `TODO`, or one shorter than three words |
| 5 | 2 | happy | an rpc with no `requires_role:` at all — `NONE`, alone, is how you say "no role gate" |
| 6 | 2 | happy | a READ rpc no write rpc can reach — unless `no_producer:` says why |
| 7 | 2 | happy | an rpc with request fields and an empty `required:` — `NONE`, alone, says the server rejects nothing |
| 8 | 1 | failure | an id wired by `from`/`same_as`/`value` with no `checked_by:` |
| 9 | 1 | happy | a response field named in no `exports:`, `terminal:` or `soft_signals:` |
| 10 | 1 | failure | a failure with no `when:`, `unreachable:` or `pending_deploy:` |
| — | — | — | codes the backend raises that no contract declares at all |

The ten scored rows are the terms `shrt contract status` prints in its footer, the same list
that computes the score — so run the command for today's list, and read the rows below for the
reasoning a one-line label cannot carry. In shrt's own development repo a test fails the build
when this table and the scored terms disagree; that test does not ship with the binary, so here
the command's footer is the authority.

**An unfilled `TODO` scores nothing.** It is listed beside the score as a hint, never charged. The
row that used to claim `1 per unfilled TODO` was wrong for as long as it stood: no scored row
counts unfilled TODOs, so clearing every TODO in a file moves the score only where clearing it also
answers one of the ten rows above.

**The phase column is what `-phase` filters.** `shrt contract quality -phase happy` scores the seven
rows a working chain needs and ignores the three that curate refusals; `-phase failure` does the
reverse. Curate happy first: a domain at happy-0 composes and runs, and nothing about the three
failure rows changes that. The default is still every row, so a gate pinned to a baseline is
unaffected.

Read-only rpcs (a name starting with a prefix in `conventions.read_only_prefixes`; default list in
`GRAMMAR.md`) are exempt from rows 2 and 3: an unwired read filter
is a filter left off, and every rpc in this corpus with no `failures:` of its own is a read that
refuses only in the domain-wide ways. Row 6 is the mirror of that exemption and applies only to
reads.

**Row 6 asks whether a producer EXISTS, not whether `needs:` names it.** A read rpc satisfies it
when any of four things is true: a `needs:` entry names a write rpc; a `fields.<f>.from` or
`same_as` points at a write rpc's response; an alias override does; or some write rpc, in any
domain, declares `before:` this read. All four are edges `shrt contract plan` walks, so the term
fires when `plan <read>` would compose a chain **one step long — the read alone**, and also when
the read's only edges point at other reads. A `no_producer:` of three words or more spares it. Requiring
`needs:` specifically was measured and rejected: it fires on 18 of the corpus's 49 read rpcs and all
18 already compose correctly through a `from:` edge, so every one would have been a false positive.

**Row 5 charges silence, not a wrong role list.** Nothing in the descriptor says which roles a
procedure needs — that lives in the backend's policy rows — so the score can see the key's absence
and nothing more. `requires_role:` is what makes `plan` print `caller must hold role …`; without it
a chain author meets a permission refusal at run time instead. Fill it from wherever your backend
keeps its role grants (policy rows, migrations, middleware config), and for an rpc that reaches no role
gate at all, such as a login or a surface for callers outside the staff roles, say so with
`requires_role: [NONE]`. `NONE` beside a real role is a lint error. In the development repo, a third
of the rpcs that declared nothing were real omissions the migrations answered.

**The three exemptions are deliberately different keys, and must stay different.** Row 6 is spared by
`no_producer:`, row 5 by the literal `NONE` inside `requires_role:`, and row 7 by the literal `NONE`
inside `required:`. One shared escape hatch would mean a note written about a missing producer
silently also settles the role question — which is the same failure as a scaffold note that answers a
question nobody asked. `note:` spares none of the three: it is prose about a field, and the score
cannot read it.

**`summary:` does NOT spare row 6, deliberately.** Every rpc has a summary — row 4 charges its
absence — so accepting one as the explanation would make row 6 fire on nothing.

**One undocumented exemption, now documented: a response field that is a non-repeated message is
not counted by row 9.** Row 9 skips the envelope field (`error` by default),
and skips message-typed fields unless they are `repeated`, because a bare nested message has no
scalar to reference and `exports:` names paths a later step can read. So a score of 0 guarantees
every SCALAR and every REPEATED response field is accounted for in `exports:`/`terminal:`/
`soft_signals:` — it does not guarantee that a singular nested message was looked at. Reach into it
with a dotted path (`row.id_reference_rate`) when a later step needs the value.

Every row but the last needs only the descriptor and the overlays, so they live in the binary. **The
last one cannot**: finding the codes a backend raises means reading that backend's source, and shrt
drives a backend it never imports. shrt ships no tool for it: in the development repo it is
`scripts/contract-quality.py`, which **exits 2 rather than 0 when it cannot find that backend**,
because a check that cannot run must fail, and you write the equivalent for your own backend.

The score itself can be gated as a **ratchet** with `shrt contract quality -gate -baseline <file>`:
it fails if the score rises, and also if it falls without the baseline being lowered, so improving
a contract means lowering the number in the file. `shrt chain hollow -gate -baseline <file>` does
the same for hollow reads. That is what stops an N-of-N score from meaning less each time the
backend grows.

What no term can see is an INCOMPLETE `needs:`. Row 6 above catches a read that nothing at all
produces, which is the loudest version of the mistake; it cannot catch a read that names one
prerequisite and misses a second, because the descriptor gives it nothing to compare against.
Measured 2026-09-12 on three blind re-authorings of `pricing`: row 6 separated two of the three from
ground truth and left the third — whose every read had a producer, just not the right ones — at 0.
So step 3 below is still the step the gates nag you about least, and it is the one all three blind
authors skipped.

`before:` has no term at all, and cannot: the edge lives in the domain that OWNS the prerequisite,
so the consumer's overlay is not where a gate would look. All three blind authors wrote zero
`before:` entries.

Fill in this order — each step pays for the next:

1. **`required`** — from the backend source, not the proto. This is what makes lint catch an
   unfilled scaffold, which nothing else can see. It applies to **reads too**, not just writes: a
   `Fetch` that rejects an empty id has a required field, and leaving the key empty is how a chain
   comes to send `""` and lint clean. If the server genuinely rejects nothing, write the literal
   `required: [NONE]` — `shrt contract quality` charges an empty `required:` on any rpc that takes
   request fields at all, and `NONE` is the only thing that spares it, so silence and "nothing is
   required" cannot look the same. The same shape as `requires_role: [NONE]`, for the same reason.
2. **`fields.<f>.from` / `same_as`** — the ordering edges. Get these right and chains compose
   themselves; get them wrong and the `# order:` line is visibly wrong, which is the cheap failure.
3. **`needs` / `before`** — the prerequisites nothing references. `before` exists so a prerequisite
   can declare the edge from its own domain, instead of the consumer importing knowledge of it.
   No gate finds either, so derive them by hand. For `needs:`, open the handler and list every row
   it **reads** whose identity comes from neither a request field nor the caller's identity; each
   such read is a `needs:` on whichever rpc **writes** that row. For `before:`, ask the mirror-image
   question, which is the one that gets skipped: *which rpc in some OTHER domain consumes state that
   an rpc of mine writes?* That edge cannot be seen from the consumer's domain, so if you do not
   declare it here nobody will.

   The cheap first pass, before opening any handler: run `shrt contract plan <rpc>` for EVERY read
   rpc in the domain and read the `# order:` line. An order one step long means the read has no
   producer at all (`shrt contract quality` charges 2 for that). The one no gate can see is an order
   that reaches the ENTITY but not the EVENT — every id the read filters by gets created, and
   nothing in the chain ever writes the row being read. Ask of each order: *could this sequence have
   produced a row for me to find?* A `from:` edge wires the id you FILTER by; `needs:` names the
   write that PUT THE ROW THERE. Wiring the ids and stopping is the characteristic failure, because
   at that point every gate is green.

   Expect the producer to live in another domain. A read often depends on a write whose proto you
   never open while curating this domain, so reading only this domain's protos cannot show you the
   edge. And **a domain with no `before:` at all is a claim that nothing outside it depends on
   anything it writes** — true for some domains, and worth stating to yourself deliberately rather
   than concluding by omission.

   Receipts for both failures, with the measured numbers: `PITFALLS.md` §18 and §19.
4. **`failures`** — one entry per way it refuses, with `when:` written as a *condition*, not a
   restatement of the name. `code` is optional: a shape error raised before the business logic has
   only a `connect_code`.
5. **`exports`, `terminal`, `soft_signals`** — what the response is for. `terminal` records a dead
   end rather than deleting the knowledge that it is one.
6. **`source`** — the files you read. This is what lets the next person re-check you, and it is
   also what a cross-check script uses to find codes you missed.

**`before:` always attaches the PLAIN rpc.** If the later rpc also `needs:` aliases of it
(`needs: [AddStock@first, AddStock@second]`), the plan gets an extra unaliased step carrying the
scaffold's empty strings and zeros, and `plan` flags it with a note naming every edge. Drop the
`before:`: the `needs:` already orders the aliases.

Leave `status: draft`. Only a human promotes to `verified`.

**`reason:` means two different things and only one of them is checkable.** For a failure with a
numeric `code`, it must be the backend's own string character for character — that is how a receipt
is matched back to its `errmsg.New` site, and a mismatch is a bug. For a shape or auth failure there
is no backend string at all (`Detail: nil`), so the `reason:` you write is a **label the contract
invents** to name the condition. Both are legitimate; confusing them is what makes an agent write a
probe asserting a path that can never exist. The numeric `code` is what tells them apart.

## 8. Run, hand off, verify

```bash
shrt chain lint <name>          # fix every error before sending traffic
shrt chain lint -strict <name>  # and every assertion-quality warning, before it is a gate;
                                # a warning about the run's environment stays a warning
shrt run <name> -dry-run        # resolves references and validates bodies against the proto,
                                # sends nothing; it does not run chain lint, so lint first
shrt run <name>
```

Each progress line starts with the step's status: `ok` passed, `FAIL` failed (the call was
answered and an expectation or export did not hold), `ERROR` error (the step never completed: an
unresolved reference, a body the proto rejects, a transport failure), `SKIP` not sent under
`-keep-going` because it reads a step that did not pass, and `--` a `-dry-run` step that resolved and
validated. A chain reading a `${vars.x}` it does not declare and was not given is refused before
anything is sent, with the `-var` flags it needs; so is one reading a response field the earlier
step's response message does not have, such as a misspelt `${create_product.product.id_prodct}`. A step with no `expect` that the backend refuses
in-band stays `passed` with a warning under it; only `chain lint -strict` stops it. A step that
declares expectations and is refused in-band FAILS unless one of them pins the verdict (`equals`
on the envelope, or any rule on the envelope or a `transport.*` path that would FAIL on a
successful answer, such as `not_equal: SUCCESS`): `expect qty_on_hand equals: 0` holds on the zero
a refusal leaves, and must not turn the step green. A pin the refusal and the ok value both satisfy
declares nothing: `status.code not_equal: ""`, `not_equal: REJECTD` (a typo), or `transport.code
equals: ok` on a call refused in-band. References in a pin are resolved first, and `chain lint`
warns on `not_equal: ""` on the envelope, which `-strict` fails. The same holds for a
response that carries NO verdict where its message declares one (no envelope, `status: {}`, or an
empty code): it fails unless an expectation pins the envelope (`status.code exists: false` when
an absent verdict is what the rpc answers) or the transport, and warns on a step with no
`expect`. Every step warning is
repeated in the closing summary as `warning [<step>]: ...`, so `-quiet`, which drops the progress
lines, still shows them.

Read the run status as three values, not two: `passed`, `failed`, and **`error`** — and `error` is
nearly always evidence about your fixture rather than the backend: most of the time no request was
sent. Read the step's `error` line before deciding which. That trap is `PITFALLS.md` §4; the vocabulary
it belongs to is `GRAMMAR.md` §8.

Then propose the run and put the decision to the user. **Run the chain twice before proposing**:
the warning about fields that will drift compares the proposed run with an earlier passing run of
the same chain, so with only one run it is not made.

```
shrt confirm <name> -run <run-id> -note "what you inspected in the responses, and why it is right"
```

This writes `.shrt/safespots/pending/<name>.json` and a full report beside it, `<name>.md`, and
prints a summary table: one row per step with an excerpt of what was sent, what it asserted and
what the backend answered (for a batch, with the per-item verdicts `conventions.item_envelope_path`
reads). It writes no safe spot, and `shrt verify` still has nothing to compare against. Proposing
again for the same chain replaces the pending proposal and its report; there is only ever one.
`shrt init` gitignores `.shrt/safespots/pending/`: a proposal is review material on the machine
that made it, and only the approved safe spot beside it is committed. A run that did not pass is
refused, and the refusal names the run and each step that failed or errored.

**Present the proposal in the conversation; do not send the user to a file.** In the user's
language, give:

1. one line: chain, run id, `n/n` steps passed, and what the chain exercises;
2. the table, with each step as a plain situation ("create again with the same sku"), what was sent,
   what came back, and whether that is right;
3. the one or two facts that carry the verdict (a stock level read back after a partial batch, say),
   and anything you corrected or are unsure of;
4. what approving means: every response field becomes the baseline `shrt verify` compares against,
   except what a volatile pattern covers. The summary lists those patterns, and warns by name
   about a step whose every response field is volatile (a `**`, or a pattern covering the whole
   response): approving sets no baseline for it, so narrow the pattern unless that is intended.
   The summary compares the run with the previous passing run of the chain against the same
   target (a run against another backend says nothing about this one) and lists each field
   that differed and is not masked: every `verify` would report those as drift. Pass the warning
   on, and fix it before asking (add the paths to `volatile:`, re-run, propose again) unless the
   difference is real. With no earlier passing run the summary says the check was not made; run
   the chain once more first. A `-supersede` proposal is also compared with the safe spot it
   replaces, which is what the user signs off on: the table gains a `vs replaced safe spot`
   column, and every request and response difference from it is listed under the table;
5. the question: approve or reject.

Run `shrt confirm <name> -approve -by <user email>` only after the user answers yes to THIS
proposal. The email is the user's own, as the session knows it; if you do not know it, ask. A
bare name is refused. `-reject` discards the proposal. Approval refuses a run record rewritten
after the proposal, since the user approved what the summary showed: the proposal's digest covers
everything that becomes the safe spot (target, build, vars, volatile, and every step's status,
request, response, http status and transport error), not only the responses. The safe spot
keeps that digest, and `verify` refuses a safe spot whose content no longer matches it: a hand
edit is not what a person approved. Restore the file, or re-approve with `-supersede`. `-pending` lists what awaits
a decision, and `chain ls` marks it `?`. A chain that already has a safe spot needs `-supersede`
on the proposal, and the old one is archived on approval as
`.shrt/safespots/archive/<chain>/<run id>.json`, named by the run it held (the id the new safe
spot's `supersedes` names; a run archived twice gets a `-2` suffix).

## 9. Refactor and test against a safe spot

This is what the whole loop is for, and it is the section most likely to be skipped, because a
chain that has never been confirmed still runs and still goes green.

**A passing run and an unchanged run are different claims.** `shrt run` asks whether the
assertions still hold. `shrt verify` asks whether the RESPONSE still matches what a person approved
as correct — every field, not just the ones somebody thought to assert. With 43% of steps in this
corpus asserting only the error envelope (`PITFALLS.md` §17), the second question is the one that
catches a refactor that changed a number nobody was watching.

Before you touch the code:

```bash
shrt run <name>                                  # a fresh receipt on today's binary
shrt verify <name> -run <run-id>                 # does it still match the safe spot?
```

After you touch it, the same two lines. When the replay ran against another target than the
safe spot's, the report opens with `targets differ: safe spot <a>, this run <b>`: a difference may
then come from the target, not the code. A drift report names the step, the path, the change kind
(listed in `GRAMMAR.md` §7), `want` and `got`, so a regression arrives as *which rpc changed* instead
of a failing test somewhere downstream. A clean report covers only the steps of that chain's safe
spot, and says so: a regression in a path no safe spot exercises is not seen.

A drift can also come from the chain itself. `verify` first compares what each step SENT with what
the safe spot's run sent, and prints each difference before the response changes:
`request differs from the confirmed run at create_order lines.0.qty (3 -> 4)`. Values built from
another step's output or from `${uuid}` / `${now}` are skipped, since they differ every run. With a
request difference the verdict is `drift with different input`, not `regression`: the backend was
asked something else. Restore the input, or, when the edit is intended, bring the expectations in
line, run it green, and propose that run with `shrt confirm <name> -supersede`. When the
difference comes from vars rather than the chain file (a `-var` on this verify, or a `-run` recorded
with other vars), verify names them, `qty=2, confirmed with 3`, instead of blaming the chain's input.

Three things that decide whether this works for a given chain:

- **`verify` compares every step the run reached, not only the steps before the first red.** A
  live `shrt verify <name>` runs the chain the way `shrt run -keep-going` does, so a red step does
  not hide the drift behind it: every step that was sent is diffed against the safe spot, and a
  step that was not (held back as `skipped` because it reads the failed step, or never sent
  because the run stopped) is reported as `not_reached` rather than as a status change or a
  shorter chain. The report names the first failing step. A recorded run that stopped at its
  first red (`-run <id>` of a run made without `-keep-going`) reports every step after the stop
  as `not_reached`.
- **`shrt verify -run <id>` re-diffs a RECORDED run and sends nothing.** It needs no backend and no
  credential, so the after-check costs one run, not two. A record whose `chain` is another chain,
  copied into `runs/<chain>/`, is refused, here and wherever a run is loaded by id. It is also how you investigate a drift
  without spending another live run.
- **A chain that creates things is re-run with a fresh `-var tag`, so every tag-derived value
  legitimately differs.** `verify` masks what `diff` masks: config and chain `volatile` paths,
  and a changed value that is id- or timestamp-shaped (`id`, `*_id`, `id_*`, `idX`, `*_at`, a
  UUID, an RFC 3339 time), counting how many it did not report. Both values must be id-shaped
  alike (two non-zero numbers, or two non-empty strings of the same shape, with the same letters
  before the first separator): an id that became `""`, null, `0`, `undefined` or a different JSON
  kind, or disappeared, is reported, and so is an id of another kind (`cus-...` became `prd-...`). Anything else that differs every
  run, a sku built from `${uuid}` or a message quoting it, must be in `volatile`, or the first
  replay reports a regression that is not one. The price is that a wrong id that is still
  id-shaped is not caught by `verify`; assert on it if it matters. The mask is part of what was
  approved: the safe spot stores its `volatile` patterns, and a pattern added to the config or
  the chain later (`**.total_minor`, `**`) fails `verify`, which names the pattern and every value
  it hid, until a run under the wider mask is proposed with `-supersede` and approved. The report
  counts the values it kept out, both kinds; `verify -masked` lists every one of them, the
  volatile ones and the id- or timestamp-shaped ones, with its path and both values. This is per-chain work and it is why paving the corpus is not a bulk
  operation — see the development repo's one worked example, `.shrt/safespots/seed-position-exposure.json`, whose
  `volatile` list is 14 patterns long.
- **A chain in the expect-fail set must NEVER be confirmed.** Those chains assert a pre-fix defect,
  so a safe spot would freeze the bug as ground truth. `PITFALLS.md` §11.

**Before a chain has a safe spot, `shrt diff` is the run-to-run check.** Only the user's yes
creates a safe spot, so a refactor often has to be checked with none:

```bash
shrt run <name>                                  # before the change
shrt run <name>                                  # after it
shrt diff <name>                                 # latest~1 against latest
shrt diff <name> <run-a> <run-b>                 # or any two runs; ids, latest, latest~N
```

It reports step status changes, where the first failing step moved, steps reached in one run and
not the other (a step `-keep-going` held back as `skipped` counts as not reached), and response
differences in steps both reached. It masks the `volatile` patterns stored in each record plus the
ones in today's config and chain file, so a pattern you add after the runs still applies. It also
masks ids and timestamps, which differ every run: a field named `id`, `*_id`, `id_*` or the
camelCase forms, a `*_at` or `*_time` field, and any pair of uuid or RFC3339 values, as long as
both values look alike: an id that became empty, null, `0`, `undefined` or another JSON kind is
shown. Values derived
from a run tag (a sku, an email) are not ids; declare them `volatile`. An id inside a longer string
(an error message naming the product) is not masked either; declare that path volatile too, knowing
it also hides a genuine change of that message. The report says how many values it hid, and names
the two runs' `build` labels and any var that differed, since a difference that follows a changed
`-var` comes from the input, not the backend. It exits 1 when the runs differ and 2 when it could not compare them (an unknown run, runs of two chains). It is a
comparison between two runs, not a verdict: it cannot tell you which of the two is right, only
that they disagree and where.

In the development repo, `scripts/verify-all-flows.sh` prints `safe spots: N of M chains` in its header and diffs every
chain that has one, reporting `DRIFT` separately from `UNEXPECTED` — the assertions held, the state
did not. The rest of the corpus is checked for PASS/FAIL only. That header line is the honest
measure of how much of the corpus is actually a regression baseline rather than a smoke test.

## 10. Find the chain first: which one exercises this rpc, or asserts this code

`chain slice` cuts one chain down to one step. It cannot tell you WHICH chain and WHICH step — and
an agent holding a failing rpc name, or an `app_code` out of a production envelope, has only that.
`chain which` is the index over the corpus that closes the gap, and it ends each chain's block
with the `chain slice` command to paste for that chain's best match.

```bash
shrt chain which -code 1218
shrt chain which -rpc ObligationActionService/ApproveOffsetObligation
shrt chain which -rpc ProposeOffsetObligation -code 1204
shrt chain which -code 1218 -json
```

1. **Two selectors; naming neither is an error, not a listing of everything.** `-rpc` takes the same
   shorthand `contract show` takes and resolves through the same catalog, so `Service/Rpc` and the
   fully qualified form find the same steps. `-code` matches an `equals` wherever a chain can name a
   failure: the envelope code, an `app_code` detail, a `reason` detail, or `transport.code` (a
   Connect refusal such as `invalid_argument` or `unauthenticated`). The searchable paths are
   derived from the corpus, so a chain asserting a code under a batch result — the corpus has
   `results.0.error.details.0.app_code` — is found without teaching the command a new shape. Both
   selectors together intersect.
2. **`asserted` and `OBSERVED` are different claims.** `asserted` means the chain says that step
   answers that code. `OBSERVED` means a run record under `.shrt/runs/` reached that step, and the
   line cites the NEWEST such run, whatever it got — `README.md`'s "where the authority is" rule,
   applied to discovery. Matches rank in three tiers: observed and holding, then asserted only, then
   observed but contradicted (under `-code`, the newest reaching run got a different code at the
   asserted path; under `-rpc` alone, the step FAILED). Within the first tier a step that passed
   outranks one that failed while still answering the code, and within the last a step that
   passed outranks one that failed, so a chain whose best match failed never heads the list
   while passing evidence exists elsewhere. The order is this because a
   chain the backend has stopped answering that way is the least likely to reproduce it, though it
   is often the regression you want to see. Run records are gitignored and machine-local, so a
   clone with none reports `no local runs` and still ranks by the assertions. A step the backend
   refused at the transport layer was reached, and its `got` is read from `transport.code`. An
   `OBSERVED` line reads `asserts <code>` for the claim; the next line, indented, always reads
   `run <id> got <code>, step <status>` for the record. `got` is read from the path of the shown assertion, never copied from it; when that path is
   absent the line says `got X at <path> (nothing at <asserted path>)`. A step that failed says
   `step FAILED`, followed by one `failed: <path> want=… got=…` line per failing expectation. When
   the newest run did not reach the step, a `newest run <id> did not reach it: step <status>` line
   follows, or `newest run <id> did not reach it: run stopped at step <id> (<status>)` when that
   run ended before the step was recorded at all. `-json` carries the same facts as `asserts`,
   `observed` (with `holds`, `asserted_path`, `failures` and `newer_runs_not_reaching`) and
   `newest_unreached` (with `stopped_at` and `stopped_status` for a run that ended earlier).
3. **The last line of each block is the deliverable.** It is a `chain slice` invocation, and for an
   `OBSERVED` match it is the `-mode pin -run <id>` form, pinning the newest reaching run (even a
   contradicting one), because pinning that run's values is the cheaper reproduction of the same
   incident. When a write the slice keeps interpolates a var into what it creates, the line ends
   with `-var <name>=<fresh>`; replace `<fresh>` before pasting. On the pinned form a string that a
   dropped step before the target also sent, template for template (`w-${vars.tag}` in both), is
   not counted: the kept write names what that step created under the run's value, so the slice
   takes the value from the run. The same rule decides every `next:` line of `slice -verify` and
   the up-front refusal of 11.5. Paste it; do not retype it from the columns.
4. **`slice k/n` is the CLOSURE size, not the size of the pinned command printed under it.** It is
   the mode-independent cost of reaching that step, so the numbers are comparable across chains;
   `-mode pin` can only drop steps, never add them.
5. **No match exits non-zero and says so.** An empty list and a green exit is the failure this repo
   cares most about, so "the corpus does not exercise it yet" is an error, not a quiet success.
   One exception under `-code`: when no chain asserts the code but a local run record carried it
   at a code path (`status.details.0.app_code: 1305` on a step that asserts only the envelope and
   the `reason`), the command lists those steps instead, each with the run, the path, and a
   `reproduce:` slice command, and exits 0. The backend exercises the code; no chain pins it, so a
   change to that answer goes unnoticed until you assert it there.

## 11. A step failed: get the minimal reproduction

A 69-step chain that goes red at step 61 is a bad bug report. `chain slice` computes the ordered
sub-chain that reaches one step and writes it out as a chain of its own — never a "skip some steps"
run mode, because `README.md` rule 2 holds for the result too.

```bash
shrt chain slice dealing-approve-obligation-guards -step approve_offset_d_not_open
shrt chain slice dealing-approve-obligation-guards -step approve_offset_d_not_open -write probe
shrt chain lint probe
shrt chain slice dealing-approve-obligation-guards -step approve_offset_d_not_open \
    -write probe -verify -run latest -var tag=PROBE1
```

1. **Slice, and read the reason on every kept step.** Each line says `target`,
   `produces ${x} used by <step>`, or `contract needs <rpc>[@alias] (<edge>)` — the third kind
   comes from `needs` / `before` / `from` / `same_as` in `.shrt/contracts/`, which is the only
   reason a step with no textual link to the target survives at all. For an aliased edge
   (`<rpc>@<alias>`) the slice keeps the step whose id carries `_<alias>` (the id `contract plan`
   gives it), preferring among those the one the body references; for an unaliased edge it keeps
   the referenced step, else the nearest. An edge declared under one alias of a contract binds only
   a step carrying that alias. Steps are numbered from 1, as in `shrt run` and the run record, and
   so are expectations in a verify difference.
2. **Read the two named sections before trusting the count.** `unmet prerequisites` means a
   contract edge names an rpc *no earlier step calls*, so the slice may not stand alone.
   `WARNING possible under-inclusion` means the dropped steps BEFORE the target include WRITES
   (a write after the target cannot matter and is not counted), and the line adds
   `under half of them` when the slice keeps fewer than half the steps up to the target: nothing in the YAML records that step 61
   needed the row step 20 wrote, so the slice can be too small and still go GREEN. That failure mode is worse than no slice.
   With `-run`, a dropped write whose step in that run was refused (a transport error, an envelope
   code other than `envelope_ok`) or never sent wrote nothing: it is listed under
   `wrote nothing in run <id>` and does not count. A write that succeeded, including an idempotent
   replay, still counts. A step calling a login rpc the config names (`auth.call` or a profile's
   `call`, typically a `skip_auth` step) is never counted: shrt logs in itself per auth profile, and
   a login builds no state the target depends on.
3. **`-write [name]` then `chain lint` it.** A slice that does not lint is a failed slice, not a
   smaller chain. Without `-write` nothing is written and the YAML goes to stdout, and `-verify`
   still runs the slice but keeps no run record, since the record would name a chain that does not
   exist. `-keep id[,id]` forces named earlier steps back into the slice, with their own producers
   and prerequisites. `-write` refuses to replace an existing chain file unless `-force`, except a
   slice this command wrote of the same chain and step (its description starts
   `Slice of <chain> reproducing step <step>:`), so the `next:` loop can re-slice in place.
   A slice whose description carries a VERIFIED verdict is protected too: re-writing it without
   `-verify` keeps the verdict when the new slice is identical (same steps, vars and pinned
   values), and otherwise is refused unless `-force`, since the new slice was never verified.
   When the slice keeps EVERY step of the chain (slicing a chain that is itself a slice, or a
   hand-written minimal chain, or a `next:` command that keeps every dropped write of a chain
   whose target is its last step), the slice is that chain: `-write` with no name writes no
   `<chain>-slice-<step>.yaml` copy, the `-verify` run record is kept under the chain itself, and
   a `reproduced` verdict is recorded in that chain's own `description:`. Its source run is then
   a run of that same chain, so the line says so ("the verdict of <chain>'s own run"): it
   re-ran the chain, it did not reproduce another chain's failure. In a hand-written chain it
   is a VERIFIED line (replacing a previous one, else appended). In a chain that is itself a
   slice it is a separate RE-RUN line, and the slice's VERIFIED line against its real source run,
   with the dropped steps it lists, is kept. `-write <name>` still writes a copy under that name.
   A written slice whose `-verify` says NOT REPRODUCED records that, with the first difference,
   in place of the HYPOTHESIS paragraph. `-build <id>` stamps the `-verify` run as `shrt run
   -build` does, and every verdict line names the build it held on.
4. **`-verify -run <id|latest>` is what turns the claim into a receipt.** It runs the slice and
   compares the TARGET step's verdict — the code at `conventions.envelope_path`, the refusal
   beside it (its `message`, and `reason` and `app_code` at `details.0.` or beside the code, the
   ones `shrt run` prints), a transport refusal (HTTP status and code), the pass/fail of every
   expectation, and for an expectation that failed in both runs against the same want, the value
   it got — against that same step in the source run. Values that differ every run are masked
   as `verify` masks them: an id- or timestamp-shaped got on both sides, or a message that
   differs only in such tokens, is the same failure. `-verify` is what needs `-run`;
   closure mode alone does not. With `-run latest`, `-verify` and `-mode pin` use the newest run
   that REACHED the target (its step passed or failed) and say on stderr when that is not the
   newest run; an explicit `-run <id>` that stopped before the target is refused, naming a run
   that reached it. It prints one of four
   outcomes, each with its own exit code:
   - `reproduced` (0): the verdicts match and the slice dropped no write step that wrote
     something in the source run. With `-write`, the verdict replaces the HYPOTHESIS paragraph in
     the written slice's `description:` (VERIFIED, both run ids, the date).
   - `NOT REPRODUCED` (1): the target ran and its verdict differs from the source run's. That
     includes the same expectation failing with a different value: source `got 2`, slice `got 0`
     is two different failures, and the difference line says both values. So is the same
     envelope code with another reason (`InvalidQty`/1203 against `PermissionDenied`/1603), and
     a target the backend refused at the transport (a 403) when the source got an answer: it was
     sent, so it ran. When the
     slice dropped writes it also ends with the `next:` `-keep` command below, since the
     difference can come from state those writes built.
   - `DID NOT RUN` (2): the target was never sent or never got an answer — an unset `${env.X}`, a login that failed, a
     step before it that errored or failed its expectations. `-verify` stops at the first kept
     step that does not pass, and has no `-keep-going`, so a defect sitting behind another red step
     cannot be verified from that chain. Nothing was compared; fix the cause the line names and
     re-run. A refusal before anything is sent exits 2 as well, without the verdict block: an
     unknown chain or step, no `-run`, a run that does not reach the step, a missing or not-fresh
     `-var name=<fresh>`. Only a flag that cannot be parsed exits 1.
   - `INCONCLUSIVE` (3): the verdicts match, but the slice dropped write steps. A match can come
     from state the slice never built (a limit the dropped writes would have reached, say), so it
     is not a receipt. The output ends with a `next:` line —
     `shrt chain slice <src> -step <t> -run <source-run> -keep <dropped writes> -verify -write` —
     which keeps every dropped write and so can give a real verdict. A var interpolated into a
     name is printed as `<fresh>`: the run above already used its value, so give a new one. A
     dropped write whose step failed or errored in the source run (a `-keep-going` run) is left
     out of `next:` and named with its status: keeping it would stop the slice there, before the
     target, so the command could only return DID NOT RUN. The same holds for an id you passed
     with `-keep`: one that failed or errored in the source run is left out of `next:`, and the
     output says it was yours. When every dropped write failed there
     is no `next:` line, and the output says why. Only
     the command that keeps every counted write can return `reproduced`; dropping ids from `-keep`
     again can only return INCONCLUSIVE or NOT REPRODUCED, which tells you whether the target
     needs that write but is not a receipt. So a slice with dropped writes cannot be both minimal
     and a receipt. When you want both, write the minimal chain by hand (only the steps the
     defect needs, its own writes included), run it, and `slice -verify` the target on THAT
     chain: nothing is dropped, so the verdict can be `reproduced`.
   Until you have a verdict, the slice is a hypothesis, and every slice prints a line saying so;
   when `-verify` reaches one (anything but DID NOT RUN), the verdict replaces that line.
5. **`-mode pin -run <id|latest>` when you want the fast reproduction, not the buildable one.**
   A producer whose only contribution was a VALUE is dropped and its value pinned into `vars:`,
   with every reference rewritten to `${vars.<name>}`. A `from` or `same_as` contract edge exists to
   deliver a value, so it is satisfied by the pinned value. A `needs` or `before` edge names a
   step that did something the target depends on, and pin mode leaves that to the source run: the
   state such a WRITE created already exists in the backend, for the very ids the slice pins, so
   re-sending it would change the state being reproduced (a second `AddStock` on the pinned
   product, and the `ConfirmOrder` behind it sees 13 in stock instead of 5, or the write's own
   expectation fails and the slice stops as DID NOT RUN). So when the source run performed the
   write (its step was sent, and not refused), pin mode does not keep it: the output lists it
   under `contract prerequisites run <id> already performed`, the description names it, and it
   still counts as a dropped write, so a matching verdict is INCONCLUSIVE and `next:` keeps it
   together with the producers it needs, building fresh state. A prerequisite the run did not
   perform, or refused, is kept and sent as in closure mode, and so is a read, which changes
   nothing. `-keep <id>` keeps it anyway. A var the kept steps read that the
   chain does not declare (one you passed with `-var` at run time) is taken from `-var`, else in pin
   mode from the source run's `vars`, and written into the slice's `vars:`. A var the chain
   declares is written with the value the source run used, not the default, in pin mode when no
   kept write re-sends it (the slice output lists it with its default).
   **A var a kept WRITE interpolates into what it creates must be fresh**, whatever would supply
   it otherwise: the chain's declared default, the source run's value, or pin mode's copy of it.
   Each of those has already created its names on this backend (the source run, or the first
   `-verify` that used the default), so re-sending it collides (a duplicate key refusal) and the
   target is never sent. `-verify` refuses up front and prints `-var name=<fresh>` unless you pass
   `-var name=...`, every `next:` line carries `-var name=<fresh>` for the slice it suggests
   (computed on that slice, so a pinned slice whose `next:` keeps the dropped creates asks for
   it too), and the slice output names the var. Pin mode keeps the run's value when no kept
   write interpolates the var, and when a kept write sends exactly a string a dropped step before
   the target sent (it names what that step created, not something new). A string that also
   carries `${uuid}` or `${now}` is fresh on every run and does not count.
   Pin mode needs a run record and refuses without `-run` rather than quietly falling back to
   closure.
6. **A pinned slice reproduces one incident, not the flow.** Its ids are the ids of that run, so it
   is dead the moment that data is. Confirm a safe spot from a closure slice, never a pinned one.
