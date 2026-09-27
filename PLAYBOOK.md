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

`plan` walks `needs`, `before`, `from` and `same_as`, sorts them and wires every `${...}`. Preview
and `-write` print the same summary, never the chain:

```
order: CreateAccount -> CreateInvoice -> PayInvoice
12 steps: 2 setup, 1 target, 4 token/role, 5 unknown id
fill: create_account.name has no usable value: set fields.name.value in CreateAccount's contract
gap: step pay_invoice: PayInvoice says nothing of what it gives back from PAID (add effects: {balance: {restore: PAID}} if it does), so the other reads assert only that they answer
3 more note(s) on why each probe is there: shrt contract plan PayInvoice -notes
next: shrt contract plan PayInvoice -write
```

- **Check the `order:` line**: `plan` cannot tell whether the flow makes business sense.
- **`fill:` is test data you owe**: add the `value:` to the contract and re-plan with
  `-write -force`; chain lint ERRORs on each unfilled field.
- **`gap:` is what the plan could not assert**: say it in the contract and re-plan, or write
  that step by hand.
- **`-notes`** adds one line per probe group; `-notes -v` every note in full and every step id,
  `-v` alone the ids.
- **Compose a whole flow at once:** `shrt contract plan PayInvoice GetInvoice ListInvoices
  CancelInvoice@paid -write`. Each target may carry `@alias`; a node reached twice appears once.
- **A missing step is a missing contract edge**: fix the contract and re-plan.
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

`chain new` wires a step's input to an earlier step's output when a contract says so, or, for an
id no contract sets, when `contract init` would guess that producer; a producer not in the list is
named in a note. Reads of one rpc after two creates take them in turn, restarting after each write.
Then write the contract (§7) so the next chains compose themselves.

## 3. Fill the bodies

`plan` calls a value unusable when the server will reject it: `""`, the zero enum member
(`*_UNSPECIFIED`), a `"0"` int64 placeholder, a zero number, a repeated field of blank entries.
`chain lint` cannot tell a scaffold `"0"` from a deliberate zero; only the plan header reports it.
Say a zero is deliberate with `value: "0"` in the contract.

**A repeated message field gets two items; keep two**, or a total or a check on the second item
goes untested. The second is a copy with numbers raised and text prefixed `2-`; a reference to a
create step points at a second copy of it (`create_account_2`, with its own values and
preparation). Give it its own data where it matters and assert what depends on both. `plan`
also varies the count (one item and three) for write targets. `shrt contract status -gaps` lists fields no chain
sends with two items (`one item`), with two items on one resource only (`same resource`), or never
with one resource on two applied items (`no repeat`).

Habits that keep a chain re-runnable:

| need | write |
|---|---|
| an idempotency key | `${uuid}`, never a literal |
| a value two steps share | one reference to the first step's request (`${steps.a.request.x}`) or one var |
| a unique name per run | `inv-${vars.tag}-a`; an undeclared `tag` is fresh on every run |

`plan` leaves `tag` undeclared, so no run needs `-var`. A duplicate refusal (PITFALLS §23) is
`fixture reused` or `fixture collision` (exit 3, re-run with a fresh `-var`), or
`CHAIN DEFECT: the chain collides with itself` (exit 1: build the literal field from a var).

## 3b. Tell shrt how YOUR backend answers

`.shrt/config.yaml` `conventions:` has six optional keys. For a backend answering
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
- `item_envelope_path`: a batch's per-item verdict. **Set it first on any backend with batch
  rpcs**: without it a batch refusing every line passes. Check it once against a refused line; it
  applies only to responses whose message declares that list and field.
- `code_fields`: detail fields `chain which -code` searches and that can pin a refusal.
- `validate_output`: a response the descriptor rejects fails its step with `"drift": true`.

A path no response message declares fails `shrt run` before sending. With the login credentials
exported, `shrt init` logs in once and writes `envelope_path`, `envelope_ok` and an unambiguous
`item_envelope_path` (else exit 3 naming the variables; `-v` prints the block to paste); it never
rewrites an existing `conventions:` block. Key table: `GRAMMAR.md` §4.

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

A `${...}` in `equals` / `not_equal` / `contains` and the numeric bounds resolves at run time. It
does no arithmetic: for *after == before + added*, choose the inputs, work the result out, and
assert it as a literal or a var. A `${...}` in `path` is a lint error. Rules: `GRAMMAR.md` §1.

**What `plan` asserts and probes by itself**, from what the contracts state (read the notes; do
not delete a probe that fails, pin a real defect with `kept_red`, §9):

- the verdict, each numeric field a write sent echoed back, a created id, the state the contract
  names, and a timestamp range (`within: {of: "${nowunix}", by: 300}` for a stamp the call makes,
  an expiry against the stated lifetime, `equals` for a creation stamp read back);
- for a list: three fixtures, or four when more keys compete, whose creation order, varied request
  fields and item numbers (so a total over the items) disagree pairwise either way (a
  server-generated id is not controlled), positions by id when the contract states the order,
  otherwise membership (`includes:`) and a count; a fixture outside the filter's scope (another
  parent, a prefix not at the start), one list per reachable state for an enum filter, and a list
  of 12 fixtures (one past a stated "at most N") so a cap shows; a list the run does not scope is
  declared `volatile`;
- for a uniqueness failure: the same value again, the same value with every other field changed,
  and a case or whitespace variant when the contract says the comparison ignores case or trims
  (`unique: {case: ignore, trim: true}` states it as data);
- for a quantity or limit failure: a request just past the limit and one exactly at it, on the first
  and the last item, and one item twice whose quantities fit alone but not together, with reads
  before and after asserting a refused write changed nothing;
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

**A read that passed and found nothing**: `shrt chain hollow` names every read step in the run
records (runs and verify replays under `paths.chains`) that passed with an empty body; exit 1 while
any is unexplained. Assert what the read should find. A probe pinning a non-OK verdict, and
`<list>.0 exists: false`, already say empty is right; otherwise add `<chain> <step-id> <reason>` to
`.shrt/hollow-allow.txt`.

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

- The refusal line on the envelope (or `transport.*`) makes lint treat the step as a probe;
  without it lint demands every required field the probe omits (PITFALLS §18).
- No `allow_fail`: it only tolerates a transport refusal on a step with no expectations.
- Assert `app_code` **and** `reason` for a named business failure; the envelope code alone is
  shared by many refusals.
- **A shape or auth error has no `app_code` or `reason`.** Read the failing step's `http_status`:
  - 200 with the verdict in the body: assert the envelope code, `error.message contains: ...`, and
    `error.details.0.reason exists: false`.
  - 4xx Connect error: there is no body. Assert `transport.code equals: invalid_argument` and
    `transport.message contains: ...`.

Before writing the probe, check the contract's `unreachable:` and `pending_deploy:`.

To see behind the first red, `shrt run -keep-going <chain>`: every step runs; one reading a failed
step's response is `skipped`. `-v` prints every step.

## 6. Cross a principal boundary

`.shrt/config.yaml` declares the login once; the runner attaches the header and logs in again on
expiry or on an unauthenticated answer. A read, or a call whose cached token was unused, is then
re-sent (`auth_retry: resent`); a write refused with an already accepted token is not
(`auth_retry: not_resent`, exit 3). `auth_profile` records the profile a step ran as.

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

  `auth: invalid` sends a never-issued token in the covering profile's header. A backend reporting
  unauthenticated in a 200 body: assert the envelope path instead.

`run` and `verify`: a token refused after it was accepted earlier in the run points at a restart
(exit 3); the same refusal at the same step in the previous run, no restart shown, is a finding
(exit 1). `WARNING: token refused <N>s after issue` well before the stated expiry: settle it with a
chain of held reads (PITFALLS §49). A step with no answer (dropped connection, timeout) is exit 3,
a `FINDING` when the previous run did the same while later steps answered. A read with a server
error is re-sent once and judged on the answer; one answered then or elsewhere is
`FINDING: intermittent failure at <rpc>`. Writes are never re-sent.

## 7. Author or extend a contract, in payoff order

State behaviour from the spec (README, API docs, tests), not from what the handler does: a contract
copied from a buggy handler makes the planner assert the bug as correct. Use the handler for names,
codes, shapes and `required` (what it rejects up front), and report where it disagrees with the spec.

```bash
shrt contract quality                  # what is missing — start here
shrt contract init <domain>            # scaffold; re-running keeps what you wrote
shrt contract lint
```

**A domain is the package segment after the organisation root, versions dropped**:
`acme.billing.invoice.v1.InvoiceService` is `billing`; a reverse-DNS root (`com`, `org`) is dropped
too. Privileged writes grouped under one segment (`acme.admin.*`) land in that overlay, so when a
domain's writes look too few for its reads, grep the catalog.

**`contract status` counts entries; `contract quality` measures them.** CONTRACT fills as soon as
a scaffold lands; RPCS above it is an rpc no overlay covers. REACHED below it is a write missing a
`needs:` or `from:` (`status -gaps`: `no path to`).

The score (the footer of `shrt contract status -v` prints today's list):

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

Reads are exempt from the id and write-failure terms. Curate `-phase happy` first. An unfilled
`TODO` is not charged by itself, and `note:` spares no term: the only exemptions are
`no_producer:`, `requires_role: [NONE]` and `required: [NONE]`. Ratchet the score with
`shrt contract quality -gate -baseline <file>`: it fails when the score rises, or falls without the
baseline being lowered.

**Score 0 is necessary, never sufficient.** No term sees an incomplete `needs:`, and `before:`
has no term. Fill in this order; each step pays for the next:

1. **`required`**, from the backend source, reads included. `required: [NONE]` if the server
   rejects nothing; `required: [UNKNOWN]` if you could not find the handler.
2. **`fields.<f>.from` / `same_as`**, the ordering edges. Wrong ones show in the `# order:` line.
3. **`needs` / `before`**, the prerequisites nothing references. `needs:` names the write creating
   each row the handler reads whose identity comes from no request field (`from:` only wires the
   id you filter by); `before:`, the rpc in another domain consuming what yours writes. Then
   `shrt contract plan <read>` for every read: a one-step order has no producer, and one creating
   the entity but never the row being read is the same bug, quieter.
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

Step status: `ok`, `FAIL` (an expectation did not hold, or the proto rejects the request), `ERROR`
(never completed), `SKIP` (held back under `-keep-going`), `--` (dry run). `shrt run` refuses a
chain before sending anything on a missing var, an unset env var, a reference to an undeclared
field, or a body the proto rejects (`run -h` lists them). A step refused in-band fails unless an
expectation pins the verdict (PITFALLS §17). Read `error` as a fixture problem first (PITFALLS §4).

**Run the chain twice, then propose:**

```
shrt confirm <name> -run <run-id> -note "what you inspected in the responses, and why it is right"
```

This writes `.shrt/safespots/pending/<name>.json` and a report, and prints the summary table
(`also baselined:` lists unasserted values that become baseline too). A field that differs from the
previous passing run is warned about: declare it `volatile` unless the change is real.

**Present the proposal in the conversation, in the user's language; do not send a file path:**

1. one line: chain, run id, `n/n` steps passed, what the chain exercises;
2. the table, each step as a plain situation ("pay the same invoice again"), what was sent, what
   came back, whether that is right;
3. the one or two facts that carry the verdict, and anything you are unsure of;
4. what approving means: every response field not under a volatile pattern becomes the baseline,
   and any warning the summary printed;
5. the question: approve or reject.

Run `shrt confirm <name> -approve -by <user email>` only after the user answers yes to THIS
proposal; ask for the email if unknown. `-reject` discards it; `-pending` lists proposals.
`confirm` refuses a hand-edited run record and, on `-approve`, a chain file unlike the one the run
ran: approve on the proposal's branch. A chain with a safe spot needs `-supersede`; the old one is
archived under `.shrt/safespots/archive/<chain>/`.

For a suite, `shrt confirm -all -note "..."` proposes every chain whose latest run passed and whose
safe spot is missing or differs, one row each (full summaries in `.shrt/safespots/pending/`).
Present the table, `shrt confirm <chain> -reject` each one the user refuses, and only after a yes
to the rest, `shrt confirm -all -approve -by <user email>`.

A safe spot belongs to the chain name. For a pure rename, `shrt confirm <new> -rename-from <old>
-by <email>` carries it across; any other difference is refused.

**Two branches superseded the same safe spot.** Never hand-merge the JSON:

1. Take one side whole: `git checkout --ours .shrt/safespots/<chain>.json` (or `--theirs`), the
   side whose chain file the merge keeps.
2. Deploy the merged backend and run `shrt doctor -strict` and `shrt verify <chain>`.
3. If it drifts, run, propose with `-supersede` and have a person approve.

**How the gate names a suspect**, one line per distinct change, under the rpc it blames:

- `suspect the read`: a server error; a failed auth probe; refused, stopped refusing, or another
  set of items with no refused or item-making write before it; only list order or an item's
  position (by id) changed; it moved while its write answered as before; or it contradicts two
  different writes. Fix the read.
- `suspect the write`: its answer changed, or a read of the record contradicts it (both values
  shown); `... it answered other than it stored`: it answers one value and persists another. Fix it.
- `suspect the write or the read`: the write answered as before, only the read moved (a write
  storing other than it answers looks the same). Filed under both, several writes nearest first;
  check what the write persisted.
- `its contract moves <field>`: of several such writes, the one whose `effects:` move it. Start there.
- `<rpc> answers <path> differently as <p> than as <q>`: filed under the rpc as that profile; a
  role-scoped view leaks or hides the field.
- `masked by` / `moved with <rpc> <path>`: a pin or drift that follows a change reported
  elsewhere. Fix that one first. `a knock-on of <suspect>`: recomputed from an earlier change.
- `pins held, new change`: a new defect beside the pinned ones; do not re-pin, run its `shrt diff`.

A change goes to an earlier step whose answer for that field of that record, or a total it
recomputes from, changed; a newly refused or changed write to the first earlier write on its
records whose answer or verdict changed, or read changed in a field both `effects:` move. A read
observes the nearest earlier write on its record, skipping refused repeats, idempotent replays
and, against a reference, a write refused as before or lacking the field whose `effects:` do not
move it. A list item's id links it to the writes naming it; steps unevaluated behind a failure
fold under it.

## 9. Refactor and test against a safe spot

`shrt run` asks whether the assertions hold; `shrt verify` asks whether every response field still
matches what a person approved.

```bash
shrt run <name>                                  # before the change
shrt verify <name> -run <run-id>                 # re-diff that record, offline
# change the code, then the same two lines
```

Reading the report:

- Change kinds: `GRAMMAR.md` §7. A clean verify covers only that chain's steps.
- `targets differ:` first means the replay ran against another target.
- Every step runs as under `-keep-going`; one held back behind a failure is `not_reached`.
- It masks `volatile` paths, id- and timestamp-shaped values, and values that only echo a fixture
  name or a `${uuid}` (counted under `-v`, listed by `-masked`). A lost volatile value (null,
  empty, gone) is still reported, as is a volatile list's length when its expectation failed.
  Anything else that changes every run belongs in `volatile`; ids do not (verify pairs them across
  runs, and a volatile id hides a stale one).
- It compares what each step SENT first: a changed request or chain that explains every response
  change is `drift with different input` / `drift after a chain change`, otherwise `regression`.
- A list whose order varies between runs of one release: declare `unordered: [<list>]`; otherwise a
  reorder fails with `order changed`, a regression when the order was promised.
- A field the descriptor gained and the backend does not send yet is not a change.
- `LATENCY:` (at least 250ms and 3x slower than the safe spot's run, a read re-sent to confirm)
  fails only with `latency: {fail: true}` (`GRAMMAR.md` §4).
- An intended change: `shrt confirm <name> -supersede -note "..."`, then a person approves.

**A chain kept red on purpose is never confirmed.** It asserts the correct behaviour and pins the
known defect with `kept_red` (step, path, and the `got` when stable). `shrt run` of it goes past
every failure, exits 0 while it fails exactly as pinned, and 1 when anything else fails, a pinned
step returns something else than in the last run that failed as pinned, or the defect is gone.

**One real defect in a long chain: keep it red in a slice, confirm the rest:**

```bash
shrt chain pin billing               # each defect kept red in its own verified slice, billing without them
shrt run billing                     # green: propose and approve it
```

`chain pin` runs `-keep-going` first if the latest run did not reach every step, then repeatedly
slices the first failing step (with the failing steps that read it, fail the same call the same
way, or are failing reads with no write between), cuts them from the chain and re-runs until it
passes. It stops when a slice does not reproduce or a FINDING or intermittent failure explains the
red.
By hand: `chain slice <c> -step <id> -kept-red=<id,...> -verify -write`, then `chain slice <c>
-without failed -write .shrt/chains/<c>.yaml`. Remove the red slice and plan again once the defect
is fixed.

**No safe spot yet: `shrt diff`.**

```bash
shrt diff <name>                                 # latest run vs the latest earlier non-replay
shrt diff <name> <run-a> <run-b>                 # any two; ids, latest, latest~N
shrt diff <name> [<run>] -step <id>              # one step's request and response, as recorded
```

It masks as verify does. It compares two runs, it is not a verdict: if run A was wrong, "no
differences" means B is wrong the same way.

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
2. **`asserted` is a chain's claim; `OBSERVED` means a local run record reached the step**, with
   what the newest such run got, even against the assertion. Run records are machine-local.
3. **Paste the `reproduce:` line**, replacing `<fresh>` in `-var <name>=<fresh>`.
4. **No match exits 1.** Under `-code`, steps whose run record carried an unasserted code are listed
   with a reproduce command instead.

## 11. A step failed: get the minimal reproduction

```bash
shrt chain slice billing -step pay_invoice_twice
shrt chain slice billing -step pay_invoice_twice -write probe
shrt chain lint probe
shrt chain slice billing -step pay_invoice_twice -write probe -verify -run latest
```

1. **Read the reason on every kept step.** It keeps the producers of what kept steps reference,
   the contracts' prerequisites, earlier writes on an entity the kept steps use, and earlier writes
   sending a unique value (id, unique or idempotency field; any field without a contract) the target
   sends again, ignoring case.
2. **Read `unmet prerequisites` and `WARNING possible under-inclusion`** before trusting the size:
   a dropped earlier write can be state the target needed, and the slice can go green without it.
3. **`-write [name]`, then `chain lint` it.** The file lands beside the source chain, where gates
   run it; a value with a slash is a path (`-write .shrt/scratch/<name>.yaml`, run by that path).
   `-force` replaces another file; a slice of the same chain and step is replaced in place.
4. **`-verify -run <id|latest>` turns the slice into a receipt.** It runs the slice (3 times by
   default, `-repeat N`) and compares the target step's verdict with the source run's: envelope
   code, reason and app code, transport refusal, and each expectation's pass, want and got; when
   those match and the source run drifted at the target against its safe spot, the slice must
   drift the same paths. It
   writes the source run's vars into the slice, except fresh vars a kept write interpolates, which
   you must pass (`-var name=<fresh>`). Outcomes:
   - `reproduced` (0): verdicts match, no dropped write touched a kept entity (`-write` records it
     in the description).
   - `NOT REPRODUCED` (1): the verdict differs; run the `next:` line.
   - `DID NOT RUN` (2): the target was never answered, or refused before sending.
   - `INCONCLUSIVE` (3): verdicts match but dropped writes act on kept entities, or another target;
     run the `next:` line, or `-keep writes` for every earlier write.
   - `intermittent: reproduced k/N` (4): flaky there; keeping more steps will not help.
   Until a verdict, a slice is a hypothesis. `-run latest` picks the run `shrt diff`
   compares: the newest record, but a `shrt run` over a verify replay recorded right after it,
   unless only the replay failed the step; it refuses (3) if that run left the target unevaluated.
5. **`-mode pin -run <id>`** drops producers whose only contribution was a value and pins their
   values into `vars:`. It does not re-send writes the source run performed, so a match is
   INCONCLUSIVE; `-verify` refuses a kept write on the run's own entities unless `-resend-writes`.
6. **A pinned slice reproduces one incident**, on that run's data. Confirm a safe spot only from a
   closure slice.

For a minimal chain written by hand, verify it with `shrt chain slice <minimal> -step <t> -run
latest -keep writes -verify -write`; to keep a receipt against the source run, slice the source with
`-keep <ids of the minimal chain>` instead.
Suspect write: `chain slice <c> -without <id> -verify` names the failing steps that need it and those still red; answering as the safe spot did, it is a precondition or stores other than it answers, so look first at the first step that needs it.
