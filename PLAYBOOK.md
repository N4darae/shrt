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
  `'^# order:'` — the `# ` is part of the line, and so is the single space after `note:`.
- **`plan` emits ten kinds of `# note:` and only one of them is test data you owe** — the
  `is required and has no usable value — fill it` kind. Read the others rather than skimming past:
  `has no contract, its body is a bare scaffold`, `wants X but that rpc is not in the plan`,
  `caller must hold role`, and above all `required is an unfilled TODO, so this plan cannot say what
  the server rejects without — treat the body as unverified`. That last one means the plan is
  guessing, which is the opposite of test data you owe. Ten is a count of call sites on
  2026-09-17 and it grows whenever `plan` learns to say something new — re-derive with
  `grep -c 'p.note(' core_distillation/contract/plan.go`, which matches the calls and not the definition
  (`func (p *Plan) note(`).
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
as "the header is dealt with".** Both sides share `HasUsableValue` in `core_distillation/contract`, but they
call it with different `Strictness`: `plan` reads a body **it just generated**, where a `"0"` can
only be the scaffold's own filler, so it says fill it; `lint` reads a body **a human edited**,
where `"0"` may be meant — `short_limit_qty: "0"` in
`dealing-createdeal-positionlimit-enforcement-probe` is a real limit of zero, and an int64 zero and
the scaffold's filler are the same bytes. Everything that cannot be deliberate — `""`, the zero
enum member, an empty or all-blank list — both sides reject. So the plan header is the only thing
that will ever tell you about a leftover numeric zero; nothing downstream repeats it.

Three habits that keep a chain re-runnable:

| need | write |
|---|---|
| an idempotency key | `${uuid}` — never a literal, or the second run collides with the first |
| a value two steps must share | one chain var, referenced twice — never two literals that a later edit can desynchronise |
| a name or code that must be fresh per run | `${vars.tag}` interpolated, and pass `-var tag=...` at run time |

The `-var` habit is what lets one chain run twice on the same box without tripping a uniqueness
constraint, and it is why `scripts/verify-all-flows.sh` generates a random tag per chain.

## 3b. Tell shrt how YOUR backend answers

`.shrt/config.yaml` carries a `conventions:` block. Five keys, all optional, and the defaults
describe a Connect-style backend that reports its verdict at `error.code` with `OK` meaning success:

```yaml
conventions:
  read_only_prefixes: [Fetch, Get, List, Query, Read]  # which rpc names are reads
  envelope_path: status.code                           # where a response states its verdict
  envelope_ok: SUCCESS                                 # the value there meaning success
  item_envelope_path: results[].error.code             # a BATCH rpc's per-item verdict
  validate_output: true                                # a response the descriptor rejects FAILS
```

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
declared that refusal, and the runner reports only the lines the step did not pin. `exists` and
`not_empty` on that path declare nothing, since both hold for `OK` and for a refusal. A path that no
response message declares fails `shrt run` before any traffic is sent: a convention that can never
fire is a config error, not a quiet pass.

`GRAMMAR.md` §4 is the key table. `shrt init` prints every key with its default when it writes the
config, and writes no comment lines into the file, so a repo whose pre-commit hook blocks new
comments can commit it as written.

## 4. Assert something that can fail

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
chain.** `error.code == OK` on a `Fetch`/`List`/`Get`/`Search`/`Preview` whose body came back empty
is green about the opposite of what its author meant. `shrt chain hollow` reads the run records and
names every one:

```bash
shrt chain hollow            # exit 1 while any is unexplained, exit 2 if there are no records at all
```

Fix one by asserting what the read should have found. If an empty body really is the right answer —
a probe pinning a refusal, a cap, a filter that rejects a bad id — say so in `.shrt/hollow-allow.txt`
with the reason; an entry without one is refused. `PITFALLS.md` §24.

## 5. Probe one failure code

The unit of failure coverage is a `(rpc, app_code)` pair that some run record has actually
observed. To turn a declared code into an observed one:

```yaml
    - id: confirm_twice
      description: |
        The second confirm must be refused. Names the code in the assertion so the receipt
        distinguishes "refused for the right reason" from "refused for any reason".
      call: acme.orders.order.v1.OrderActionService/ConfirmOrder
      body: {id_order: ${create_order.id_order}}
      allow_fail: true
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

- `allow_fail: true` is not what makes this step green: an in-band refusal that matches these
  expectations passes on its own. `allow_fail` only tolerates a transport-level refusal on a step
  that asserts nothing — it never waives a failed or unevaluated expectation, and it never lets a
  chain run past a step that did not pass. To see what lies behind the first red, run
  `shrt run -keep-going <chain>`: every step still runs, a step that reads a failed step's
  response or exports is recorded `skipped` instead of being sent, and the run stays failed with
  every red step listed.
- Assert on `error.details.0.app_code` **and** `reason` — but **only for a named business failure**.
  `error.code` alone is a Connect code that a dozen unrelated refusals share.
- **A shape error has no `app_code` and no `reason` to assert on.** `errmsg.NewShape` and
  `errmsg.NewAuth` both build the envelope with `Detail: nil`, so `details` comes back `[]`. Not a
  rare case but close to half the surface — **336 of the 738 declared failures in
  `.shrt/contracts/` carried no numeric `code` on 2026-09-12**, and every one of them is this shape.
  The count moves with every contract edit, so re-derive it rather than quote this line:

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
  reads better with it — put it first, give it `skip_auth: true`, and its token seeds the cache so
  later steps do not log in twice. It seeds only a profile whose `body` it sent verbatim; a login as
  anyone else seeds nothing, and its `note` says so. Each step's `auth_profile` in the run record
  names the profile it ran under.
- Never `skip_auth` plus a hand-written `Authorization` header. That is the workaround profiles
  replaced, and lint rejects `skip_auth` and `auth` together.
- **Probe that a missing or invalid token is refused** with the two sanctioned forms, never a
  hand-written header:

  ```yaml
      - id: fetch_without_token
        call: acme.orders.order.v1.OrderService/FetchOrder
        body: {id_order: ${create_order.id_order}}
        skip_auth: true
        expect:
          - path: transport.code
            equals: unauthenticated
          - path: transport.http_status
            equals: 401
      - id: fetch_with_bad_token
        call: acme.orders.order.v1.OrderService/FetchOrder
        body: {id_order: ${create_order.id_order}}
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

**A domain is the SECOND dotted segment of the service name** — `DomainOf` reads
`<pkg>.<domain>.…` and nothing about it is specific to any one backend. Wherever a surface groups
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

**`shrt contract status` counts entries; `shrt contract quality` measures them.** The CONTRACT,
REACHED and VERIFIED columns of `status` go to their ceiling the moment a scaffold lands — N-of-N,
and 200/200 the day after 76 empty scaffolds landed — so a full column says only that somebody ran
`contract init`. The one thing they do tell you is when RPCS **exceeds** CONTRACT: that is an rpc
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
lists them as `no path to`; the column cannot tell the two apart and does not try.

**The score measures OMISSION as well as vagueness, and it did not always.** Until 2026-09-12 every
term asked whether something *declared* was vague, so declaring nothing scored best of all: a bare
`shrt contract init` scaffold scored **0** and sat exactly at the gate's floor. `PITFALLS.md` §18 is
the receipt. What the pair of them measures now, and what each term is worth:

| # | weight | phase | term |
|---|---|---|---|
| 1 | 2 | happy | a request field in the descriptor with no `fields:` entry and not in `required:` |
| 2 | 2 | happy | an id field with no `from` / `same_as` / `value` — unless it is optional AND its `note:` says why |
| 3 | 2 | failure | a write rpc whose own `failures:` is empty; a domain-wide block does not satisfy it |
| 4 | 2 | happy | a missing `summary` |
| 5 | 2 | happy | an rpc with no `requires_role:` at all — `NONE`, alone, is how you say "no role gate" |
| 6 | 2 | happy | a READ rpc no write rpc can reach — unless `no_producer:` says why |
| 7 | 2 | happy | an rpc with request fields and an empty `required:` — `NONE`, alone, says the server rejects nothing |
| 8 | 1 | failure | an id wired by `from`/`same_as` with no `checked_by:` |
| 9 | 1 | happy | a response field named in no `exports:`, `terminal:` or `soft_signals:` |
| 10 | 1 | failure | a failure with no `when:`, `unreachable:` or `pending_deploy:` |
| — | — | — | codes the backend raises that no contract declares at all |

The ten scored rows come from `QualityTerms()` in `core_distillation/contract/quality.go`, which is what
`shrt contract status` prints in its footer and what computes the score — so run the command for
today's list, and read the rows below for the reasoning a one-line label cannot carry. A term
cannot exist in code without a label there; `internal/guard` fails the build if one does, and
`TestPlaybookQualityTableMatchesTheCode` fails it if this table and `QualityTerms()` disagree on how
many rows there are, on a weight, or on a phase.

**An unfilled `TODO` scores nothing.** It is listed beside the score as a hint, never charged. The
row that used to claim `1 per unfilled TODO` was wrong for as long as it stood: no `QualityTerm`
counts `UnfilledTodos`, so clearing every TODO in a file moves the score only where clearing it also
answers one of the ten rows above.

**The phase column is what `-phase` filters.** `shrt contract quality -phase happy` scores the seven
rows a working chain needs and ignores the three that curate refusals; `-phase failure` does the
reverse. Curate happy first: a domain at happy-0 composes and runs, and nothing about the three
failure rows changes that. The default is still every row, so a gate pinned to a baseline is
unaffected.

Read-only rpcs (`Fetch`/`List`/`Get`/`Search`) are exempt from rows 2 and 3: an unwired read filter
is a filter left off, and every rpc in this corpus with no `failures:` of its own is a read that
refuses only in the domain-wide ways. Row 6 is the mirror of that exemption and applies only to
reads.

**Row 6 asks whether a producer EXISTS, not whether `needs:` names it.** A read rpc satisfies it
when any of four things is true: a `needs:` entry names a write rpc; a `fields.<f>.from` or
`same_as` points at a write rpc's response; an alias override does; or some write rpc in another
domain declares `before:` this read. All four are edges `shrt contract plan` walks, so the term
fires exactly when `plan <read>` would compose a chain **one step long — the read alone**. Requiring
`needs:` specifically was measured and rejected: it fires on 18 of the corpus's 49 read rpcs and all
18 already compose correctly through a `from:` edge, so every one would have been a false positive.

**Row 5 charges silence, not a wrong role list.** Nothing in the descriptor says which roles a
procedure needs — that lives in the backend's policy rows — so the score can see the key's absence
and nothing more. `requires_role:` is what makes `plan` print `caller must hold role …`; without it
a chain author meets app code **1603** at run time instead. Measured 2026-09-12: 12 of 124 rpcs
declared nothing, of which 4 were real omissions (`FetchInstrumentAccess` needs ACCOUNTING+ADMIN per
migration `000047`; `FetchManagementCash` and `RecordMovementFeePayment` need MANAGER+ACCOUNTING per
`000195`; `ChangeStaffPassword` is granted to all four staff roles per `000042`) and 8 were the
partner surface plus staff `Login`, which reach no staff role gate at all and now say so with
`requires_role: [NONE]`. `NONE` beside a real role is a lint error.

**The three exemptions are deliberately different keys, and must stay different.** Row 6 is spared by
`no_producer:`, row 5 by the literal `NONE` inside `requires_role:`, and row 7 by the literal `NONE`
inside `required:`. One shared escape hatch would mean a note written about a missing producer
silently also settles the role question — which is the same failure as a scaffold note that answers a
question nobody asked. `note:` spares none of the three: it is prose about a field, and the score
cannot read it.

**`summary:` does NOT spare row 6, deliberately.** Every rpc has a summary — row 4 charges its
absence — so accepting one as the explanation would make row 6 fire on nothing.

**One undocumented exemption, now documented: a response field that is a non-repeated message is
not counted by row 9.** `referenceableResponseField` in `core_distillation/contract/quality.go` skips `error`,
and skips message-typed fields unless they are `repeated`, because a bare nested message has no
scalar to reference and `exports:` names paths a later step can read. So a score of 0 guarantees
every SCALAR and every REPEATED response field is accounted for in `exports:`/`terminal:`/
`soft_signals:` — it does not guarantee that a singular nested message was looked at. Reach into it
with a dotted path (`row.id_reference_rate`) when a later step needs the value.

Every row but the last needs only the descriptor and the overlays, so they live in the binary. **The
last one cannot**: finding the codes a backend raises means reading that backend's source, and shrt
drives a backend it never imports. That one cross-check, and nothing else, stays in
`scripts/contract-quality.py` — which **exits 2 rather than 0 when it cannot find that backend**,
because a check that cannot run must fail. `scripts/check.sh` gates both as a **ratchet**: the score
may fall, never rise, and undeclared backend codes must stay at zero. Improve a contract and the gate
tells you to lower the baseline in `scripts/contract-quality-baseline.txt`. That is what stops an
N-of-N score from meaning less each time the backend grows.

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
shrt run <name> -dry-run        # resolves references and validates bodies, sends nothing
shrt run <name>
```

Read the run status as three values, not two: `passed`, `failed`, and **`error`** — and `error` is
evidence about your fixture, never about the backend. That trap is `PITFALLS.md` §4; the vocabulary
it belongs to is `GRAMMAR.md` §8.

Then stop and hand the confirmation to the user:

```
shrt confirm <name> -run <run-id> -by <their-name> -i-verified -note "..."
```

## 9. Refactor and test against a safe spot

This is what the whole loop is for, and it is the section most likely to be skipped, because a
chain that has never been confirmed still runs and still goes green.

**A passing run and an unchanged run are different claims.** `shrt run` asks whether the
assertions still hold. `shrt verify` asks whether the RESPONSE still matches what a human confirmed
was correct — every field, not just the ones somebody thought to assert. With 43% of steps in this
corpus asserting only the error envelope (`PITFALLS.md` §17), the second question is the one that
catches a refactor that changed a number nobody was watching.

Before you touch the code:

```bash
shrt run <name>                                  # a fresh receipt on today's binary
shrt verify <name> -run <run-id>                 # does it still match the safe spot?
```

After you touch it, the same two lines. A drift report names the step, the path, `want` and `got`,
so a regression arrives as *which rpc changed* instead of a failing test somewhere downstream.

Three things that decide whether this works for a given chain:

- **`shrt verify -run <id>` re-diffs a RECORDED run and sends nothing.** It needs no backend and no
  credential, so the after-check costs one run, not two. It is also how you investigate a drift
  without spending another live run.
- **A chain that creates things is re-run with a fresh `-var tag`, so every tag-derived value
  legitimately differs.** Those paths must be in `volatile`, or the first replay reports a
  regression that is not one. This is per-chain work and it is why paving the corpus is not a bulk
  operation — see the one worked example, `.shrt/safespots/seed-position-exposure.json`, whose
  `volatile` list is 14 patterns long.
- **A chain in the expect-fail set must NEVER be confirmed.** Those chains assert a pre-fix defect,
  so a safe spot would freeze the bug as ground truth. `PITFALLS.md` §11.

**Before a chain has a safe spot, `shrt diff` is the run-to-run check.** Only a human may create a
safe spot — never run `shrt confirm` yourself — so a refactor often has to be checked with none:

```bash
shrt run <name>                                  # before the change
shrt run <name>                                  # after it
shrt diff <name>                                 # latest~1 against latest
shrt diff <name> <run-a> <run-b>                 # or any two runs; ids, latest, latest~N
```

It reports step status changes, where the first failing step moved, steps reached in one run and
not the other, and response differences in steps both reached. The chain's `volatile` paths and
config `volatile` are masked as `verify` masks them, and so are ids and timestamps, which differ
every run; the report says how many values it hid. It exits 1 when the runs differ. It is a
comparison between two runs, not a verdict: it cannot tell you which of the two is right, only
that they disagree and where.

`scripts/verify-all-flows.sh` prints `safe spots: N of M chains` in its header and diffs every
chain that has one, reporting `DRIFT` separately from `UNEXPECTED` — the assertions held, the state
did not. The rest of the corpus is checked for PASS/FAIL only. That header line is the honest
measure of how much of the corpus is actually a regression baseline rather than a smoke test.

## 10. Find the chain first: which one exercises this rpc, or asserts this code

`chain slice` cuts one chain down to one step. It cannot tell you WHICH chain and WHICH step — and
an agent holding a failing rpc name, or an `app_code` out of a production envelope, has only that.
`chain which` is the index over the corpus that closes the gap, and it ends every match with the
`chain slice` command to paste.

```bash
shrt chain which -code 1218
shrt chain which -rpc ObligationActionService/ApproveOffsetObligation
shrt chain which -rpc ProposeOffsetObligation -code 1204
shrt chain which -code 1218 -json
```

1. **Two selectors; naming neither is an error, not a listing of everything.** `-rpc` takes the same
   shorthand `contract show` takes and resolves through the same catalog, so `Service/Rpc` and the
   fully qualified form find the same steps. `-code` matches an `equals` wherever a chain can name a
   failure: the envelope code, an `app_code` detail, a `reason` detail. The searchable paths are
   derived from the corpus, so a chain asserting a code under a batch result — the corpus has
   `results.0.error.details.0.app_code` — is found without teaching the command a new shape. Both
   selectors together intersect.
2. **`OBSERVED` outranks `asserted`, and they are different claims.** `asserted` means the chain
   says that step answers that code. `OBSERVED` means a run record under `.shrt/runs/` shows that
   step actually answering it — `README.md`'s "where the authority is" rule, applied to discovery. A
   chain that claims 1218 and a chain that has been seen answering 1218 are not equally good
   answers to "what reproduces this". Run records are gitignored and machine-local, so a clone with
   none reports `no local runs` and still ranks by the assertions. An `OBSERVED` line reads
   `asserts <code>` for the claim and `run <id> got <code>, step <status>` for the record: `got` is
   the code the recorded response carried, never the asserted one, and a step that failed its
   assertions says `step FAILED`. `-json` carries the same two facts as `asserts` and `observed`.
3. **The last line of each block is the deliverable.** It is a `chain slice` invocation, and for an
   `OBSERVED` match it is the `-mode pin -run <id>` form, because pinning that run's values is the
   cheaper reproduction of the same incident. Paste it; do not retype it from the columns.
4. **`slice k/n` is the CLOSURE size, not the size of the pinned command printed under it.** It is
   the mode-independent cost of reaching that step, so the numbers are comparable across chains;
   `-mode pin` can only drop steps, never add them.
5. **No match exits non-zero and says so.** An empty list and a green exit is the failure this repo
   cares most about, so "the corpus does not exercise it yet" is an error, not a quiet success.

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
   `produces ${x} used by <step>`, or `contract needs <rpc>` — the third kind comes from
   `needs` / `before` / `from` / `same_as` in `.shrt/contracts/`, which is the only reason a step
   with no textual link to the target survives at all.
2. **Read the two named sections before trusting the count.** `unmet prerequisites` means a
   contract edge names an rpc *no earlier step calls*, so the slice may not stand alone.
   `WARNING possible under-inclusion` means the slice is under half the chain and the dropped steps
   include WRITES: nothing in the YAML records that step 61 needed the row step 20 wrote, so the
   slice can be too small and still go GREEN. That failure mode is worse than no slice.
3. **`-write` then `chain lint` it.** A slice that does not lint is a failed slice, not a smaller
   chain. Without `-write` nothing is written and the YAML goes to stdout.
4. **`-verify -run <id|latest>` is what turns the claim into a receipt.** It runs the slice and
   compares the TARGET step's verdict — the code at `conventions.envelope_path` and the pass/fail
   of every expectation — against that same step in the source run. It prints one of four
   outcomes, each with its own exit code:
   - `reproduced` (0): the verdicts match and the slice dropped no write step.
   - `NOT REPRODUCED` (1): the target ran and its verdict differs from the source run's.
   - `DID NOT RUN` (2): the target was never sent — an unset `${env.X}`, a login that failed, a
     step before it that errored. Nothing was compared; fix the cause the line names and re-run.
   - `INCONCLUSIVE` (3): the verdicts match, but the slice dropped write steps. A match can come
     from state the slice never built (a limit the dropped writes would have reached, say), so it
     is not a receipt. Put the writes back into the written slice and compare again.
   Until you have a `reproduced`, the slice is a hypothesis, and the command says so on its own
   last line.
5. **`-mode pin -run <id|latest>` when you want the fast reproduction, not the buildable one.**
   A producer whose only contribution was a VALUE is dropped and its value pinned into `vars:`,
   with every reference rewritten to `${vars.<name>}`. A step that contributed a SIDE EFFECT — it
   is there via `needs` / `before` — is never pinned away. Pin mode needs a run record and refuses
   without `-run` rather than quietly falling back to closure.
6. **A pinned slice reproduces one incident, not the flow.** Its ids are the ids of that run, so it
   is dead the moment that data is. Confirm a safe spot from a closure slice, never a pinned one.
