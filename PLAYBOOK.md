# PLAYBOOK

Procedures. Keys are in `GRAMMAR.md`, symptoms in `PITFALLS.md`. Examples use the default envelope, `error.code` with `OK` for success; substitute yours (§3b).

## 1. Compose a chain when a contract exists

```bash
shrt contract show InvoiceService/PayInvoice                   # read before you generate
shrt contract plan InvoiceService/PayInvoice                   # preview
shrt contract plan InvoiceService/PayInvoice -write            # .shrt/chains/<name>.yaml
shrt contract plan InvoiceService/PayInvoice -write try.yaml   # .shrt/scratch/try.yaml, no gate runs it
```

`plan` walks `needs`, `before`, `from` and `same_as`, orders the calls and wires every `${...}`. It prints a summary, never the chain.

- **`order:`** is a claim about the flow. Check it: `plan` cannot judge business sense.
- **`fill:`** is test data you owe. Add the `value:` to the contract and re-plan with `-write -force`. Chain lint errors on each unfilled field.
- **`gap:`** is what the plan could not assert. State it in the contract and re-plan, or write that step by hand.
- **A missing step** is a missing contract edge. Fix the contract and re-plan.
- `-force` refuses a chain with a safe spot or a kept-red slice. Plan beside it with `-write <name>.yaml`.
- `-notes` says why each probe group is there; `-v` adds every step id.
- A whole flow at once: `shrt contract plan PayInvoice GetInvoice ListInvoices CancelInvoice@paid -write`. A node reached twice appears once.
- The same rpc twice, as a read before and after a write, needs an alias per instance: `aliases: {before: {note: ...}, after: {note: ...}}`, then `shrt contract plan GetAccount@before PayInvoice GetAccount@after -write`. Unordered targets keep the order you name them in.

## 2. Compose a chain with no contract yet

```bash
shrt catalog ls -filter <word>
shrt catalog describe acme.billing.invoice.v1.InvoiceService/CreateInvoice
shrt chain new -name invoice-happy-path InvoiceService/CreateInvoice InvoiceService/GetInvoice
```

- `chain new` wires an input to an earlier step's output when a contract says so, or when `contract init` would guess that producer. A producer missing from the list is named in a note.
- Each step asserts what a plan would, including levels and totals worked out from the quantities sent. Edit those and rework the numbers.
- An optional enum left at `*_UNSPECIFIED` is left out of the body.
- Then write the contract (§7), so the next chains compose themselves.

## 3. Fill the bodies

- `plan` calls a value unusable when the server will reject it: `""`, the zero enum member, a `"0"` int64, a zero number, blank list entries.
- Lint cannot tell a scaffold `"0"` from a deliberate zero; only the plan header reports it. Mark a deliberate zero with `value: "0"` in the contract.
- **A repeated message field gets two items. Keep two**, or a total or a check on the second item goes untested. The second copies the first with numbers raised by one and text prefixed `2-`. Give it its own data where it matters, and assert what depends on both.
- For write targets `plan` also sends one item and three.
- `shrt contract status -gaps` lists what no chain sends yet; `-v` explains each kind.

Habits that keep a chain re-runnable:

| need | write |
|---|---|
| an idempotency key | `${uuid}`, never a literal |
| a value two steps share | one reference to the first step's request (`${steps.a.request.x}`) or one var |
| a unique name per run | `inv-${vars.tag}-a`; an undeclared `tag` is fresh on every run |

`plan` leaves `tag` undeclared, so no run needs `-var`. A duplicate refusal: PITFALLS §15.

## 3b. Tell shrt how your backend answers

`conventions:` in `.shrt/config.yaml` has six optional keys. For a backend answering `{"status": {"code": "SUCCESS"}}` with batch items `{"results": [{"status": {...}}]}`:

```yaml
conventions:
  read_only_prefixes: [Fetch, Get, List, Query, Read]
  envelope_path: status.code
  envelope_ok: SUCCESS
  item_envelope_path: results[].status.code
  code_fields: [app_code, reason, error_code]
  validate_output: true
```

- `read_only_prefixes`: which rpc names are reads, matched at a word boundary.
- `envelope_path` and `envelope_ok`: where a response states its verdict, and the success value.
- `item_envelope_path`: a batch's per-item verdict. **Set it first on any backend with batch rpcs**: without it, a batch refusing every line passes. Check it once against a refused line.
- `code_fields`: detail fields `chain which -code` searches and a refusal can pin.
- `validate_output`: a response the descriptor rejects fails its step.

With the login credentials exported, `shrt init` logs in once and writes the envelope keys. It never rewrites an existing `conventions:` block; `init -v` prints the block to paste.

## 4. Assert something that can fail

A step whose only expectation is `error.code == OK` asserts only that the server did not crash. Say what the call did:

```yaml
expect:
  - path: error.code
    equals: OK
  - path: invoice.amount_minor
    equals: ${vars.amount}
  - path: account.balance_minor
    equals: ${steps.account_before.response.account.balance_minor}
```

- A `${...}` in `equals`, `not_equal`, `contains` or a numeric bound resolves at run time. A `${...}` in `path` is a lint error.
- It does no arithmetic. For *after == before + added*, choose the inputs, work the result out, and assert it as a literal or a var.

**`plan` probes by itself** what the contracts state: echoed values, states, list order and filters, uniqueness, limits, each failure's `when:`, other roles and missing tokens, `effects:` numbers, boundaries, batches and replays. Each probe group has fixtures of its own, so one defect fails one group. Never delete a probe that fails; pin a real defect (§9).

**A read that passed and found nothing.** `shrt chain hollow` names every read step in the run records that passed with an empty body, and exits 1 while any is unexplained. Assert what the read should find. Where empty is right, add `<chain> <step-id> <reason>` to `.shrt/hollow-allow.txt`. A pinned refusal, or `<list>.0 exists: false`, already says empty is right.

## 5. Probe one failure code

Failure coverage counts `(rpc, code)` pairs a run record observed; check one with `shrt chain which -rpc R -code C` (§10).

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

- The refusal line on the envelope, or on `transport.*`, makes lint treat the step as a probe. Without it lint demands every required field the probe leaves out.
- No `allow_fail`: it only tolerates a transport refusal on a step with no expectations.
- Assert `app_code` **and** `reason` for a named business failure. The envelope code alone is shared by many refusals.
- A shape or auth error has no `app_code` or `reason`. Read the step's `http_status`:
  - 200 with the verdict in the body: assert the envelope code, `error.message contains:`, and `error.details.0.reason exists: false`.
  - 4xx Connect error, no body: assert `transport.code` (`invalid_argument`) and `transport.message contains:`.
- Check the contract's `unreachable:` and `pending_deploy:` first.
- To see past the first red, `shrt run -keep-going <chain>`. A step reading a failed step's response is `skipped`.

## 6. Cross a principal boundary

`.shrt/config.yaml` declares the login once. The runner attaches the header and logs in again on expiry or an unauthenticated answer. Then a read is re-sent; a write refused after its token was accepted is not (exit 3).

- A second principal is a named profile under `auth.profiles`. Put `auth: <profile>` on the step.
- A login step is optional. It seeds a profile's token only when it sent that profile's `body` exactly.
- Never write an `Authorization` header. Lint rejects it and `shrt run` refuses the chain.
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

- A backend answering unauthenticated in a 200 body: assert the envelope path instead.
- A token refused well before its stated expiry: PITFALLS §33.
- A read answered with a server error is re-sent once and judged on the answer. Writes are never re-sent.

## 7. Author or extend a contract

```bash
shrt contract quality                  # what is missing: start here
shrt contract init <domain>            # scaffold; re-running keeps what you wrote
shrt contract lint
```

**Spec over handler.** State behaviour from the spec: README, API docs, tests. A contract copied from a buggy handler makes the planner assert the bug as correct. Use the handler for names, codes, shapes, `required` and `requires_role`. Report where the two disagree.

**Domains.** A domain is the package segment after the organisation root, versions dropped: `acme.billing.invoice.v1.InvoiceService` is `billing`. Privileged writes grouped under one segment (`acme.admin.*`) land in that overlay. A domain whose writes look too few for its reads: grep the catalog.

**Score.** `shrt contract quality` lists what each rpc lacks; `shrt contract status -v` explains the terms.

- Curate `-phase happy` first.
- Reads are exempt from the id and write-failure terms.
- Only `no_producer:`, `requires_role: [NONE]` and `required: [NONE]` exempt a term. `note:` spares nothing.
- `[NONE]` is a positive claim, never a way to lower the score.
- `shrt contract quality -gate -baseline <file>` fails when the score rises, or falls without the baseline being lowered.
- **Score 0 is necessary, never sufficient.** No term sees an incomplete `needs:`, and `before:` has none.

**Fill in this order**; each step pays for the next:

1. **`required`**, from the backend source, reads included. `[NONE]` if the server rejects nothing; `[UNKNOWN]` if the handler cannot be found.
2. **`fields.<f>.from` / `same_as`**: the ordering edges. Wrong ones show in the `order:` line.
3. **`needs` / `before`**: prerequisites no field references.
   - `needs:` names the write creating each row the handler reads whose identity comes from no request field. `from:` only wires the id you filter by.
   - `before:` is the rpc in another domain consuming what yours writes. It attaches the plain rpc; to order aliases, list them in the dependent rpc's `needs:`.
   - Then `shrt contract plan <read>` for every read. A one-step order has no producer; one creating the entity but never the row being read is the same bug.
   - A write that only takes the record to the state the rpc's `restore:` names makes `plan` also call the rpc from the state before it.
   - Name writes that live in another overlay; do not copy them.
4. **`failures`**: one per way the rpc refuses, `when:` written as a condition. `code` is optional for a shape error. `reason:` is the backend's string verbatim, or a label you choose for a shape or auth failure. Codes every rpc in the domain returns go in the top-level `failures:`.
5. **`effects`**: for a write that moves a number a record holds. `plan` asserts levels and totals from it; a `gap:` prints the one to add.
   - `{balance: {increase: amount}}`, or `decrease`; `lines.amount` moves once per line
   - `{balance: {decrease: lines.amount, of: id_invoice}}`: per line of the record `id_invoice` names
   - `{balance: {restore: POSTED}}`: gives back what was taken, from a POSTED record
   - `{balance: none}` leaves it alone; `{balance: zero}`: a create starts it at 0
   - `{total_minor: {sum: lines.qty, times: unit_price}}`
   - `{lines: per_item}`: each line applied or refused alone
6. **`exports`, `terminal`, `soft_signals`**: what the response is for. `terminal` records a dead end instead of deleting it.
7. **`source`**: the files you read, without line ranges.

- `summary` and `note` are prose for people. A note earns its place by saying what a caller could not guess, such as "decimal string, must be > 0".
- `@alias` is for two independent instances of one rpc (`CreateAccount@payer`, `CreateAccount@payee`). Declare it under `aliases:` with what makes them differ.
- Leave `status: draft`. Only a person sets `verified`.
- A codes-against-contract cross-check is a script you write (PITFALLS §24).

## 8. Run, hand off, verify

```bash
shrt chain lint <name>          # fix every error before sending traffic
shrt chain lint -strict <name>  # and every assertion-quality warning, before it gates
shrt run <name> -dry-run        # resolve and validate, send nothing
shrt run <name>
```

Read a step's `error` as a fixture problem first (PITFALLS §2).

**Run the chain twice, then propose:**

```
shrt confirm <name> -run <run-id> -note "what you inspected in the responses, and why it is right"
```

It writes `.shrt/safespots/pending/<name>.json` and prints a summary table.

**Present the proposal in the conversation, in the user's language, not as a file path:**

1. One line: chain, run id, steps passed, what the chain exercises.
2. The table: each step as a plain situation ("pay the same invoice again"), what was sent, what came back, whether that is right.
3. The one or two facts that carry the verdict, and anything you are unsure of.
4. What approving means: every response field not under a volatile pattern becomes the baseline. Add any warning the summary printed.
5. The question: approve or reject.

Then:

- Run `shrt confirm <name> -approve -by <user email>` only after a yes to this proposal. Ask for the email if unknown.
- `-reject` discards it; `-pending` lists proposals.
- Approve on the proposal's branch: `-approve` refuses a chain file unlike the one the run ran.
- A chain with a safe spot needs `-supersede`; the old one is archived.
- A suite: `shrt confirm -all -note "..."` proposes each chain whose latest run passed and whose safe spot is missing or differs. Present the table, `-reject` each one the user refuses, then `shrt confirm -all -approve -by <user email>`.
- A safe spot belongs to the chain name. A pure rename: `shrt confirm <new> -rename-from <old> -by <email>`.

**Two branches superseded the same safe spot.** Never hand-merge the JSON:

1. Take one side whole, the side whose chain file the merge keeps: `git checkout --ours .shrt/safespots/<chain>.json`, or `--theirs`.
2. Deploy the merged backend; run `shrt doctor -strict` and `shrt verify <chain>`.
3. If it drifts, run, propose with `-supersede`, and have a person approve.

**Suspect words** in gate, `verify` and `run` lines:

- `suspect write`: its answer changed, or a read shows it stored other than it answered. Fix the write.
- `suspect read`: the read itself answered otherwise. Fix the read.
- `unclear: write <a> or <b>`: several writes since the last matching read may have moved the field. Read the record between them.
- `unclear: write ... or the read`: the write answered as before and only the read moved. Read the field through the rpc `tell them apart:` names.
- `knock-on of <step>`: failed behind that step. Fix that one first.
- `as <role>`: no other role fails at that rpc and field.

A suspect is a lead. Test a suspect write with `shrt chain slice <chain> -without <step> -verify` (§11).

## 9. Refactor and test against a safe spot

```bash
shrt run <name>                    # before the change
shrt verify <name> -run <run-id>   # re-diff that record offline
# change the code, then the same two lines
```

What verify compares and masks: `GRAMMAR.md` §7. A clean verify covers only that chain's steps.

- A value that changes every run and is not id- or timestamp-shaped belongs in `volatile`. Ids do not: verify pairs them across runs.
- A list whose order varies between runs of one release: `unordered: [<list>]`.
- An intended change: `shrt confirm <name> -supersede -note "..."`, then a person approves.

**A chain kept red on purpose is never confirmed.** It asserts the correct behaviour and pins the known defect with `kept_red`: step, path, and the `got` when stable.

- `shrt run` of it goes past every failure. It exits 0 while it fails exactly as pinned.
- It exits 1 when anything else fails, a pinned step answers differently from the last run that failed as pinned, or the defect is gone.
- A slice whose pins held but which fails where its parent fails too reports the parent's regression, not a new defect.

**One real defect in a long chain: keep it red in a slice, confirm the rest.**

```bash
shrt chain pin billing      # each defect kept red in its own verified slice, billing without them
shrt run billing            # green: propose and approve it
```

- `chain pin` slices the first failing step, with the steps failing with it, cuts them from the chain, and re-runs until the chain passes.
- It notes `Kept red in <slice>` in the chain's description and the pins in the slice's.
- It stops when a slice does not reproduce, or a FINDING or intermittent failure explains the red.
- Once the defect is fixed, remove the slice and plan again.

**No safe spot yet: `shrt diff`.**

```bash
shrt diff <name>                             # latest run against the latest earlier non-replay
shrt diff <name> <run-a> <run-b>             # any two: ids, latest, latest~N
shrt diff <name> [<run>] -step <id>[,<id>]   # those steps' requests and responses, as recorded
```

It masks as verify does. It is not a verdict: if run A was wrong, "no differences" means B is wrong the same way.

## 10. Find the chain first

```bash
shrt chain which -code 1218
shrt chain which -rpc InvoiceService/PayInvoice
shrt chain which -rpc PayInvoice -code 1204 -json
```

- `-rpc`, `-code` or both; both intersect. `-code` matches the envelope code, a `code_fields` detail, `transport.code` or `transport.http_status`.
- Each chain heads with its reproduce command; replace `<fresh>`. A verdict appears only if the newest local run failed or never reached the step.
- Under `-rpc`, each write step says the state and item count it acted on.
- No match exits 1. Under `-code`, steps whose run carried the code unasserted are listed with a reproduce command.

## 11. A step failed: get the minimal reproduction

```bash
shrt chain slice billing -step pay_invoice_twice
shrt chain slice billing -step pay_invoice_twice -write probe   # .shrt/scratch/probe.yaml
shrt chain lint .shrt/scratch/probe.yaml
shrt chain slice billing -step pay_invoice_twice -write probe -verify -run latest
```

1. **Read the reason on every kept step.** It keeps the producers of what kept steps reference, contract prerequisites, earlier writes on the same entities, and earlier writes of a unique value the target sends again. A kept step that failed in the source run loses its expectations, unless it is a write failing on the target's field. That one stays, so run the slice with `-keep-going` to reach the target.
2. **Read `WARNING possible under-inclusion`** before trusting the size. A dropped write can be state the target needed. Put steps back with `-keep id[,id]`; `-keep writes,<id>` keeps every earlier write too.
3. **`-write [name]`, then lint it by path.** Without a path it lands in `.shrt/scratch/`, which no gate, lint or hollow sweep reads; run it by path. A value with a slash is a path. A slice never overwrites the chain it drops steps from.
4. **`-verify` makes the slice a receipt.** It runs the slice 3 times and compares the target's verdict with the source run's: envelope code, reason, transport refusal, and each expectation's want and got. Pass a fresh `-var name=<fresh>` when it asks.
5. **`-minimize`** first drops each step no kept step reads, when a run without it fails the same way.

| verdict (exit) | means | do |
|---|---|---|
| `reproduced` (0) | verdicts match in every counted run | nothing; `-write` records it |
| `NOT REPRODUCED` (1) | the verdict differs | run the `next:` line |
| `intermittent: reproduced k/n` (1) | flaky there | keeping more steps will not help |
| `DID NOT RUN` (3) | the target was never answered | read why, printed under it |
| `INCONCLUSIVE` (3) | a kept step that passed in the source fails here, or the source never evaluated the expectation | run the `next:` line, or `-keep writes` |

`-without <id> -verify` runs the chain without those steps:

- `STILL FAILS` (exit 1): every failing step fails exactly as before, so the left-out write is not their cause.
- `FAILS DIFFERENTLY` (exit 3): the write is involved. Run the `next:` slice instead of clearing it.
- Steps that pass without it may only need its state.

Until a verdict, a slice is a hypothesis. `-run latest` picks the run `shrt diff` compares, and refuses (exit 3) if that run left the target unevaluated.

**A chain written by hand is proven by running it, not slicing it:**

```bash
shrt run .shrt/scratch/repro.yaml -repeat 3
```

- It runs the chain 3 times unchanged, each run past a failed step, and compares each run with the first.
- `reproduced 3/3` (exit 0) is the receipt. `NOT REPRODUCED` (1) lists what differed; `passed 3/3` (1) means nothing failed; `DID NOT RUN` (3) a run got no answer.
- Under `-repeat`, exit 0 means the failure is there.
- Each run gets a fresh `tag` unless you pass `-var tag=`.
- `-keep-going=false` stops each run at its first failure.
