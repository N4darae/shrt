# PLAYBOOK

Procedures. Keys are in `GRAMMAR.md`; symptoms in `PITFALLS.md`. Examples use the default envelope,
`error.code` with `OK` for success; substitute your `conventions.envelope_path` and `envelope_ok`
(§3b).

---

## 1. Compose a chain — the fast path (a contract exists)

```bash
shrt contract show InvoiceService/PayInvoice          # read before you generate
shrt contract plan InvoiceService/PayInvoice          # preview
shrt contract plan InvoiceService/PayInvoice -write   # write .shrt/chains/<name>.yaml
```

`plan` walks `needs`, `before`, `from` and `same_as`, sorts them and wires every `${...}`. The
preview prints a short summary, never the chain; `-write` writes the chain and prints the same
summary:

```
order: CreateAccount -> CreateInvoice -> PayInvoice
12 steps: 2 setup, 1 target, 4 token/role, 5 unknown id
fill: create_account.name has no usable value: set fields.name.value in CreateAccount's contract
gap: step pay_invoice: PayInvoice says nothing of what it gives back from PAID (add effects: {balance: {restore: PAID}} if ...
3 more note(s) on why each probe is there, and each gap in full: shrt contract plan PayInvoice -notes
next: shrt contract plan PayInvoice -write
```

- **The `order:` line is the claim to check.** `plan` wires what the contracts say; it cannot
  tell whether the flow makes business sense.
- **Every `fill:` line is test data you owe.** Add the `value:` to the contract, then re-plan with
  `-write -force`; a chain lint ERRORs on each unfilled field.
- **Every `gap:` line is something the plan could not plan or assert.** Say it in the contract and
  re-plan, or write that step by hand.
- **`-notes` prints each gap in full, then one line per probe group** with its step count and
  why it is there; `-notes -v` prints every note in full and every step id, `-v` alone the ids.
- **Compose a whole flow at once:** `shrt contract plan PayInvoice GetInvoice ListInvoices
  CancelInvoice@paid -write`. Each target may carry `@alias`; a node reached twice appears once.
- **A step the plan left out means the contract is missing an edge.** Fix the contract and
  re-plan instead of adding the step by hand.
- **The same rpc twice** (a read before and after a write) needs one alias per instance
  (`aliases: {before: {note: ...}, after: {note: ...}}`), then
  `shrt contract plan GetAccount@before PayInvoice GetAccount@after -write`. Unordered targets
  keep the order you name them in.

## 2. Compose a chain — no contract yet

```bash
shrt catalog ls -filter <word>
shrt catalog describe acme.billing.invoice.v1.InvoiceService/CreateInvoice
shrt chain new -name invoice-happy-path InvoiceService/CreateInvoice InvoiceService/GetInvoice
```

`chain new` wires a later step's input to an earlier step's output when a contract says so, and
names a producer that is not in the list in a note. Then write the contract for what you learned
(§7): the same work in the overlay composes the next chains.

## 3. Fill the bodies

`plan` calls a value unusable when the server will reject it: `""`, the zero enum member
(`*_UNSPECIFIED`), a `"0"` int64 placeholder, a zero number, a repeated field of blank entries.
`chain lint` is laxer on one case: it cannot tell a scaffold `"0"` from a deliberate zero, so only
the plan header reports a leftover numeric zero. Say a zero is deliberate with `value: "0"` in the
contract.

**A repeated message field gets two items; keep two.** One item leaves everything that combines
items untested (a total, a check on the second item). The second item is a copy of the first
with numbers raised and text prefixed `2-`; a reference to a create step points at a second copy
of it (`create_account_2`, with its own unique values and its own preparation steps). Give the
second item its own data where it matters and assert what depends on both. `plan` also varies the
count (one item and three) for write targets. `shrt contract status -gaps` lists fields no chain
sends with two items (`one item`), with two items on one resource only (`same resource`), or never
with one resource twice (`no repeat`).

Habits that keep a chain re-runnable:

| need | write |
|---|---|
| an idempotency key | `${uuid}`, never a literal |
| a value two steps share | one reference to the first step's request (`${steps.a.request.x}`) or one var |
| a unique name per run | `inv-${vars.tag}-a`; an undeclared `tag` is fresh on every run |

`plan` leaves `tag` undeclared, so every run gets a fresh one and none needs `-var`. When a run is refused as a duplicate, `run` and `verify` say
which (PITFALLS §23): `fixture reused` or `fixture collision` (exit 3, re-run with a fresh `-var`),
or `the chain collides with itself` (exit 1: build the literal field from a var).

## 3b. Tell shrt how YOUR backend answers

`.shrt/config.yaml` `conventions:` has six optional keys. The defaults describe a backend with its
verdict at `error.code` and `OK` for success. An example for a backend answering
`{"status": {"code": "SUCCESS"}}` with batch items `{"results": [{"status": {...}}]}`:

```yaml
conventions:
  read_only_prefixes: [Fetch, Get, List, Query, Read]
  envelope_path: status.code
  envelope_ok: SUCCESS
  item_envelope_path: results[].status.code
  code_fields: [app_code, reason, error_code]
  validate_output: true
```

- `read_only_prefixes`: which rpc names are reads, matched at a word boundary (`Get` covers
  `GetInvoice`, not `Getaway`).
- `envelope_path` / `envelope_ok`: where a response states its verdict, and the success value.
- `item_envelope_path`: a batch's per-item verdict. **Check it first on any backend with batch
  rpcs**: without it a batch that refuses every line passes. Verify it once against a response you
  know refused a line. It applies only to responses whose message declares that list and field.
- `code_fields`: detail fields `chain which -code` searches and that can pin a refusal.
- `validate_output`: a response the descriptor rejects fails its step with `"drift": true`.

A path no response message declares fails `shrt run` before anything is sent. With the login
credentials exported, `shrt init` logs in once and writes `envelope_path`, the `envelope_ok` it read
and an unambiguous `item_envelope_path`; otherwise it exits 3 naming the variables to export
(`-v` prints the block to paste). It never rewrites a
`conventions:` block already there. `GRAMMAR.md` §4 is the key table.

## 4. Assert something that can fail

A step whose only expectation is `error.code == OK` asserts the server did not crash. Say what the
call *did*:

```yaml
expect:
  - path: error.code
    equals: OK
  - path: invoice.amount_minor
    equals: ${vars.amount}
  - path: account.balance_minor
    equals: ${steps.account_before.response.account.balance_minor}
```

A `${...}` in `equals` / `not_equal` / `contains` and in the numeric bounds resolves at run time,
so a chain can state an invariant between two values. It does no arithmetic: to assert
*after == before + added*, choose the inputs, work the result out, and assert it as a literal or a
var. A `${...}` in `path` is a lint error. Rules and their exact behaviour: `GRAMMAR.md` §1.

**What `plan` asserts and probes by itself**, from what the contracts state (read the notes; do
not delete a probe that fails, pin a real defect with `kept_red`, §9):

- the verdict, each numeric field a write sent echoed back, a created id, the state the contract
  names, and a timestamp range (`within: {of: "${nowunix}", by: 300}` for a stamp the call makes,
  an expiry against the stated lifetime, `equals` for a creation stamp read back);
- for a list: three fixtures whose candidate sort keys disagree, positions by id when the contract
  states the order, otherwise membership (`includes:`) and a count; a fixture outside the filter's
  scope (another parent, a prefix not at the start), and one list per reachable state for an enum
  filter; a list the run does not scope is declared `volatile`;
- for a uniqueness failure: the same value again, the same value with every other field changed,
  and a case or whitespace variant when the contract says the comparison ignores case or trims
  (`unique: {case: ignore, trim: true}` states it as data);
- for a quantity or limit failure: a request just past the limit and one exactly at it, on the first
  and the last item, with reads before and after asserting a refused write changed nothing;
- a failure whose `when:` names a state, probed from that state; a not-found failure, probed with
  an id nothing created; `invalid_argument` clauses in `when:` (empty, zero, negative, missing `@`)
  turned into malformed requests;
- with auth configured: each target without a token and with `auth: invalid`; a role-gated rpc
  as each profile not holding the role; an rpc every role may call, repeated as each profile and
  compared;
- the exact number a write moves on a record it names, on the write and on the read after it, and
  a total over lines, with one line past 2^32, when `effects:` states them (§7);
- numbers of different magnitudes, a large value, minimum and maximum boundaries a note or `when:`
  states, long and multi-byte text;
- a batch with a refused item first, in the middle and last; an idempotency key replayed with the
  same and with another body.

Each probe group runs on fixtures of its own (`<fixture>_for_<group>`), so one defect fails one
group. `shrt contract status -gaps` lists what no chain exercises yet.

**A read that passed and found nothing** is invisible in the chain file. `shrt chain hollow`
reads the run records (runs and verify replays of chains under `paths.chains`) and names every read
step that passed with an empty body; exit 1 while any is unexplained. Fix it by asserting what the
read should find. A probe pinning a non-OK verdict, and `<list>.0 exists: false`, already say
empty is right; for any other case add `<chain> <step-id> <reason>` to `.shrt/hollow-allow.txt`.

## 5. Probe one failure code

The unit of failure coverage is an `(rpc, code)` pair some run record observed. Check one pair with
`shrt chain which -rpc R -code C` (§10). To turn a declared code into an observed one:

```yaml
    - id: pay_twice
      description: The second payment must be refused, for this reason.
      call: acme.billing.invoice.v1.InvoiceService/PayInvoice
      body: {id_invoice: "${create_invoice.invoice.id_invoice}"}
      expect:
        - path: error.code
          not_equal: OK
        - path: error.details.0.app_code
          equals: "1242"
        - path: error.details.0.reason
          equals: InvoiceAlreadyPaid
```

- The refusal line on the envelope (or on `transport.*`) is what makes lint treat the step as a
  probe; without it lint demands every required field the probe omits (PITFALLS §18).
- No `allow_fail`: an in-band refusal matching these expectations passes. `allow_fail` only
  tolerates a transport refusal on a step with no expectations.
- Assert `app_code` **and** `reason` for a named business failure; the envelope code alone is
  shared by many refusals.
- **A shape or auth error has no `app_code` or `reason`.** Read the failing step's `http_status`:
  - 200 with the verdict in the body: assert the envelope code, `error.message contains: ...`, and
    `error.details.0.reason exists: false`.
  - 4xx Connect error: there is no body. Assert `transport.code equals: invalid_argument` and
    `transport.message contains: ...`.

Before writing the probe, check the contract's `unreachable:` and `pending_deploy:`.

To see what lies behind the first red, `shrt run -keep-going <chain>`: every step still runs; a
step reading a failed step's response is recorded `skipped`. It prints the steps that did not pass,
one `N step(s) unevaluated behind <step>` line per cause and a passed count; `-v` prints every step.

## 6. Cross a principal boundary

`.shrt/config.yaml` declares the login once; the runner attaches the header and logs in again on
expiry. A call answered unauthenticated drops the token and logs in fresh; a read, or a call whose
cached token no call had used yet, is re-sent (`auth_retry: resent`). A write refused with a token
already accepted is not re-sent (`auth_retry: not_resent`, exit 3). The step records which profile
it ran as (`auth_profile`).

- A second principal is a named profile under `auth.profiles`; put `auth: <profile>` on the step.
- A login step is optional. It needs no `skip_auth`, and seeds a profile's token only when it sent
  that profile's `body` exactly.
- Never a hand-written `Authorization` header, with or without `skip_auth`: lint rejects it and
  `shrt run` refuses the chain.
- Probe a missing and an invalid token with the two sanctioned forms:

  ```yaml
      - id: get_without_token
        call: acme.billing.invoice.v1.InvoiceService/GetInvoice
        body: {id_invoice: "${create_invoice.invoice.id_invoice}"}
        skip_auth: true
        expect:
          - path: transport.code
            equals: unauthenticated
      - id: get_with_bad_token
        call: acme.billing.invoice.v1.InvoiceService/GetInvoice
        body: {id_invoice: "${create_invoice.invoice.id_invoice}"}
        auth: invalid
        expect:
          - path: transport.code
            equals: unauthenticated
  ```

  `auth: invalid` sends a token the backend never issued, in the covering profile's header, and
  never logs in again. If your backend reports unauthenticated in the body with HTTP 200, assert
  the envelope path instead.

How `run` and `verify` read auth refusals: a token refused after it was accepted earlier in the run
points at a restart (exit 3). The same refusal at the same step in the previous run, with no
restart shown, is a finding (exit 1). A token refused long before the expiry its login stated
prints `WARNING: token refused <N>s after issue ...`; settle it with a chain of held reads
(PITFALLS §49). A step with no answer (dropped connection, timeout) is exit 3; the same in the
previous run while later steps answered is a `FINDING`. A read with a server error is re-sent once
and judged on the answer; a server error on a request answered then or elsewhere is
`FINDING: intermittent failure at <rpc>`. Writes are never re-sent.

## 7. Author or extend a contract, in payoff order

```bash
shrt contract quality                  # what is missing — start here
shrt contract init <domain>            # scaffold; re-running keeps what you wrote
shrt contract lint
```

**A domain is the package segment after the organisation root, once versions are dropped**:
`acme.billing.invoice.v1.InvoiceService` is domain `billing`; a reverse-DNS root (`com`, `org`) is
dropped too. A surface that groups privileged writes under one segment (`acme.admin.*`) puts them in
that overlay, not beside what they are about, so `catalog ls -filter billing` can list more rpcs
than `billing.yaml` has. When a domain's writes look too few for its reads, grep the catalog.

**`contract status` counts entries; `contract quality` measures them.** CONTRACT fills the moment
a scaffold lands. RPCS above CONTRACT is an rpc no overlay covers. REACHED counts rpcs in some
multi-step plan; below it, a write that cannot run alone is missing a `needs:` or `from:`.
`status -gaps` lists them as `no path to`.

The score (the footer of `shrt contract status` prints today's list):

| weight | phase | term |
|---|---|---|
| 2 | happy | a request field neither in `required:` nor documented in `fields:` |
| 2 | happy | an id field with no `from` / `same_as` / `value` (unless optional and its note says why) |
| 2 | failure | a write rpc with no `failures:` of its own |
| 2 | happy | a missing, `TODO` or under-three-word `summary` |
| 2 | happy | no `requires_role:` (`[NONE]` says no role gate) |
| 2 | happy | a read no write can reach (unless `no_producer:` says why) |
| 2 | happy | request fields and an empty `required:` (`[NONE]` says nothing is required) |
| 1 | failure | a wired id with no `checked_by:` |
| 1 | happy | a top-level response field in no `exports:`, `terminal:` or `soft_signals:` |
| 1 | failure | a failure with no `when:`, `unreachable:` or `pending_deploy:` |
| 2 | happy | a unary rpc no overlay covers, scored as an empty entry plus this |

Reads are exempt from the id and write-failure terms. `-phase happy` scores what a working chain
needs; curate it first. An unfilled `TODO` is not charged by itself. `note:` spares no term:
`no_producer:`, `requires_role: [NONE]` and `required: [NONE]` are the three exemptions, kept
separate on purpose. Gate the score as a ratchet with `shrt contract quality -gate -baseline
<file>`: it fails when the score rises, and when it falls without the baseline being lowered.

**Score 0 is necessary, never sufficient.** No term sees an incomplete `needs:`, and `before:`
has no term. Fill in this order; each step pays for the next:

1. **`required`**, from the backend source, reads included. `required: [NONE]` if the server
   rejects nothing; `required: [UNKNOWN]` if you could not find the handler.
2. **`fields.<f>.from` / `same_as`**, the ordering edges. Wrong ones show in the `# order:` line.
3. **`needs` / `before`**, the prerequisites nothing references. For `needs:`, list every row the
   handler reads whose identity comes from no request field; each is a `needs:` on the write that
   creates it. For `before:`, ask which rpc in another domain consumes what yours writes. Then run
   `shrt contract plan <read>` for every read: a one-step order has no producer, and an order that
   creates the entity but never the row being read is the same bug, quieter. A `from:` wires the id
   you filter by; `needs:` names the write that put the row there.
4. **`failures`**, one per way the rpc refuses, `when:` written as a condition. `code` is optional
   for a shape error. `reason:` is the backend's string verbatim for a coded failure, and a label
   you invent for a shape or auth failure.
5. **`effects`**, for a write that moves a number a record holds. `summary` is prose for people;
   `plan` asserts levels and totals from this key, and a `gap:` prints the one to add:
   `{balance: {increase: amount}}` (`decrease`; `lines.amount` moves once per line),
   `{balance: {decrease: lines.amount, of: id_invoice}}` (per line of the record `id_invoice` names),
   `{balance: {restore: POSTED}}` (gives back what that took, from a POSTED record),
   `{balance: none}` (leaves it alone), `{balance: zero}` (a create starts it at 0),
   `{total_minor: {sum: lines.qty, times: unit_price}}`, `{lines: per_item}` (each line applied or
   refused alone).
6. **`exports`, `terminal`, `soft_signals`**: what the response is for. `terminal` records a dead
   end instead of deleting it.
7. **`source`**: the files you read, without line ranges.

`before:` always attaches the plain rpc; to order aliases, list them in the dependent rpc's
`needs:`. Leave `status: draft`: only a human sets `verified`. A codes-vs-contract cross-check needs
a reader of your backend's error constructor, so it is a script you write (PITFALLS §37).

## 8. Run, hand off, verify

```bash
shrt chain lint <name>          # fix every error before sending traffic
shrt chain lint -strict <name>  # and every assertion-quality warning, before it is a gate
shrt run <name> -dry-run        # resolve and validate, send nothing; does not lint
shrt run <name>
```

Progress lines start with the step status: `ok`, `FAIL` (answered, an expectation did not hold),
`ERROR` (the step never completed), `SKIP` (held back under `-keep-going`), `--` (dry run).
`shrt run` refuses before sending anything a chain with a missing var, an unset env var, a
reference to a field the producing message does not declare, or a body the proto rejects in any
step (`run -h` lists every refusal). A step refused in-band fails unless an expectation pins the
verdict (PITFALLS §17). Read `error` as a problem with the fixture first (PITFALLS §4).

**Run the chain twice, then propose:**

```
shrt confirm <name> -run <run-id> -note "what you inspected in the responses, and why it is right"
```

This writes `.shrt/safespots/pending/<name>.json` and a report beside it, and prints a summary
table: per step what was sent, asserted and answered, then `also baselined:` values nobody asserted
that still become the baseline. It warns about fields that differ from the previous passing run
(declare them `volatile` unless the change is real) and about a step whose whole response is
volatile. A `-supersede` proposal adds a column against the safe spot it replaces.

**Present the proposal in the conversation, in the user's language; do not send a file path:**

1. one line: chain, run id, `n/n` steps passed, what the chain exercises;
2. the table, each step as a plain situation ("pay the same invoice again"), what was sent, what
   came back, whether that is right;
3. the one or two facts that carry the verdict, and anything you are unsure of;
4. what approving means: every response field not under a volatile pattern becomes the baseline,
   and any warning the summary printed;
5. the question: approve or reject.

Run `shrt confirm <name> -approve -by <user email>` only after the user answers yes to THIS
proposal; ask for the email if you do not know it. `-reject` discards it; `-pending` lists
proposals. `confirm` refuses a run record whose seal does not match (edited by hand) and, on
`-approve`, a chain file that differs from the one the proposed run ran: approve on the branch the
proposal came from. A chain with a safe spot needs `-supersede`; the old one is archived under
`.shrt/safespots/archive/<chain>/`.

For a whole suite, `shrt confirm -all -note "..."` proposes every chain whose latest run passed and
whose safe spot is missing or differs, as one table row per chain (the full summary of each is in
`.shrt/safespots/pending/<chain>.md`). Present the table; `shrt confirm <chain> -reject` each one the user refuses, and only after the user said yes
to the rest, `shrt confirm -all -approve -by <user email>` approves every pending proposal.

A safe spot belongs to the chain name. For a pure rename, `shrt confirm <new> -rename-from <old>
-by <email>` carries it across; any other difference is refused.

**Two branches superseded the same safe spot.** Never hand-merge the JSON:

1. Take one side whole: `git checkout --ours .shrt/safespots/<chain>.json` (or `--theirs`), the
   side whose chain file the merge keeps.
2. Deploy the merged backend and run `shrt doctor -strict` and `shrt verify <chain>`.
3. If it drifts, run, propose with `-supersede` and have a person approve.

## 9. Refactor and test against a safe spot

`shrt run` asks whether the assertions hold; `shrt verify` asks whether every response field still
matches what a person approved.

```bash
shrt run <name>                                  # before the change
shrt verify <name> -run <run-id>                 # re-diff that record, offline
# change the code, then the same two lines
```

Reading the report:

- Each change names the step, the path, its kind (`GRAMMAR.md` §7), `want` and `got`. A clean
  verify covers only that chain's steps.
- `targets differ:` first means the replay ran against another target.
- verify runs every step as `-keep-going` does; a step held back behind a failure is `not_reached`.
- It masks `volatile` paths, id- and timestamp-shaped values, and values that only echo a fixture
  name or a `${uuid}`, and counts them under `-v`; `-masked` lists them. A volatile value that was lost (null,
  empty, gone) is still reported. Anything else that changes every run belongs in `volatile`.
- It compares what each step SENT first. A changed request (`request differs ...`) or a changed
  chain (`chain differs ...`) gives `drift with different input` or `drift after a chain change`
  when it explains every response change, otherwise `regression`.
- A list whose order the rpc does not promise: declare `unordered: [<list>]`; otherwise a reorder
  fails with `order changed`.
- A field the descriptor gained and the backend does not send yet is not a change.
- `LATENCY:` lines flag a step at least 250ms and 3x slower than the safe spot's run, confirmed by
  re-sending a read; they fail only with `latency: {fail: true}` (`GRAMMAR.md` §4).
- A `regression` ends with the command for an intended change: `shrt confirm <name> -supersede
  -note "..."`, then a person approves.

**A chain kept red on purpose is never confirmed.** It asserts the correct behaviour and pins the
known defect with `kept_red` (step, path, and the `got` when stable). `shrt run` of it goes past
every failure, exits 0 while it fails exactly as pinned, and 1 when anything else fails, a pinned
step returns something else than in the last run that failed as pinned, or the defect is gone.

**One real defect in a long chain: keep it red in a slice, confirm the rest:**

```bash
shrt chain pin billing               # the failing steps kept red in a verified slice, billing without them
shrt run billing                     # green: propose and approve it
```

`chain pin` runs the chain with `-keep-going` first when its latest run did not reach every step,
and refuses when the slice does not reproduce or a FINDING or intermittent failure explains the red.
By hand: `chain slice <c> -step <id> -kept-red=<id,...> -verify -write`, then `chain slice <c>
-without failed -write .shrt/chains/<c>.yaml`. Remove the red slice and plan again once the defect
is fixed.

**No safe spot yet: `shrt diff`.**

```bash
shrt diff <name>                                 # the two latest runs that are not verify replays
shrt diff <name> <run-a> <run-b>                 # any two; ids, latest, latest~N
```

It reports status changes, where the first failure moved, steps no longer reached, what was sent,
and response differences, masked as verify masks them. It is a comparison between two runs, not a
verdict: if run A was wrong, "no differences" means B is wrong the same way.

## 10. Find the chain first: which one exercises this rpc, or asserts this code

```bash
shrt chain which -code 1218
shrt chain which -rpc InvoiceService/PayInvoice
shrt chain which -rpc PayInvoice -code 1204 -json
```

1. **`-rpc`, `-code` or both** (both intersect). `-code` matches an `equals` on the envelope code,
   a `code_fields` detail (`app_code`, `reason`), `transport.code` or `transport.http_status`. An
   `app_code` and a `reason` seen together in a run record or declared together in a failure count
   as one refusal.
2. **`asserted` is a chain's claim; `OBSERVED` means a local run record reached the step**, and
   the line under it cites the newest such run and what it got, even when that contradicts the
   assertion. Run records are machine-local; a fresh clone reports `no local runs`.
3. **The `reproduce:` line is the deliverable.** Paste it. It is `-mode pin -run <id>` for a read
   and a closure slice with `-keep writes` (or the plain closure first, with a fallback) for a
   write. Replace `<fresh>` in `-var <name>=<fresh>` before pasting.
4. **No match exits 1.** Under `-code`, when no chain asserts the code but a run record carried it,
   those steps are listed with a reproduce command instead, and whether anything else guards them.

## 11. A step failed: get the minimal reproduction

```bash
shrt chain slice billing -step pay_invoice_twice
shrt chain slice billing -step pay_invoice_twice -write probe
shrt chain lint probe
shrt chain slice billing -step pay_invoice_twice -write probe -verify -run latest
```

1. **Read the reason on every kept step**: `target`, `produces ${x} used by <step>`,
   `contract needs <rpc> (<edge>)`, or `changes the state <rpc> sets on <step>`. It keeps the
   producers of what kept steps reference, the contracts' prerequisites, and earlier writes that act
   on an entity the kept steps use.
2. **Read `unmet prerequisites` and `WARNING possible under-inclusion`** before trusting the size:
   a dropped earlier write can be state the target needed, and the slice can go green without it.
3. **`-write [name]`, then `chain lint` it.** `-write` and `-write <name>.yaml` put the file beside
   the source chain, where gates run it. A value with a slash is a path
   (`-write .shrt/scratch/<name>.yaml`, run with `shrt run .shrt/scratch/<name>.yaml`). `-force`
   replaces another file; a slice of the same chain and step is replaced in place.
4. **`-verify -run <id|latest>` turns the slice into a receipt.** It runs the slice (3 times by
   default, `-repeat N`) and compares the target step's verdict with the source run's: envelope
   code, reason and app code, transport refusal, and each expectation's pass, want and got. It
   writes the source run's vars into the slice, except fresh vars a kept write interpolates, which
   you must pass (`-var name=<fresh>`). Outcomes:
   - `reproduced` (0): the verdicts match and no dropped write touched an entity a kept step uses.
     With `-write` the verdict goes into the slice's description.
   - `NOT REPRODUCED` (1): the target's verdict differs; a `next:` line keeps the dropped writes
     that may explain it.
   - `DID NOT RUN` (2): the target was never answered (a kept step failed first, a login failed),
     or a refusal before sending (unknown step, a run that does not reach it, a missing fresh var).
   - `INCONCLUSIVE` (3): the verdicts match but dropped writes act on entities the kept steps use,
     or the source run was against another target. Run the `next:` line, which keeps only those
     writes; `-keep writes` keeps every earlier write.
   - `intermittent: reproduced k/N` (4): the backend is flaky there; keeping more steps will not
     help.
   Until a verdict, a slice is a hypothesis, and says so. `-run latest` is the newest run (a
   `shrt run` over an equally far verify replay); it refuses (3) if that run left the target unevaluated.
5. **`-mode pin -run <id>`** drops producers whose only contribution was a value and pins their
   values into `vars:`. It does not re-send writes the source run performed, so a match is
   INCONCLUSIVE; `-verify` refuses a kept write on the run's own entities unless `-resend-writes`.
6. **A pinned slice reproduces one incident**, on that run's data. Confirm a safe spot only from a
   closure slice.

For a minimal chain written by hand, verify it with `shrt chain slice <minimal> -step <t> -run
latest -keep writes -verify -write`; to keep a receipt against the source run, slice the source with
`-keep <ids of the minimal chain>` instead.
