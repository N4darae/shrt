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
emits a chain with every `${...}` already wired. A list target (`ListOrders`) whose contract names
nothing that creates the items it lists gets the one write with a contract whose response carries
such an item (`CreateOrder`) added before it, with a note saying to declare `needs:`, since a list of
nothing passes whatever the backend lists; when no such write is known, the note says the list comes
back empty, and `contract lint` warns (field `needs`) on a list rpc with no `needs:`. Ahead of that chain it prints a header you must
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

The count on that last line is every required field with no usable value, including a required
field on either side of a `same_as:` pair that sends the shared `${vars.<producer>_<field>}` while
`vars:` declares it empty (the producer's contract gave it no `value:`). That pair gets one
`must send the same value` note naming the var rather than a `fill it` note per side, but each
required side is counted, because `chain lint` errors on each until the var has a value.

- **The `# order:` line is the claim to check.** `plan` wires what the contracts say to wire; it
  cannot tell that a movement in one asset should not discharge an obligation in another. A plan
  that lints clean and runs green can still reproduce a meaningless state. Grep it as
  `'^# order:'` — the `# ` is part of the line, and so is the single space after `note:`. That
  is the printed form; with `-write` the same lines go to the terminal as `wrote <path>`, then
  `  order: ...` and `  note: ...`, indented two spaces with no `#`, so grep `'^  order:'` there.
- **`plan` emits ten kinds of `# note:` and only one of them is test data you owe** — the
  `is required and has no usable value — fill it` kind. One more is an assertion you owe:
  `asserts only the verdict ... declares what its response carries (...)` names the facts to assert,
  and `chain lint -strict` fails the step until you do (§4). `plan` already asserts a floor on every
  step it can (an id read back, the state the contract names, the created id, each batch item, and
  on a read the creation stamp equal to the one its creator returned), and says which in one note, so
  a fresh plan passes `chain lint -strict`; that note lists what it did, not what you owe, but the
  floor is not the test: add the values your data should produce. Read the others rather than skimming past:
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
- If you find yourself adding a step by hand for an rpc the plan left out, the contract is missing
  an edge — fix the contract, then re-plan. That is the difference between composing one chain and
  making every future chain compose itself.
- **The exception is the same rpc twice**, typically a read before and after a write, so the chain
  can compare the two. `plan` puts each node in once, so a second plain `GetProduct` target is
  merged into the first (`plan GetProduct AddStock GetProduct` plans one plain read, and a `note:`
  line says so and names the aliases to use); no edge is missing.
  Declare one alias per instance on the read (`aliases: {before: {note: ...}, after: {note: ...}}`),
  then name them around the write. Each aliased step's `description:` is the rpc's summary followed
  by the alias `note:`, so the two reads say which is which:
  `shrt contract plan GetProduct@before AddStock GetProduct@after -write`. Targets that no edge
  orders keep the order you name them in, so read the printed `order:` line; to make the contract
  itself pin the read ahead of the write, list `GetProduct@before` in the write's `needs:`, and name
  the after-read after the write (`shrt contract plan AddStock GetProduct@after`). That `needs:` is
  on the write, so it pulls `GetProduct@before` into **every** plan containing `AddStock`, including
  one for an rpc that only needs stock to exist (a `CreateOrder` whose `needs:` names `AddStock`
  plans `CreateProduct -> GetProduct@before -> AddStock -> ...`, a read nothing compares against). Put the before-read in `needs:` only when every chain through the write
  should compare against it; otherwise leave it out and name `GetProduct@before` as a target.

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

**A repeated message field gets two items, and you should keep two.** One order line exercises
the per-line code once and leaves everything that combines lines untested: a total summed over
them, a check that only fires on the second, a batch that stops after the first. A regression
there passes a chain that sends one line, and so passes the gate. So `plan` scaffolds every
repeated message field in a request with two items: the second is a copy of the first with its
numbers raised by one and its free-text strings prefixed with `2-`, while ids, keys, enums, zeros
and `${...}` references are copied unchanged, except a reference to a step of the chain that
creates something: the second item reads a second copy of that step instead (`create_product_2`,
its var-built values suffixed `-2` and its numbers raised by one, so a different sku and price),
and each write step that prepares the first (`add_stock`) gets a copy for the second
(`add_stock_2`, its var-built values suffixed and its numbers raised by one, so `qty: "11"` next to
`qty: "10"`), whether that step runs before the order or is pulled in after it by another target's
`needs:` (`ConfirmOrder` needing `AddStock`); a step you named as a target is not copied. Both lines of a planned `CreateOrder` then point at two products with two prices,
so a backend that prices every line at the first line's product changes the total, and a safe spot
catches it even before you assert it. A note names each such field and each added step. Give the
second item its own test data where it matters, and assert what depends on both
(`order.total_minor` for the pair, `order.lines.1.qty`). `chain new` does the same when the rpc list
names one producer; list the producer twice and each item reads its own.
The count is varied too, since a backend can break at a number of items two never reaches: a
write target that reads a fixture sending a repeated message field with two items (a `CancelOrder`
reading `create_order`), or sends one itself, is repeated on a copy of the fixture with one item
and one with three (`create_order_1_lines` then `cancel_order_1_lines`, `create_order_3_lines` then
`cancel_order_3_lines`), expecting what the target expects. The third item reads a third resource
where the second reads its own (`create_product_3`, price 12345, prepared by `add_stock_3`).
`shrt contract status -gaps` lists, as `one item`, each repeated request field that some chain
sends but no chain sends with two or more items; as `same resource`, one that chains send with two
or more items only when all of them point at the same resource (the same `${step...}` reference or
literal id); and, as `no chain`, each unary rpc no chain calls
at all, with the repeated message fields it takes, since those are never sent even once.

Three habits that keep a chain re-runnable:

| need | write |
|---|---|
| an idempotency key | `${uuid}` — never a literal, or the second run collides with the first |
| a value two steps must share | one chain var, referenced twice — never two literals that a later edit can desynchronise |
| a name or code that must be fresh per run | `${vars.tag}` interpolated, and pass `-var tag=...` at run time |

`shrt chain lint` warns `literal-idempotency-key` on a body field or header whose name says
idempotency (`idempotency_key`, `Idempotency-Key`, `dedup_id`) holding a literal. A backend that
honours the key answers a repeat with the first run's result, so `verify` does not call what follows a
regression: when the step sent the key the confirmed run sent and answered with the confirmed run's
id (`order.id_order`), it prints `CHAIN DEFECT: ... an idempotent replay` and exits 1 for a literal,
or `fixture reused: ... the confirmed run's idempotency key` with a fresh `-var` hint and exits 3 for
a key built from a var (`${vars.ik}` left at the confirmed value). The same holds for a key any other
recorded run sent, of this chain or another: `shrt run idem -var tag=G1` then `shrt verify idem
-var tag=G1` names that run (`the idempotency key the recorded run ... of this chain already sent`)
and exits 3, since the backend answered with what that run created. When a step before the replay
already drifted, that drift stays the verdict (`regression: N change(s)`, exit 1), and a
`note: step "order" sent idempotency_key=... an idempotent replay` line names the replay, so the
changes at and after it are not read as more of the regression.

The `-var` habit is what lets one chain run twice on the same box without tripping a uniqueness
constraint, and it is why a sweep over the corpus generates a random tag per chain. `shrt run`
refuses a `-var` the chain never reads (a mistyped name would otherwise silently collapse every
run onto one key), so a sweep passes `-var tag=...` only to the chains that read `${vars.tag}`:
check with `grep -l 'vars.tag' .shrt/chains/*.yaml`, or read the refusal, which lists the vars the
chain does read. `shrt contract plan` declares a var that the contract's `value:` entries
interpolate (`sku-${vars.tag}`) under `vars:`, with the chain's name as its value, so a planned
chain lints without a warning and its first run needs no `-var`. The second run sends the same
values and trips the same uniqueness constraint, so keep passing a fresh `-var tag=...`. `shrt
verify` recognises that case: when the first failing step is refused as a uniqueness conflict
(`already exists`, `SkuTaken`, `duplicate`) on a field built from a var whose value a recorded run
of the chain already used (a run counts if that step was answered and not refused there, so it
created the record, or if it was sent and got no answer, a dropped connection or a timeout, so
whether it took effect is unknown, which the line then says), it prints `fixture reused: ...` and, unless a step before it drifted,
exits 3 with `could not verify <chain>: fixture reused`, not `regression`. It is also `fixture
reused`, naming the chain and run, when a recorded run of ANOTHER chain of this repo sent the same
value in a step that created it (`happy` and `cust2` both creating `c-${vars.tag}@...` under one
tag). When no recorded run of any chain used that value, the other record came from somewhere else
(another client, a shared backend): it prints `fixture collision: ...` and exits 3 the same
way, with the same fresh `-var` hint. When the previous run of the chain that sent that step was
refused there the same way with a different value that no recorded run had created, two fresh
values in a row collided: that points at the backend unless another client uses the same values
(two pipelines deriving the tag from one commit SHA do), which shrt cannot tell from a var, so it
stays `fixture collision`, exit 3, and says so. Only when the conflicting field is built from
`${uuid}` or a clock value (`name: widget ${vars.tag} ${uuid}`), a value unique to its run, does a
repeat print `FINDING: ...` naming both values and exit 1, a finding about the backend. The same
way means the same refusal codes over the same field: a previous run refused `SkuTaken` on `sku`
is no repeat of a refusal `NameTaken` on `name`, which stays the first of its kind, exit 3. `shrt run` prints that line and hint too when its first
failing step is refused that way. The var named is the one the conflicting field is built from:
the field whose sent value the refusal quotes, or else whose name it spells (`EmailTaken` names
`email`); when it names none, every fixture field of the step counts. When the field the refusal
quotes or names is a literal (`sku: fixed-sku-1`, built from no var), no `-var` can help: the chain
collides with itself, since every run after the first sends the same value. `verify` and `run` then
say `the chain collides with itself: ... sku is the literal fixed-sku-1 ... build it from a var, e.g.
sku: sku-${vars.tag}` and exit 1, a defect in the chain rather than could-not-verify. The same
holds when the refusal quotes and names no field (`duplicate record`) while every field of the
step built from a reference is built from `${uuid}` or a clock value: those are unique to their
run and cannot be what collided, so the literal field (`sku: fixed-sku-ao3`) is blamed, never the
`${uuid}` one, and a repeat is not a `FINDING`. A literal as short as one character counts when
the refusal quotes it as a word of its own (`email @ is already registered` for `email: '@'`) or
spells the field's name. A literal is not blamed when two recorded runs of the chain in a row sent
it at that step and were both answered without a refusal: the second was accepted after the record
already existed, so it is not unique and cannot be what collides. Two accepting runs with a refusal
between them are not that: each accepted the value on a backend that did not hold it yet (the
first run, the first run after a reset), and the chain still collides with itself. With
no literal left to blame and a refusal that names no field, verify gives its plain verdict against
the safe spot (`regression: N change(s)`). The same holds when the refusal quotes or names that
accepted literal (`name Bob Literal already exists` for `name: Bob Literal`): the backend now refuses
what it accepted, a regression, exit 1, and the var-built field next to it (`email:
l-${vars.tag}@example.test`) is not blamed, so it is no `fixture collision` either. When an earlier step of the same run, calling the same
rpc, sent the identical value on the conflicting field and was accepted (two `CreateProduct` steps
both sending `sku: sku-${vars.tag}`), `run` and `verify` say `the chain collides with itself within
one run: ... the value step "create_product" of this same run sent there` and exit 1: every run
collides with itself whatever `-var` is given, so it is never a fixture collision. Not when the
repeat is deliberate or proven: a value the later step reads from the earlier one's request
(`idempotency_key: ${steps.order1.request.idempotency_key}`, an idempotent resend), or a repeat the
safe spot's run or another recorded run sent at those two steps and had accepted, is what the
backend used to accept, so its refusal now is `regression: N change(s)`, exit 1. A var that
is a field's whole value (`${vars.key}`) has no safe default and stays undeclared.

## 3b. Tell shrt how YOUR backend answers

`.shrt/config.yaml` carries a `conventions:` block. Six keys, all optional, and the defaults
describe a Connect-style backend that reports its verdict at `error.code` with `OK` meaning success.
The block below is NOT the defaults: it is an example for a backend that answers
`{"status": {"code": "SUCCESS"}}`, and whose batch rpcs answer
`{"status": {...}, "results": [{"status": {"code": "SUCCESS"}, ...}]}`, with every key set to show
its shape. It carries no comment lines, so it pastes into a repo whose hook rejects them:

```yaml
conventions:
  read_only_prefixes: [Fetch, Get, List, Query, Read]
  envelope_path: status.code
  envelope_ok: SUCCESS
  item_envelope_path: results[].status.code
  code_fields: [app_code, reason, error_code]
  validate_output: true
```

Key by key: `read_only_prefixes` says which rpc names are reads, a prefix counting only at a word
boundary (`Get` covers `GetProduct`, not `Getaway`; `Show` not `ShowcaseProduct`); `envelope_path` is where a
response states its verdict, and `envelope_ok` the value there meaning success;
`item_envelope_path` is a BATCH rpc's per-item verdict, a path that must exist in your response
messages (`results[].error.code` on a backend like this one makes `shrt run` refuse every chain,
because no response declares it); `code_fields` are the detail fields
`chain which -code` searches; and with `validate_output: true` a response the descriptor rejects
FAILS its step. `verify` then leaves that step and the later steps reading from it unjudged (exit 3),
but still judges a later step that reads nothing from it: a changed value there is a `regression`.
Once a write after it was not sent (skipped because it read the drifted step), every later step is
unjudged too: its difference may be the missing write's side effect, so verify exits 3 and names that write.

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
the call *did*. `shrt contract plan` does not invent that assertion: it cannot know what the call
should have produced, so each planned step asserts the verdict, plus two checks it can derive: a
range on each timestamp-like response field (an expiry within 5s of `${nowunix+<lifetime>}` when
the contract states the lifetime, else `gte: ${nowunix}`; a `created_*`/`updated_*` stamp the call
makes within 300s of `${nowunix}`; a creation stamp a later step reads back, such as `GetProduct`'s
`product.created_at`, equal to the one the creating step received,
`equals: ${create_product.product.created_at}`, since the read did not stamp it), and, for a list rpc it is asked to plan, the order of the list. For the list
it creates THREE items whose candidate sort keys disagree: the prefix field the list filters on
(`sku`: base, base-b, base-a), every other string or number field of the create (`name` B, A, C;
`price_minor` 750, 250, 500), and creation order, each put the three in a different order, so an
order assertion can only pass on the key the backend really sorts by. Fixtures whose names sort
like their skus pass a backend sorting by name. When the list's contract states an order
(`sorted by sku`, `newest first` in its summary or in `exports:` for the list), the plan asserts
each position by id; when it states none, it asserts only the count and says how to have the
order asserted. `chain new` does the same for two or more creates feeding a list, adding a third.
`chain lint` names a step that asserts positions of a list whose items sort alike under two or more
keys, creation order included (`indistinct-order`, a hint). For a create whose contract declares a
uniqueness refusal (a reason such as `EmailTaken`, `SkuTaken`, `…Exists`, `…AlreadyExists`,
`Duplicate…`, or a `when:` saying unique or duplicate), the plan adds a step right after it sending
the same value again (`${steps.<id>.request.<field>}`), and a second (`<id>_same_<field>_other_fields`) sending it with
every other literal field changed (`Widget …-other`, a price of 2n+1), expecting the verdict not to be the ok value,
the failure's code and reason on the response's code fields (`conventions.code_fields`, e.g.
`status.details.0.app_code` and `.reason`), and no created object. The field is the failure's
`field:`, else the one the reason names, else the one field whose note says unique. When the
contract says the comparison ignores case (`ignoring case`, `case-insensitive` in the failure's
`when:` or the field's note), it adds the value with the case of every letter outside its
references swapped (`CUST-${vars.tag}@EXAMPLE.TEST`), since a backend comparing case-sensitively
passes an exact duplicate; a `${uuid}` in that field becomes `${vars.tag}` so both steps send the
same value, and each run then needs `-var tag=<fresh>`. When it says the value is trimmed of
surrounding whitespace, it adds the value padded with spaces too. A negated phrase counts against:
`no trimming`, `not trimmed`, `without trimming`, `case-sensitive`, `not ignoring case` add no
variant, and a mention of whitespace that is not about trimming (`whitespace only is
invalid_argument`) adds none either. Say it as data to leave no doubt: `unique: {case: ignore,
trim: true}` (or `case: exact`, `trim: false`) on the failure wins over the prose. When the contract
says nothing about case, a note says how to ask for the variant rather than guessing.

A refused write must change nothing, and only a read proves it. For a target whose contract declares
a shortage refusal (`InsufficientStock`, a `when:` saying more than, exceeds, not enough), the plan
asks for 100000 of the first quantity field (`qty`, `quantity`, `count`, `amount`) it finds, in the
step's own body or in a copy of the step it reads (`create_order_for_insufficient_stock`, the order
`confirm_order_insufficient_stock` then confirms), expecting the refusal. It does so once with the
shortage on the first item and once on the last (`..._last_item`): a backend that checks only the
first line confirms the second. Around each refused step it reads every entity the step touches,
directly or through the step it reads, with the read rpc whose contract takes that entity's id
(`get_product_before_…`, `get_product_after_…`), and the after-read asserts each numeric and enum
field equal to the before-read. A backend that refuses but still takes the first line's stock fails
there. Pin a shortage your backend really mishandles with `kept_red`; do not delete the probe.

Each write probe group runs on fixtures of its own: the shortage probes, the token probes and the
item-count probes each get copies of the steps that created and prepared the main path's fixtures,
named `<fixture>_for_<group>` (`create_product_for_shortage`, `add_stock_for_shortage`,
`create_order_for_denied`), with unique fields changed and numbers kept. A defect one probe exposes,
such as stock drained by a refused confirm that went through, then fails that probe and its reads
only, not every later probe that orders the same product.

A failure whose `when:` names a state of the entity the target acts on (`the order is already
CONFIRMED`, `the order is CANCELLED`) is probed in that state: the plan creates a fresh entity, moves
it there with the write whose summary or export says it moves entities to that state
(`confirm_order_to_confirmed_for_confirm_order`), calls the target on it expecting exactly that code
(`confirm_order_when_confirmed`, 1303), and reads it before and after. A not-found failure (`no order
has this id`, `a line names an unknown product`) matched to a `from:` field gets a call with an id
nothing created (`confirm_order_unknown_id_order`, `create_order_unknown_id_product` on the last
line), expecting exactly that code. A state no write reaches gets a note instead of a probe.

When the config declares auth, the plan also probes who may call. For a target whose contract names
`requires_role: [ADMIN]`, each auth profile whose name is not a required role (`clerk`) gets
`<step>_as_clerk`, the same call under `auth: clerk`, expecting the failure the contract declares
for a caller without the role (a reason such as `PermissionDenied`, or a `when:` naming the role);
the plan assumes such a profile lacks the role, so name profiles after their role. The plan's first
target also gets `<step>_without_token` (`skip_auth: true`) and `<step>_with_bad_token` (`auth:
invalid`), expecting the domain's `connect_code: unauthenticated` failure as `transport.code` (one
pair per plan, not per rpc). A refusal every rpc of every domain shares, as `unauthenticated` usually
is, is declared once: put it in one overlay's domain-level `failures:` with `scope: all` (in
`auth.yaml`, say) rather than copying the block into each overlay; without `scope: all` a
domain-level failure reaches only its own overlay's rpcs. For a write, all of these sit between reads of what it touches
(`get_product_before_add_stock_denied` / `…_after_…`), so a denied call that still wrote fails.
`shrt contract status -gaps` lists the role-gated rpcs no chain calls as a lower profile (`no role
probe`) and the chained rpcs no chain calls without a token (`no token`).

An rpc every role may call must behave the same for each of them, and only calling it as each proves
it. For a target whose contract says `requires_role: [NONE]` (or names none), each other auth profile
gets a copy. A read is repeated right after itself as that profile (`get_product_as_clerk`) and
asserts every non-repeated field of the answer equal to the first read's (`product.price_minor
equals ${get_product.product.price_minor}`), so a backend that zeroes the price for a clerk fails. A
field that really differs by role is left out when the contract documents it under `terminal:` or
`soft_signals:` with text naming a role, caller or profile (`cost_minor: shown to ADMIN only`); a
repeated field is not compared item by item, and a note says so. A write runs as that profile on
fixtures of its own, copied from the steps that created and prepared its fixtures with unique
fields changed and every number kept (`create_product_for_clerk`, `add_stock_for_clerk` at the same
`qty`, `create_order_for_clerk`, then `confirm_order_as_clerk`); the plan reads what the default
profile's write changed right after it (`get_product_after_confirm_order`) and what the other
profile's changed after that one (`get_product_after_confirm_order_as_clerk`), asserting the same
numbers and states. Timestamps are not compared. A confirm that takes stock twice for a clerk fails.

Numbers in planned fixtures differ by magnitude, because a bug in arithmetic shows at a size the
first fixture never reaches (a price stored as `price - price/1000` is right for 250 and wrong for
1250). A second producer takes the first value plus 1000 (`create_product_2`, price 1250), three list
fixtures take 250, 1250 and 12345, and a quantity (`qty`, `quantity`, `count`, `amount`) moves by one
per fixture instead: up when it is a sort key, down where it can inside repeated items (`lines.0.qty`
3, 2, 1), so it stays within the stock the
plan adds. Every planned write asserts that each numeric field it sent comes back in its response's
object unchanged (`product.price_minor equals ${steps.create_product.request.price_minor}`). A target
also gets `<step>_<field>_large` (12345; never for a quantity, which runs into stock rules) and, when
a failure's `when:` or the field's note states a minimum (`qty is zero or negative`, `must be greater
than zero`, `at least 5`), `<step>_<field>_min` at it, expected accepted, and
`<step>_<field>_below_min` one below, expected refused with that failure, between reads proving the
refused write changed nothing.

Text is tested at lengths and in characters the fixtures never use, since a column that truncates
at 20 characters or mangles UTF-8 passes every short ASCII name. For a write target whose response
carries back string fields it sends (not ids, keys or enums), the plan adds `<step>_long_text`,
each such field grown by 65 characters (an email before its `@`, so it stays an email), and, for
free-text fields (`name`, `title`, `description`, `note`, `comment`, `label`, ...),
`<step>_unicode_text` with multi-byte characters (`Ünïcødé-日本語-✓`). Each asserts the response
echoes every field exactly (`customer.name equals ${steps.create_customer_long_text.request.name}`)
and, when a read rpc takes the created id, a read after it asserts the stored text the same way
(`get_customer_after_create_customer_long_text`). When a field's note or a failure's `when:` states
a maximum (`at most 40 characters`, `longer than 40 characters`), that field is left out of the long
probe and gets `<step>_<field>_at_max` (exactly that many characters, accepted and stored; a unique
field is built from `${uuid}` so its length is known) and `<step>_<field>_over_max` (one more,
refused with the failure whose field or `when:` names the length, between reads proving nothing
changed). A backend with a real limit you have not written down fails the long probe: state the
limit in the contract and plan again, rather than deleting the probe.

A list filter is tested by what it leaves out. For a list target the plan works out its scope: a
request field holding a reference to a step that every fixture also reads is a parent (`id_customer`
of `ListOrders`), a field named `...prefix` is a prefix. For a parent it adds another one
(`create_customer_other`, unique fields changed) with an item of its own (`create_order_other_customer`);
for a prefix, an item whose field contains the prefix not at the start (`x-sku-…`,
`create_product_prefix_inside`) and, when the list's or the field's contract says `case-sensitive`
or `exactly as sent`, one starting with it in another letter case (`create_product_prefix_case`).
The list asserts its exact count, so letting any of them through fails. A prefix that ends in a var
(`sku-${vars.tag}`) gets the separator every fixture carries after it (`sku-${vars.tag}-`), or a run
with `tag=cp-1` would count the items of a run with `tag=cp-10`; `chain lint` warns
`unterminated-prefix` on a chain that still ends the prefix at the var. When the list request has
an enum field whose values are those of an enum field of the items (`ListOrdersRequest.status`,
`Order.status`), the plan finds the writes whose contract takes an item's id (`from:
CreateOrder->order.id_order`) and whose `exports:` or summary name the state they leave it in
(`status CONFIRMED`, `to CANCELLED`), applies one to each further fixture after the unfiltered
list, and adds one list per reachable state (`list_orders_pending`, `list_orders_confirmed`) asserting
only the fixtures in that state come back: by id when the contract states creation order or one
matches, their status always, and the count. A note names states no write reaches, and a write
whose own `needs:` the plan does not call (a confirm that needs `AddStock`) is left out with a note
saying which rpc to plan with it.

A batch is tested with a refused item in the middle. With `conventions.item_envelope_path` set
(`results[].status.code`) and a contract saying failures are reported per item (`reported on that
line only`, `applied independently`), a batch target gets `<step>_partial`: the first item, a copy of
the last item with a numeric field one below the minimum a failure's `when:` states (`qty` 0), and
the last item again. It asserts the batch verdict, `results.0` and `results.2` ok, `results.1` not ok
with the failure's code and reason, and no fourth result; then one read per resource the applied
items touch asserts that the stored value is the one its last item reported
(`product.qty_on_hand equals ${add_stock_batch_partial.results.2.qty_on_hand}`). A batch that stops at
the refused item, applies it, or reports stale values for later items fails.

An idempotency key is tested by replaying it. When a create target's request has a key field
(`idempotency_key`, `idempotent…`, `dedup…`, `request_id`, `client_token`), the plan adds
`<step>_replay`, the same body with the key the step sent (`${steps.create_order.request.idempotency_key}`),
expecting the same id back (`order.id_order equals ${create_order.order.id_order}`);
`<step>_replay_other_body`, the same key with every number raised by one, expecting the first
object back with its numbers unchanged (or, when the contract declares a failure for a reused key,
a reason or `when:` saying idempotency, key reuse or conflict, that refusal); and, unless the key
is in `required:`, `<step>_no_key` and `<step>_no_key_2` sending an empty key, the second asserting
an id different from the first. Copies elsewhere in a plan (a shortage probe's order) get a fresh
`${uuid}` key so they are never mistaken for a replay.
A replay must answer with the object as it is now, not as it was created: a backend that caches the
response at creation replays a CONFIRMED order as PENDING. So when the created object carries a state
enum (`order.status`) and the contracts name writes that take its id and the state they leave it in
(`exports: order: ... status CONFIRMED`), the plan adds, per write, a fresh object
(`create_order_for_replay_after_confirm_order`), the write (`confirm_order_for_replay`), a read
(`fetch_order_after_confirm_order_for_replay`) and the replay of the fresh object's key
(`create_order_replay_after_confirm_order`), asserting the replay's id and every numeric and enum
field the read returns (`order.status equals ${fetch_order_after_confirm_order_for_replay.order.status}`).
A write whose `needs:` the plan does not call is left out with a note naming what to plan with it
(`CreateOrder AddStock`, so `ConfirmOrder` can run).

A `note:` names each step
whose contract declares response facts (`exports:`, `terminal:`, `soft_signals:`) together with
those facts. `chain lint` warns on such a step, planned or hand-written (`envelope-only`, failed by
`-strict`); a refusal probe, a step with `allow_fail`, and an rpc whose contract declares no fact are
not flagged. `shrt chain hollow` counts the same read steps as `asserting only the envelope verdict`.

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
state an invariant between two values — *after == before*, *side A == side B* — instead of a
hand-typed number that only encodes what its author expected. It compares one value with one
value; it does no arithmetic. `equals: ${a.qty}+${b.qty}` resolves to the text `0+5`, which no
number equals, so the step fails every time; `chain lint` warns on it against a numeric field
(`interpolated-arithmetic`, failed by `-strict`). An invariant with arithmetic in it — *give + fees
== get + margin*, *after == before + added* — is stated by pinning each side: choose the inputs
(`vars:` or literal body values), work the expected result out, and assert it as a value
(`equals: 5` on the after-read, `equals: ${vars.expected_total}`), or assert each term against the
step that produced it.

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

It counts the runs of the chains under `paths.chains` only, and every kind of run record of them:
`shrt run` records, `verify` replays (a gate that runs and verifies a chain leaves two records per
gate run), runs of chains kept red and runs that failed, since a read step that passed is hollow
whatever its run's verdict. The record count in its output splits them that way, `19 run
record(s) (12 shrt run and 7 verify replay(s); 14 passed and 5 did not pass; of chains kept red: 1
passed, 3 did not pass; ...)`, so a
count that grows by two per gate run is expected. Runs of a chain no file there declares are
listed apart and not counted: as `scratch <dir>` when they were run by path from a file that still
exists (`shrt run .shrt/scratch/x.yaml`, or a slice `-verify -write`s to such a path), as `orphan <dir>`
when the chain is gone. A run recorded before shrt kept `chain_source` cannot say it was run by path,
so it stays an orphan until the chain is run again. A file run by path whose `name:` is that of a chain under
`paths.chains` is refused by `shrt run` (rename it), so its runs never count as that chain's.

Fix one by asserting what the read should have found. A probe that pins a non-OK envelope value (or
`not_equal` the OK value), and a read asserting `<list>.0 exists: false`, already say an empty body
is the answer and are not reported. A pin written as a reference is judged by its value: a
`${vars.x}` is read from the chain's `vars`, so `equals: ${vars.ok}` holding the OK value is an
envelope-only assertion like `equals: SUCCESS`, and any other reference on the envelope, which
only a run can resolve, never exempts a step. For any other case where empty is right — a cap, a filter that
rejects a bad id — say so in `.shrt/hollow-allow.txt`, one line per step as
`<chain> <step-id> <reason>`; an entry without a reason is
refused. `PITFALLS.md` §24. The summary's `asserting only the envelope verdict` count is the reads
whose expectations touch nothing but the envelope; a refusal probe that pins a detail code field
under it (`error.details.0.app_code`, `reason`) asserts the refusal's detail and is not counted. A
read with no `expect:` at all is counted apart, as `asserting nothing`; both kinds can be hollow,
and `of those hollow` counts the two together.

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
  covers (the id, when the total was wrong) is still sent. A run without `-keep-going` that
  failed names the steps it never sent on a line of its own, `N later step(s) were not run (...)`,
  so a `-quiet` run in a gate does not read as a single red step.
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
from an expired token, because that is a false fail, not a regression. With `expires_path` the
refresh happens before the call goes out. A call answered unauthenticated (401, or the envelope
saying so) drops the token either way and makes a fresh login. The call is then re-sent when it is a
READ (`conventions.read_only_prefixes`), or when its token came from the on-disk cache, no call
in this run had used it yet, and the answer was HTTP 401 (the backend restarted and refused it at
authentication, before the handler, so it did not perform the call); the step records
`auth_retry: resent` with a warning. A write refused with a token the backend already accepted in
this run, or refused in-band (HTTP 200 with `unauthenticated` at the envelope path, which means the
handler ran) even with an untried cached token, is not re-sent, since the backend may already have
performed it: it records `auth_retry: not_resent` and a warning, and the next call or run logs in
fresh. A step still refused authentication is `error`, not `failed`: no verdict about the rpc.
When the refused token came from a login in THIS run and was refused on its first use (re-sent
after a fresh login and refused again, or a write refused with a token just issued), the
credentials work, so the step's error and `verify` both say it `may be an auth regression` in the
backend with that evidence, instead of pointing at the credentials, and `verify` leads with that,
never with a restart, even when the first token had been accepted earlier in the run: a restart
explains a refused old token, not a refused fresh one. It still exits 3; re-run. When the
previous run that sent the step was refused there the same way, each time on a call re-sent with
the token its own login had just issued, `run` and `verify` print `auth refused at <rpc> ... a
finding about the backend` and exit 1. A write refused with a token just issued was never re-sent,
so a restart between the login and the call explains it as well (two restarts timed that way in
two runs look the same), and a repeat of that stays exit 3. A cached token refused and re-sent after a fresh login that is refused too
does not blame the cache: its warning says a stale cached token does not explain the refusal. A token the backend accepted on earlier calls of this run and then
refused points the other way: it likely restarted mid-run and lost its sessions, so the step's
error and `verify`'s `WARNING` say so and it exits 3, whether the token came from a login in this
run or from the on-disk cache; when data created before the refusal is still there after the
re-login (a later step answered with an id created before it, not merely answered), that line says
so too. When the previous run that sent the step was
refused at the same step the same way (the same HTTP status and code: an in-band refusal and a 401
are not the same way), a restart does not explain it: `run` and `verify` print
`auth refused at <rpc> ... a finding about the backend` and exit 1. That finding needs the refusal
to persist after a fresh login, so evidence of a restart in either run overrides the repeat: a call
refused at authentication, re-sent after a fresh login and accepted (a cached token refused on its
first use counts; such a call is never `auth refused`), the refused rpc accepting a later call after
the fresh login, data created before the refusal gone after the re-login (a later step reading it
refused naming its id or as not found, unless the step expects that answer and passed; a list
answered with fewer items than the same call before the refusal; a step expecting a uniqueness
conflict with a value created before the refusal accepted instead), or a step before the refusal
that got no answer from the service (a gateway answer, a dropped connection). Then it stays a
restart, exit 3. A run that passed with a read re-sent after a refused token and accepted says only that
(`a read was re-sent after a refused token at step <n> <id>`), with no restart or re-run advice.

A token refused long before the expiry its login stated (more than a minute, or a tenth of the
stated lifetime, still to go; `token_refused` in the step record keeps when it was issued, its
stated expiry and when it was refused) is not called a restart, whatever the rules above would say:
`run` and `verify` print `WARNING: token refused <N>s after issue although the login said it expires
in <M>s (auth profile <p>, ...)`, and a write it refused is error, exit 3. It becomes `FINDING: token
refused ...`, exit 1, when the fresh token the re-login issued is refused early too in the same run
(one restart cannot end two sessions issued on either side of it), or when the previous run of the
chain also had a token refused early after it was accepted in that run; either run showing a restart
(a step the service did not answer, a build change, data created before the refusal gone after it)
keeps it exit 3. A cached token refused early on its first use is only a warning, because a deploy
between runs explains it: the CI gate counts these lines (README).

A step the backend never answered (the connection dropped, or no answer before `target.timeout`)
is could-not-verify, exit 3, since an outage or a crash explains it. When later steps of the same
run were answered, and the previous run that sent that step got no answer there the same way while
answering later steps too, the backend is up and fails that one rpc every time: `verify` prints
`FINDING: ... the backend fails this rpc every time while answering others`, naming the rpc, and
exits 1. The first occurrence stays exit 3, and so does a repeat where nothing after the step was
answered, and so does a repeat where the same rpc answered another step of either run: then that
call failed, not the rpc, which looks intermittent.

A step the backend did answer, with a server error (Connect `internal`, `unknown`,
`resource_exhausted`, `data_loss`, `aborted`, `deadline_exceeded`, or another HTTP 5xx with a Connect
body; `unavailable` is a gateway or a restart, above), is checked for flakiness before it is called
a regression. When the backend answered the same request at another step of this run, or the
previous run of the chain failed at a different step with the same error and answered this one,
`verify` and `run` print `FINDING: intermittent failure at <rpc>` with that evidence, and `verify`
fails with it instead of `regression: ...` when every change is at such a step, also when an
expectation was edited since the safe spot (an expectation edit never explains a transport error, so
the step's status is never marked `explained by the failed changed expectation`). It still exits 1:
the backend does fail that rpc, only not on every call. When the only evidence is that the previous
run that sent the step answered it, the verdict stays `regression` with a `note: ... this looks
intermittent` line, because a backend change deployed between the two runs reads the same; re-run,
and a failure that moves or passes becomes the finding, while one at the same step again stays a
regression. A flake that lands on the same step every run (a server-wide counter that every run
reaches at the same call) is indistinguishable from a deterministic failure and is reported as one.
"The previous run that sent that step" (here, for an auth refusal and for a reused fixture)
pairs steps the way verify pairs a renamed step, by call and position, so a step renamed since that
run is still found under its old name.

A connection the backend closed after the request was written is `sent, no answer: the backend
closed the connection ...`, like a timeout: the call may have taken effect, `verify` counts the step
as a status change rather than `not sent` when an earlier change is the verdict, and `shrt diff`
lists it as `sent in B, no answer (the connection closed)`. Only a connection closed before the
request was written says `so it was not sent and took no effect`.

- A second kind of principal → declare it as a named profile in the config, then `auth: <profile>`
  on the step. Each profile holds its own token cache.
- A login step is optional. Write one only when the chain is *testing* login, or when the flow
  reads better with it — put it first; it needs no `skip_auth`, since a call to a configured login
  rpc never carries a token (its `auth_profile` is `none`), and its token seeds the cache so later
  steps do not log in twice. It seeds only a profile whose `body` it sent verbatim; a login as
  anyone else seeds nothing, and its `note` says so. Each step's `auth_profile` in the run record
  names the profile it ran under.
- Never `skip_auth` plus a hand-written `Authorization` header. That is the workaround profiles
  replaced, and lint rejects `skip_auth` and `auth` together. Nor a hand-written header on a step
  an auth profile covers: the middleware would overwrite it and the step would run as that
  profile's principal. Lint rejects both, and `shrt run` (and `verify`) refuses the chain before
  sending anything, naming the step.
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
lists them as `no path to`, except the login the config's `auth` calls (a profile's `call`), which
is known to need no path; for the rest the column cannot tell the two apart and does not try. A streaming rpc
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
| 9 | 1 | happy | a top-level response field (scalar, list or singular message; not the envelope) named in no `exports:`, `terminal:` or `soft_signals:` — an entry whose description is a `TODO` names nothing |
| 10 | 1 | failure | a failure with no `when:`, `unreachable:` or `pending_deploy:` |
| 11 | 2 | happy | a unary rpc in the catalog that no overlay covers: it is scored as an empty entry (rows 1-10 as they apply) plus this row, so deleting an overlay or an entry raises the score instead of lowering it |
| — | — | — | codes the backend raises that no contract declares at all |

The eleven scored rows are the terms `shrt contract status` prints in its footer, the same list
that computes the score — so run the command for today's list, and read the rows below for the
reasoning a one-line label cannot carry. In shrt's own development repo a test fails the build
when this table and the scored terms disagree; that test does not ship with the binary, so here
the command's footer is the authority.

**An unfilled `TODO` scores nothing.** It is listed beside the score as a hint, never charged. The
row that used to claim `1 per unfilled TODO` was wrong for as long as it stood: no scored row
counts unfilled TODOs, so clearing every TODO in a file moves the score only where clearing it also
answers one of the eleven rows above.

**The phase column is what `-phase` filters.** `shrt contract quality -phase happy` scores the eight
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

**Row 9 charges every top-level response field but the envelope (`error` by default): a scalar, a
list, and a singular message alike.** An entry names it by its head, so `order` or any dotted path
into it (`order.status`, `customer.id_customer`) declares `order`; a `TODO` description declares
nothing. Until 2026-09-24 a non-repeated message was skipped, so `terminal: order` set to a `TODO`, or
deleted, still scored 0 while `orders` (repeated) was charged. Reach into a message with a dotted path
(`row.id_reference_rate`) when a later step needs the value.

Every row but the last needs only the descriptor and the overlays, so they live in the binary. **The
last one cannot**: finding the codes a backend raises means reading that backend's source, and shrt
drives a backend it never imports. shrt ships no tool for it: in the development repo it is
`scripts/contract-quality.py`, which **exits 2 rather than 0 when it cannot find that backend**,
because a check that cannot run must fail, and you write the equivalent for your own backend.

The score itself can be gated as a **ratchet** with `shrt contract quality -gate -baseline <file>`:
it fails if the score rises, and also if it falls without the baseline being lowered, so improving
a contract means lowering the number in the file. A failing gate lists the gaps it counts now, per
rpc (`CreateOrder (2): 1 undocumented field(s): note`), since the file holds only a total; a new
undocumented field is most often one the descriptor gained (a proto field added to a request), so
document it in the rpc's `fields:` and the score comes back. `shrt chain hollow -gate -baseline <file>` does
the same for hollow reads. A baseline file that does not exist fails the gate with the command
that creates it with today's count (`echo <n> > <file>`; or write 0, run the gate once, and write
the number it reports). That is what stops an N-of-N score from meaning less each time the
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
`before:`: the `needs:` already orders the aliases. The same note fires when a plain step sends
exactly the body an aliased sibling sends. It does not fire when the plain rpc and its aliases are
each wired on purpose with different values, such as order line 0 `from:` `CreateProduct` and line
1 `from:` `CreateProduct@b` with its own sku and price: that is two products, not a duplicate.

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
step's response message does not have, such as a misspelt `${create_product.product.id_prodct}`, or a request path the earlier step's
request message does not declare, such as `${steps.create_order.request.id_custmer}`; so is one
filling a numeric field with a reference whose declared type can never be a number, such as
`qty: "${create_product.product.created_at}"` (a timestamp) into AddStock's int64 `qty`, or a bool,
an enum or bytes; so is one filling a single-valued field with a whole list or map, such as
`qty: "${create_order.order.lines}"`, or a list or map field with a single value. A string source, such as `.product.name`, is only a `chain lint` warning (not
failed by `-strict`) and is sent: protojson accepts a digit string like `"5"` for an int64, and
backends often carry numbers as strings. Refused as well is
one whose step body the proto rejects (an unknown field, an enum value the message does not
have) in any step, not only the first: `shrt run` validates every request up front as `-dry-run`
does, with the same synthetic values for references to earlier responses, and exits 1. A body
that fails only because of such a synthetic value is left to the run, where the real value decides. A step with no `expect` that the backend refuses
in-band stays `passed` with a warning under it; only `chain lint -strict` stops it. A step that
declares expectations and is refused in-band FAILS unless one of them pins the verdict (`equals`
on the envelope path itself, or any rule on the envelope path or a `transport.*` path that would
FAIL on a successful answer, such as `not_equal: SUCCESS`): `expect qty_on_hand equals: 0` holds on
the zero a refusal leaves, and must not turn the step green. A rule on a sibling of the verdict
(`status.message`, `status.details.0.reason not_equal: X`) pins nothing, except an `equals` or `contains` of a non-empty value on a `code_fields` entry under the envelope's parent (`status.details.0.app_code equals: 1101`, `status.details.0.reason equals: EmailTaken`), which names the refusal exactly as it does on a refused batch line; path case does not matter. A pin the refusal and the ok value both satisfy
declares nothing: `status.code not_equal: ""`, `not_equal: REJECTD` (a typo), or `transport.code
equals: ok` on a call refused in-band. References in a pin are resolved first, and `chain lint`
warns on `not_equal: ""` on the envelope, which `-strict` fails. The same holds for a
response that carries NO verdict where its message declares one (no envelope, `status: {}`, or an
empty code): it fails unless an expectation pins the envelope (`status.code exists: false` when
an absent verdict is what the rpc answers; a `not_equal` does not, since it holds on an empty code) or the transport, and warns on a step with no
`expect`. Every step warning is
repeated in the closing summary as `warning [<step>]: ...`, so `-quiet`, which drops the progress
lines, still shows them; `verify -quiet` prints the same lines after its diff summary.

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
reads). The sent excerpt leads with literal inputs (`lines.0.qty=3 lines.1.qty=2`) and puts id- and
uuid-shaped values (`id_customer`, `idempotency_key`), usually references, after them, abbreviated;
a literal input is shown in full whatever its length (`email=cust-order-confirm@example.test`), so a
fixture built from a long tag reads the same as one from a short tag; the asserted cell shows every
expectation with its value in full (`customer.name equals Customer for order-confirm`); the answered
cell gives the verdict, then the value the backend returned at every path the step asserts, except
id-shaped ones (`order.total_minor=4548`), then `also baselined:` with the values the step does NOT
assert that still become the baseline verify compares (`also baselined: order.total_minor=300
order.lines.0.qty=1`), leaving out ids, timestamps, everything under a volatile path (a step's
`volatile: [products]` leaves out the whole list), empty strings and values echoing a var, and
showing a list the step declares `unordered` as one entry (`products=3 item(s) in any order`), since
verify compares it as a multiset, not by index; shallow paths first, capped with `+N more`; every
value in the answered cell is shown in full, never clipped, since it is what the approver signs; read
those too, since approving signs them. It writes no safe spot, and `shrt verify` still has nothing to compare against. Proposing
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
   that differed and is not masked the way `verify` masks it: every `verify` would report those
   as drift. An id, a timestamp, and a value that only echoes a fixture name (a response `sku`,
   `name` or `email` that follows the run's `sku-${vars.tag}`) are masked by `verify` and are not
   listed, so do not declare them volatile. Each field is listed with both values. The fields of a list
   that grew or shrank are one line, the list's path with a count and the fix (`volatile: [<path>]` on
   the step, or `unordered: [<path>]` if only its order changes), since a list other runs add to grows
   every run. A list with as many items as before and only some fields inside them changed is not
   that: its line names each field and its values (`orders.0.total_minor` 750 -> 1), says it may be a
   real change, and suggests only the field (`volatile: [orders.*.total_minor]`), also when the
   field is new in every item (`orders.0.note absent -> gift wrap`). A step present in one run and
   absent in the other is a chain edit, not a field that changes every run, and is never listed.
   Under `-supersede`, a field where the earlier run still held the value of the safe spot being
   replaced is the change you are signing off on, recorded by a run from before it, not
   instability: those are listed apart (`N field(s) differ from the earlier passing run ... only
   where that run still held what the safe spot it replaces holds`) with no volatile advice; run
   the chain once more against the new backend and propose again to check them. Pass the warning
   on, and fix it before asking (add the paths to `volatile:`, re-run, propose again) unless the
   difference is real. With no earlier passing run the summary says the check was not made; run
   the chain once more first. A `-supersede` proposal is also compared with the safe spot it
   replaces, which is what the user signs off on: the table gains a `vs replaced safe spot`
   column, and every difference from it is listed under the table: requests and responses, a
   target change (`target base_url <old> -> <new>`), and each chain edit since then, such as an
   expectation added or changed (`chain expect absent -> <path> <rule> <value>`) or an `unordered`
   path added (`chain unordered absent -> unordered: [<path>]`), which verify will compare as a multiset. A request or
   response value that differs only in the fixture name (`sku-${vars.tag}` under a fresh tag) is
   masked there as `verify` masks it, so it is not listed;
5. the question: approve or reject.

Run `shrt confirm <name> -approve -by <user email>` only after the user answers yes to THIS
proposal. The email is the user's own, as the session knows it; if you do not know it, ask. A
bare name is refused. `-reject` discards the proposal. A run record carries a `seal` shrt writes
with it, and `confirm` refuses to propose (and `-approve` to approve) a record whose content no
longer matches its seal, or that has none: a record flipped from failed to passed by hand is not
what ran. A record written before seals existed is refused too, with a message that says so first
(`the run record predates sealed run records`) rather than calling it tampered; run the chain again
and propose the new run. `diff`, `verify -run`, `chain slice`, `chain which` and `chain hollow`
refuse an edited record too (`which` and `hollow` leave it out and name it), and read an unsealed
older one as recorded after one `note:` line saying it predates seals. Deleting `seal` does not make
a record look older: every sealing build also writes `format`, and a record that has `format` but no
`seal` is refused everywhere as edited (`its seal was removed`). The seal catches an edit, not a forger who recomputes it: it is a checksum,
not a signature. Approval refuses a run record rewritten
after the proposal, since the user approved what the summary showed: the proposal's digest covers
everything that becomes the safe spot (target, build, vars, volatile, and every step's status,
request, response, http status and transport error), not only the responses. The proposal also
records the branch and commit it was proposed on (the summary names them), and `-approve` refuses
when the chain checked out now has another `chain_digest` than the one the proposed run recorded,
naming each difference a run record shows (a step changed, added, removed or reordered, a body
template that no longer produces what was sent) or saying it lies where a record does not show
(vars defaults, allow_fail, export, redact, kept_red, a description): a proposal
made on a feature branch and approved after `git checkout main` would otherwise baseline a chain
main does not have, and the next `verify` there reports drift with different input. Check out the
branch the proposal came from and approve there, or run the chain as it is now and propose that
run. The pending proposal is kept on refusal. The safe spot
keeps that digest, sealed together with who approved it and when (`confirmed_by`, `confirmed_at`,
`note`), and `verify` refuses a safe spot whose content or approval no longer matches it: a hand
edit is not what a person approved. A safe spot sealed before the approval was covered still
verifies, and `verify` says it is of the older kind; re-approve it with `-supersede` to seal it. Restore the file, or re-approve with `-supersede`. `-pending` lists what awaits
a decision, and `chain ls` marks it `?`. A chain that already has a safe spot needs `-supersede`
on the proposal, and the old one is archived on approval as
`.shrt/safespots/archive/<chain>/<run id>.json`, named by the run it held (the id the new safe
spot's `supersedes` names; a run archived twice gets a `-2` suffix).

A safe spot belongs to its chain's name. Renaming a chain file (and its `name:`) leaves the safe
spot behind, and `doctor` warns that `chain "<new>" has the same step ids and calls`. When the
rename is all that changed, carry the approved safe spot across instead of asking for a new
approval: `shrt confirm <new> -rename-from <old> -by <email>`, with the email of the user who
agreed to the rename (the same rule as `-approve`). It refuses unless `<old>` has a safe spot and
no chain file any more, `<new>` has no safe spot, nothing is pending for either, and `<new>` is
identical, apart from `name:`, to the chain file the approved run ran: the safe spot keeps its
`chain_digest`, so every body template (not only the value it resolved to), vars default,
`allow_fail`, `export`, `redact`, `kept_red`, `unordered`, expectation and description is
compared, whether or not the rename is committed yet. Any other difference is refused, naming
where it differs from the last committed `<old>.yaml` when that is the approved file, and the chain
is run, proposed and approved normally. A safe spot approved by an older shrt has no
`chain_digest`, so nothing proves the rename pure: it is refused the same way. On success it moves `<old>.json` to `<new>.json` keeping
`confirmed_by`, `confirmed_at` and `note`, records the rename under `renamed` and re-seals the
digest. Run records of `<old>` stay under `.shrt/runs/<old>/`; `chain hollow` lists them as an
orphan `renamed to <new>`, explained as a rename rather than a deleted chain (excluded from the
counts and the gate; the renamed chain's own runs count), and the command prints the `rm -rf` that
removes them.

Two branches can each supersede the same safe spot, each approved by a person on its branch. The
merge then conflicts in `.shrt/safespots/<chain>.json`, and neither side is right for the merged
backend until it is checked there. Resolve it this way, never by editing the JSON by hand (a hand
merge breaks the digest, and `verify` refuses it):

1. Take one side whole: `git checkout --ours .shrt/safespots/<chain>.json` or `--theirs`. Pick the
   side whose chain file the merge keeps; if the chain file conflicted too, resolve it first and
   take the safe spot of the branch whose chain won.
2. Finish the merge of the code, rebuild and deploy the merged backend, and run the gate on it
   (`shrt doctor -strict`, then `shrt verify <chain>` for each safe spot).
3. If `verify` passes, commit the merge. If it reports drift, or the merged chain is neither side's
   chain as approved, run the chain on the merged backend, propose it
   (`shrt confirm <chain> -supersede -note "merged <a> and <b>: ..."`) and have a person approve it.
4. The losing side's approval is not lost: it stays in git history on its branch and in the merge
   commit's parent, which is where an audit reads it.

A safe spot left with conflict markers is caught: `shrt verify` refuses it with `git merge conflict
markers in safe spot ...` and this remedy instead of a JSON parse error, and `shrt doctor` FAILs its
`safespot-digests` check on it.

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

After you touch it, the same two lines.

A refactor can also make a call slower without changing a byte of its answer. `verify` (and
`shrt run` of a chain with a safe spot) compares each step's latency with the safe spot's run and
prints `LATENCY: ListProducts at step list_products took 701ms, the safe spot's run 1ms (+700ms,
701.0x); re-sent 2 more time(s), every answer slow: ...` when a step is at least 250ms slower and 3x
as slow. A slow read is re-sent with the same request before it is judged, and judged on its fastest
answer, so one slow call from a busy box is not reported; a write is never re-sent, and is confirmed
only when the previous run of the chain was slow at the same step too. It is a warning: the exit code
stays 0 unless `.shrt/config.yaml` sets `latency: {fail: true}`; tune `floor_ms`, `ratio` and
`remeasure` there (`GRAMMAR.md` §4). `shrt verify <name> -latency` lists every step, before and after.

Then read the report. When the replay ran against another target than the
safe spot's, the report opens with `targets differ: safe spot <a>, this run <b>`: a difference may
then come from the target, not the code. A drift report names the step, the path, the change kind
(listed in `GRAMMAR.md` §7), `want` and `got`, so a regression arrives as *which rpc changed* instead
of a failing test somewhere downstream. A clean report covers only the steps of that chain's safe
spot, and says so: a regression in a path no safe spot exercises is not seen. A `regression` verdict
ends with the next step for a change you meant (`If the change is intended ... shrt confirm <name>
-supersede -note "..."`, then a person approves it), and says so when every change is a response
field the safe spot does not have (`all of them response field(s) the safe spot does not have:
create_order order.currency, ...`), the shape of a field added on purpose. A field the descriptor
gained that the backend does not send yet is not a change: run records hold every declared field,
so the new field shows up at its proto3 default (`note: ""`, `0`, `false`, an empty list, a null
message), which is exactly the bytes the safe spot's backend sent. verify leaves those out and
names them (`N response field(s) are declared now but were not on the wire ...`); the day the
backend sends a non-default value there, it is reported as `unexpected` like any new field. That
holds only for a field the safe spot's run did NOT have on the wire: a field the backend already
sent before the proto declared it (the run's `response field(s) the proto does not declare`
warning) is compared with the value that run received, which the record keeps under `undeclared`.
The same value is not a change (`... were on the wire, undeclared, in the safe spot's run with the
same value ...`); a field gone since, now at its default, is `changed product.active want=true
got=false (on the wire, undeclared, in the safe spot's run; not on the wire now ...)`. A safe spot
approved by an older build has only the warning, not the value: a field gone since is still a change,
and a value now is listed as `not compared` until a newer run is proposed in its place.

A drift can also come from the chain itself. `verify` first compares what each step SENT with what
the safe spot's run sent, and prints each difference before the response changes:
`request differs from the confirmed run at create_order lines.0.qty (3 -> 4)`. Values built from
another step's output or from `${uuid}` / `${now}` are skipped, since they differ every run; the
reference itself is not. Rewire `id_customer: ${create_customer.customer.id_customer}` to
`${create_customer_2...}` and verify prints `chain differs from the confirmed run at create_order
body.id_customer (${create_customer.customer.id_customer} -> ${create_customer_2.customer.id_customer})`
and fails with `drift after a chain change`, not `regression`. Two
kinds of request difference are printed on one line but are NOT different input: a fixture name,
a string that interpolates a var inside other text (`sku-${vars.tag}`, `Widget ${vars.tag}`), and a
value under a `volatile` path. A var is a fixture name only where it isolates: the chain reads it
inside other text in some field that is not an id, and never next to digits alone; `qty: "${vars.q}0"`
and `id_customer: cus-${vars.n}` are input. That is what a fresh `-var tag` changes, so a CI replay with a new
tag is still compared like with like, and a response value that only echoes the new name (the
confirmed value with the old name swapped for the new) is masked and counted. Every other request
difference is input, and it explains a response change only causally: at its own step; at every
later step when its step is a write (not matched by `read_only_prefixes`) whose own response
changed, since the write demonstrably did something different and the server state after it may
differ; otherwise only at a later step that reads (through a reference or an export, transitively)
a field of an earlier step whose value actually changed with an explanation of its own, or
reads a request value of a step that changed. It is the field read that counts, not the step: a
list that shrank from 2 items to 1 explains nothing at a step reading `${list.products.0.id_product}`
when that id is the same product after renaming, so a change there is a `regression`. A step whose
response did not change passes no explanation on: a header the backend ignores at `create_customer` explains nothing at
`create_order`, which reads the unchanged customer id, while a changed `qty` at `create_order`
changes its total, so it explains every later step, the stock reads of the product included. With a request difference the verdict is `drift with different input`, not
`regression`, when every response change is explained this way; any other change is a
`regression`, including one after the differing step that reads nothing it changed. A `-var` that changes no request (a var only expectations
read, `-var total=6250`) is not input at all. Restore the input, or, when the edit is intended,
bring the expectations in line, run it green, and propose that run with `shrt confirm <name>
-supersede`. When the difference comes from vars rather than the chain file (a `-var` on this
verify, a `-run` recorded with other vars, or a confirmed run given a `-var` this verify leaves
at the chain's own value: `ik=key-one, confirmed with k-b1`, fixed by `Verify with -var
ik=k-b1`), verify names the vars that feed the differing
request values, `qty=2, confirmed with 3`, instead of blaming the chain's input; when the chain
file changed too (a step's `auth:`, the step list, an expectation), it names that edit as well,
and never says the chain file is not what differs. An expectation added, removed or edited since
approval is a `chain differs ... <step> expect (...)` line: it explains a status change at that
step and the later steps the run then did not reach, not a response change. A response that
depends on a fixture name other than by echoing it (a list sorted by name) is reported; declare
it `volatile`. A list whose order the rpc does not promise is declared `unordered: [products]` on
the step (or the chain): verify compares it as a multiset, pairing items by content; a changed
item is named at the safe spot's index and, when it sits elsewhere in this run, at that index too
(`this item is at products.1.price_minor in this run`). Without the
declaration, the same items in another order are reported as `same items in another order`, and
when that is every change verify fails with `order changed`, not `regression`, also when an
expectation reading the list by position (`results.0.status.code`) failed because of it: that
step's status change counts as the reorder, the failed expectations are named on the line and in
the verdict, and the per-item changes (ids included) under the reordered list are counted but not
listed (the line says how many; `-json` lists them). A changed value at a later step reading the
list by position shows as `want` the safe spot's value with this run's fixture names swapped in
(`product.sku want=sku-ln2-a got=sku-ln2-b`), so it reads as what this run should have got. The
`first failing step` line names the first expectation that failed, with `-quiet` too.

Three things that decide whether this works for a given chain:

- **`verify` compares every step the run reached, not only the steps before the first red.** A
  live `shrt verify <name>` runs the chain the way `shrt run -keep-going` does, so a red step does
  not hide the drift behind it: every step that was sent is diffed against the safe spot, and a
  step that was not (held back as `skipped` because it reads the failed step, or never sent
  because the run stopped) is reported as `not_reached` rather than as a status change or a
  shorter chain. The report names the first failing step. A recorded run that stopped at its
  first red (`-run <id>` of a run made without `-keep-going`) reports every step after the stop
  as `not_reached`. Consecutive steps not reached for the same reason (a stopped backend, say)
  are one line, `[<first>..<last>] not_reached <n> step(s) ...`; `-json` still lists each.
- **`shrt verify -run <id>` re-diffs a RECORDED run and sends nothing.** It needs no backend and no
  credential, so the after-check costs one run, not two. A record whose `chain` is another chain,
  copied into `runs/<chain>/`, is refused, here and wherever a run is loaded by id. It is also how you investigate a drift
  without spending another live run. `-run latest` takes the newest run record of the chain, an
  earlier `verify` replay included, and prints `verify: -run latest is run <id>` on stderr so you
  know which one it diffed; when that is the safe spot's own run it says so, as for the id.
- **A chain that creates things is re-run with a fresh `-var tag`, so every tag-derived value
  legitimately differs.** The principal a step runs as is input too: a step whose `auth_profile`
  differs from the safe spot's, such as `auth: clerk` added after approval, fails `verify` with
  `drift with different input` naming the profile change, even when every response matches.
  A safe spot confirmed before shrt recorded `auth_principal` cannot tell which account ran:
  verify prints `principal checking is off for safe spot …` and calls a drift against it
  `drift, principal not checked` (exit 1), not `regression`. Turn it on with
  `shrt confirm <chain> -supersede -note "..."` and a person's approval.
  The chain's step list and expectations are compared too: a step removed, added, moved or
  re-pointed, an expectation edited (compared as the chain declares it, so `within: {of: "${nowunix+3600}", by: 10}`
  resolving to another second is no edit), or a body field reading another step's field, since approval is a `chain differs` line, a chain change
  rather than an input change, and alone it fails with `drift after a chain change`, not a `regression`; a move is
  never a response change, so beside an expectation edit it still makes the verdict a chain change. A removed, added or
  re-called step explains only the steps it can affect: itself, every step after it when it is a write, and every step
  reading it. A change at any other step (a read left in place when an unrelated trailing read was deleted) is still a
  `regression`, and verify says the chain change `cannot affect` it. A step renamed in
  place (same call, same position, and that call's steps still at the same positions, so two steps of one call
  renamed together, or their ids swapped with the bodies left in place, pair by position) is not a removal and an addition;
  nor is one renamed beside an inserted or deleted step: the steps whose ids did not change are paired first, and
  between them a renamed step pairs with the old step of the same call in the same relative order:
  `verify`, `diff` and the `-supersede` review say `renamed step(s): step 8 list_orders -> list_customer_orders` and
  compare its response with the old step's, so a changed value there (`orders.0.total_minor want=750 got=1`) is judged. A call respelled to
  the same rpc (`ListProducts` to its fully qualified name) is not a change: the recorded
  `procedure` decides. Expectations are paired by path, then by rule, not by position, so one
  added in the middle is one `absent -> <path> <rule> <value>` line; each path reports its own
  added, removed or changed expectation, with values as the run resolved them.
  `verify` masks what `diff` masks: config and chain `volatile` paths,
  and a changed value that is id- or timestamp-shaped (`id`, `*_id`, `id_*`, `idX`, `*_at`, a
  UUID, an RFC 3339 time), counting how many it did not report. Both values must be id-shaped
  alike (two non-zero numbers, or two non-empty strings of the same shape, with the same letters
  before the first separator): an id that became `""`, null, `0`, `undefined` or a different JSON
  kind, or disappeared, is reported, and so is an id of another kind (`cus-...` became `prd-...`). A timestamp is masked only when both values are the same unit (RFC 3339 text, or unix seconds, milliseconds, microseconds or nanoseconds by digit count under a time-shaped name) and within 400 days of their own run; `expires_at changed unit: seconds -> milliseconds`, or a time far outside the run, is a counted change. A value the chain builds
  from `${uuid}` or a clock form, whole or inside other text (`sku: s-${uuid}`), and a response
  value that only echoes it (a message quoting it), is treated like a fixture name and masked and
  counted (GRAMMAR §7), so it needs no `volatile`. Anything else that differs every run and that
  the chain did not build, such as a server-generated code that is not id-shaped, must be in
  `volatile`, or the first replay reports a regression that is not one. The price is that a wrong id that is still
  id-shaped is not caught by `verify`; assert on it if it matters. The mask is part of what was
  approved: the safe spot stores its `volatile` patterns, and a pattern added to the config or
  the chain later (`**.total_minor`, `**`) fails `verify`, which names the pattern and every value
  it hid, until a run under the wider mask is proposed with `-supersede` and approved. A `redact` path is a mask too: a
  redacted response value is blanked in the safe spot and the replay alike, so it is never
  compared; `confirm` lists such fields and `verify` counts and names them. A `redact` pattern
  added after approval blanks values the safe spot holds in the clear: `verify` does not compare
  them and fails naming the pattern and each value, like an unapproved volatile pattern, until a
  run under it is proposed with `-supersede` and approved. The report
  counts the values it kept out, both kinds; `verify -masked` lists every one of them, the
  volatile ones and the id- or timestamp-shaped ones, with its path and both values. This is per-chain work and it is why paving the corpus is not a bulk
  operation — see the development repo's one worked example, `.shrt/safespots/seed-position-exposure.json`, whose
  `volatile` list is 14 patterns long.
- **A chain kept red on purpose must NEVER be confirmed.** Such a chain asserts the correct
  behaviour and pins the known defect it shows with `kept_red` (a step, an expectation path, and
  the value it got when that is stable); a safe spot would freeze the bug as ground truth. Pin
  every expectation that fails today, and nothing more: `shrt run` goes past every failure,
  pinned or not, as `-keep-going` does, so several pins are all evaluated in a plain run (the gate's), and exits
  0 only while the chain fails exactly there, and 1 when an earlier or later step regresses, a
  step is left unsent, the failure changes, or the defect is gone. `shrt confirm` refuses a chain
  with `kept_red`, saying so, even when its run passed. `PITFALLS.md` §11.

**One real defect in a long chain: keep it red in a slice of its own, confirm the rest.** A planned
chain of 49 steps that shows one baseline defect cannot be confirmed (it did not pass) and must not
be kept red as a whole if you want the other 45 steps guarded by `verify`, since a chain with
`kept_red` never gets a safe spot. Split it in two commands, from the run that showed the defect:

```bash
shrt run orders -keep-going                                   # red at confirm_order_insufficient_stock_last_item
shrt chain slice orders -step confirm_order_insufficient_stock_last_item \
    -kept-red -verify -run latest -var tag=<fresh> -write orders-last-line-red
shrt chain slice orders -without failed -run latest -write orders-rest
shrt run orders-rest -var tag=<fresh>                          # green: propose and approve it
```

`-kept-red` pins the slice on every expectation of `-step` that failed in the run (`kept_red:
[{step, path}]`, no `got`), so `shrt run` of it exits 0 while the defect is there and 1 once it is
gone or anything else fails. With `-verify` it pins only a slice that reproduced the step's verdict:
a slice that lost a dependency which is state rather than a reference (the `AddStock` that stocked
the first line) passes where the chain failed, so it is not pinned and not written, and the `next:`
line (which keeps `-kept-red`) says what to `-keep`. Without `-verify` the pinned slice is a
hypothesis: a run saying `PINNED DEFECT GONE` while the chain still fails means exactly that.
`-without <id,...>` writes the chain minus those steps and every step that reads one of them, by a
reference or an export; `-without failed` names every step that failed in the run (`-run`, default
latest), which leaves out the after-reads that fail with the defect too. It lists each step left out
and why, and drops their `kept_red` pins. A step left in can still depend on what a left-out write
did to shared state, so run the rest before proposing it; `-write <chain>.yaml` replaces the chain
itself. Remove the kept-red slice and plan again once the defect is fixed.

**Before a chain has a safe spot, `shrt diff` is the run-to-run check.** Only the user's yes
creates a safe spot, so a refactor often has to be checked with none:

```bash
shrt run <name>                                  # before the change
shrt run <name>                                  # after it
shrt diff <name>                                 # the two latest runs that are not verify replays
shrt diff <name> <run-a> <run-b>                 # or any two runs; ids, latest, latest~N
```

It reports step status changes, where the first failing step moved, steps reached in one run and
not the other (a step `-keep-going` held back as `skipped` counts as not reached), and, in steps
both reached, what each SENT (request values, the step's own `headers`, the `auth_profile` a step
ran as, and the rpc, where `ListProducts` and `shop.catalog.v1.ProductService/ListProducts` are the
same call) before the response differences. A request value that only differs in a fixture name
(`sku-${vars.tag}`, `X-Tag: t-${vars.tag}`) is counted, not listed, as verify does, and so is a
reference that copies one from an earlier step (`sku: ${steps.create_product.request.sku}`). It masks the `volatile` patterns stored in each record plus the
ones in today's config and chain file, so a pattern you add after the runs still applies. When
those patterns cover every response field of a step (`volatile: ["**"]`), the report opens with a
`WARNING` naming the steps it compared nothing of (`fully_masked` under `-json`), because "no
differences" then says nothing about them, as `confirm` warns for the same patterns and `verify`
opens its report for a `no drift` it reached the same way. It also
masks ids and timestamps, which differ every run: a field named `id`, `*_id`, `id_*` or the
camelCase forms, a `*_at` or `*_time` field, and any pair of uuid or RFC3339 values, as long as
both values look alike: an id that became empty, null, `0`, `undefined` or another JSON kind is
shown. A value derived
from a run tag (a response sku, name or email that echoes the `sku-${vars.tag}` the run sent) is
masked the way `verify` masks it and counted (`N response value(s) differ only by echoing the fixture
name`), so it needs no `volatile`, also when the field that sent it is id-shaped (`id_customer:
cus-missing-${vars.tag}`, echoed in a refusal message `no customer cus-missing-…`); a value that differs
in anything else is shown. An id inside a longer string
(an error message naming the product) is compared after the same renaming: the message is equal when
the only difference is an id the two runs renamed one-to-one, and any other change of its text is shown,
so it needs no `volatile`. The report says how many values it hid, and names
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
   failure: the envelope code, an `app_code` detail, a `reason` detail, `transport.code` (a
   Connect refusal such as `invalid_argument` or `unauthenticated`) or `transport.http_status`
   (`-code 401`). A run record's step refused at authentication (status `error` with a 401) counts
   as observed: the backend answered it. The searchable paths are
   derived from the corpus, so a chain asserting a code under a batch result — the corpus has
   `results.0.error.details.0.app_code` — is found without teaching the command a new shape. Both
   selectors together intersect. An `app_code` and a `reason` (any two `code_fields`) that a local
   run record carried side by side in one object, or that a contract's `failures:` entry declares
   together (`code: 1603`, `reason: PermissionDenied`), name the same refusal, so `-code 1603`
   also finds a step asserting only `reason: PermissionDenied`, and the other way round; the header
   line lists the aliases it searched (`asserts 1603 or PermissionDenied (seen with it ...)`).
2. **`asserted` and `OBSERVED` are different claims.** `asserted` means the chain says that step
   answers that code. `OBSERVED` means a run record under `.shrt/runs/` reached that step, and the
   line cites the NEWEST such run, whatever it got — `README.md`'s "where the authority is" rule,
   applied to discovery. Under `-code` matches rank in three tiers: observed and holding, then
   asserted only, then observed but contradicted (the newest reaching run got a different code at
   the asserted path). Within the first tier a step that passed outranks one that failed while
   still answering the code, and within the last a step that passed outranks one that failed,
   because a chain the backend has stopped answering with that code is the least likely to
   reproduce it. Under `-rpc` alone the question is an incident at that rpc, so the order is the
   other way round: a step whose newest reaching run FAILED there ranks first (one that still got
   the asserted envelope code before one that did not), then observed steps that passed, then
   steps no run reached, and the first `reproduce:` line slices the red step instead of a green
   one. Under `-code` the `reproduce:` line slices a step whose newest reaching run FAILED there
   too, when the chain has one, though the listing still ranks it last: the failure is what there
   is to reproduce. Only runs recorded against the config's target are cited. Run records are gitignored and machine-local, so a
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
   incident. That holds for a read. When the step itself is a write (`ConfirmOrder`, anything
   `conventions.read_only_prefixes` does not name), the line is the closure form with
   `-keep writes` and no `-run`: pinning would re-send the write on the entities the recorded run
   created and already changed, and a confirm then answers `OrderAlreadyConfirmed` instead of
   reproducing anything; `-keep writes` keeps each earlier write its state may depend on (a
   restock the closure alone would drop). When a write the slice keeps interpolates a var into what it creates, the line ends
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
   `reproduce:` slice command built as in 3 (the pinned form for a read, `-keep writes` for a write,
   and the pinned form with `-keep writes` for a read of an entity a write of the chain created,
   which a pinned slice would drop and call INCONCLUSIVE),
   and exits 0. A step that asserts the code's alias (the `reason` seen with it) is an asserting
   match, not one of these. The backend exercises the code and no expectation pins
   it, but that is not always unguarded: each step also says when an expectation pins a sibling of
   the same detail (`results.1.status.details.0.reason equals ProductNotFound`, which fails `shrt run`
   on a different refusal) and when the chain's safe spot holds the code at that path (`baselined:`),
   so `shrt verify` reports a change to it. Only a step with neither is one where a change goes
   unnoticed; assert the code there to have `shrt run` fail on it and `chain which` list it.

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
   gives it), preferring among those the one the body references; for an unaliased `needs` or
   `before` edge it keeps every call of that rpc that acts on an entity the step references (a
   `CreateOrder` whose lines name two products keeps the `AddStock` of each, not only the later
   one; the entities count through references, so a `ConfirmOrder` of an order whose lines name
   two products keeps both too), else the referenced step, else the nearest. A fourth reason,
   `changes the state <rpc> sets on <step>, which <target> needs`, keeps an earlier write that
   changes the state such an edge depends on: a write of that rpc, or of an rpc whose own contract
   `needs` it (`ConfirmOrder` needs `AddStock`: an earlier confirm reserves stock), on one of the
   same entities (an earlier confirm of another order with a line on the same product). A write
   expected to be refused (an envelope `not_equal: <envelope_ok>` counts) changes nothing and is
   not kept for this. An edge declared under one alias of a contract binds only
   a step carrying that alias. A `from` / `same_as` edge on a field the kept step fills with a
   literal or a `${vars.*}` value needs no producer and keeps nothing. A step that expects a
   refusal (a `transport.code` other than `ok`, an envelope code other than `envelope_ok`), or
   that the `-run` record shows refused, created nothing and is never kept as a producer; when
   no other step calls the rpc the edge is listed as unmet. Steps are numbered from 1, as in
   `shrt run` and the run record, and so are expectations in a verify difference.
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
   and prerequisites; `-keep writes` does so for every earlier write step (see INCONCLUSIVE below).
   `-write` and `-write <name>` put the file next to the source chain: in `paths.chains` for a
   chain there, and beside it for a chain given by a path outside it (`.shrt/scratch/min.yaml`
   slices to `.shrt/scratch/min-slice-<step>.yaml`), so an exploratory slice never lands in the
   directory every sweep and gate runs. A value with a slash or ending in `.yaml` is written exactly
   there. `-write` refuses to replace an existing chain file unless `-force` (exit 1, or 2 under
   `-verify`, before anything is sent), except a slice this command wrote of the same chain and step
   (its description starts `Slice of <chain> reproducing step <step>:`), so the `next:` loop can
   re-slice in place, and the source chain file itself when you name it with `-write <its path>`:
   the minimal-chain recipe below replaces the chain with its verified slice that way.
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
   expectation, and for an expectation that failed in both runs, its want and the value it got —
   against that same step in the source run. A failure against another want, or with another got,
   is NOT REPRODUCED (`failed in both, with other values: source want 3697 got 1995, slice want 4548
   got 6250`): it is not the same failure. So that like is compared with like, `-verify` writes the
   source run's value of every declared var into the slice, in closure mode too (listed under `vars
   written with the value run <id> used`), except the fresh vars a kept write interpolates; a
   `-var` still overrides, and when the verdicts then differ the verdict names each var that differs
   from the source run's. Values that differ every run are masked
   as `verify` masks them: an id- or timestamp-shaped got on both sides, or a message that
   differs only in such tokens, is the same failure, and so is a want, got or refusal message
   that differs only by a var's value echoed in it (`source want Customer customers-k1 got
   cust-customers-k1@example.test, slice want Customer sl-c1 got cust-sl-c1@example.test` with
   `tag` customers-k1 in the source run and sl-c1 in the slice). `-verify` is what needs `-run`;
   closure mode alone does not. With `-run latest`, `-verify` and `-mode pin` use the newest run
   that REACHED the target (its step passed or failed, or was sent and answered with an error,
   such as a token refused at authentication) and say on stderr when that is not the
   newest run; an explicit `-run <id>` that stopped before the target is refused, naming a run
   that reached it. When the source step was refused a token long before the expiry its login
   stated and the slice's younger token is accepted, NOT REPRODUCED says the refusal depends on
   how long the session had lived, which no slice carries, and points at the source chain instead
   of at dropped writes. It runs the slice `-repeat` times (default 3, `-repeat 1` for a single run)
   and never lets one run stand for the whole: a backend that fails a step one call in four
   (a flaky dependency, a counter, a race) otherwise gives `reproduced` once and `NOT REPRODUCED`
   twice for the same command. The verdict line counts the runs (`verify reproduced 3/3`) and a
   `runs:` line gives each slice run id and its outcome. When some runs reproduced and some did
   not, the verdict is `intermittent: reproduced k/N` (exit 4, recorded as `INTERMITTENT by
   'shrt chain slice -verify': ...`), with the details of a run that did not, and no `next:`
   (keeping more steps does not fix a flake). When no run reproduced, the verdict is that of the
   most telling run (NOT REPRODUCED over INCONCLUSIVE over DID NOT RUN). A var a kept write
   interpolates gets `-r2`, `-r3` appended on the later runs (`-var tag=s1` sends `s1`, `s1-r2`,
   `s1-r3`), so a repeat does not collide with the names the first run created. A `-mode pin`
   slice sent with `-resend-writes` runs once unless `-repeat` is given, since each run re-sends
   the write. Apart from `intermittent`, it prints one of four
   outcomes, each with its own exit code:
   - `reproduced` (0): the verdicts match and no write step the slice dropped changed an entity a
     kept step uses. Which entities a step uses is read from the source run: every id (a field named
     `id`, `id_*`, `*_id` or `*Id`) in a kept step's request or response. The entity a write acts on
     is the id its response returns for the object it answers with (`order.id_order` of a
     `ConfirmOrder`, `product.id_product` of a `CreateProduct`), or, when the response returns no
     such object (`AddStock`, `AddStockBatch`), every id its request names. A write that answered
     exactly as an earlier call of the same rpc did (an idempotent retry) changed nothing. A dropped
     write on another entity (a second product, an order the target never reads) is named as `info:`
     and does not make the match inconclusive. With `-write`, the verdict replaces the HYPOTHESIS
     paragraph in the written slice's `description:` (VERIFIED, both run ids, the date).
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
     re-run. When the step that stopped it (or the target) was refused as a uniqueness conflict,
     the verdict carries the diagnosis `run` and `verify` give: `fixture reused` naming the run (of
     this or another chain) that already sent the value, or `fixture collision`, or the chain
     colliding with itself, and the fresh `-var name=<value>` to re-run the slice with. A refusal before anything is sent exits 2 as well, without the verdict block: an
     unknown chain or step, no `-run`, a run that does not reach the step, a missing or not-fresh
     `-var name=<fresh>` (a `-var` equal to the source run's value for a var a kept write interpolates
     is not fresh). Only a flag that cannot be parsed exits 1.
   - `INCONCLUSIVE` (3), also when the source run was recorded against another target than the
     config's: the line says `the source run was recorded against <A>, this target is <B>`. Under
     `-mode pin` nothing is sent (its ids were minted there); in closure mode a different verdict
     can come from the target. Run the chain here and slice from that run (`-run latest`).
     Otherwise: the verdicts match, but the slice dropped write steps that act on entities the
     kept steps use (a confirm of the order the target cancels, stock added to the product its
     line holds, an order created for the customer a `ListOrders` target lists: a write whose
     request carries an id the kept step's request carries and whose created entity is the kind
     of item the kept step's response lists or asserts), or whose entity cannot be told (a write
     whose request and response carry no id). A write whose contract `needs` another write, and
     whose request names an existing record (a confirm naming its order), acts on the entities that
     record holds too (the products on the order's lines), so it is never listed as changing no
     entity a kept step uses. A match can come from state the slice never built,
     so it is not a receipt. Nor is a match on a failing expectation that compares with what a
     dropped write created in the source run (`orders.1.id_order equals` the id a dropped
     `CreateOrder` returned, under `-mode pin`): the slice never creates that item, so a correct
     backend fails it the same way, and the verdict is INCONCLUSIVE naming the write, whatever
     else was kept. A dropped write on a kept entity does NOT block the receipt when the contracts
     say no later kept step reads what it changed: the fields it set (`qty_on_hand` for an
     `AddStockBatch`) are in no request or response message of those steps, each has a contract,
     and none `needs`/`before` the write's service or names it (or the field) in a failure. The
     verdict then says so on an `info:` line. With no contract for a reader the write stays
     suggested. The output
     ends with a `next:` line — `shrt chain slice <src> -step <t> -run <source-run> -keep
     <those writes> -verify -write` — naming only those writes, so the `-keep` set stays minimal:
     a write on another entity is never suggested, however many there are. NOT REPRODUCED
     suggests the same set first, and `-keep writes` only once no dropped write acts on a kept
     entity. `-keep writes` keeps every write step before the target except one the source run
     shows refused (it wrote nothing), and combines with ids (`-keep writes,<read id>`). A var
     interpolated into a name is printed as `<fresh>`: the run above already used its value, so
     give a new one. A kept step that the backend ANSWERED but that failed an expectation in the
     source run (a `-keep-going` run) took effect: its write happened. The slice keeps it and drops
     only the expectations that failed there, and says so under `relaxed:` and in its description,
     so it reaches the target; the call must still be answered. A dropped write whose step errored
     or was never answered in the source run is left out of `next:` and named with its status:
     keeping it would stop the slice there, before the target, so the command could only return DID
     NOT RUN. The same holds for an id you passed with `-keep`. When every dropped write it would
     need is such a step there is no `next:` line, and the output says why; nor is there one when
     the slice the command would build keeps, through the references of what it keeps (a
     `ConfirmOrder` needs its `CreateOrder`), a step that errored or was not sent in the source run:
     `next:` is checked against its own slice, so it never names a command that stops before the target. When a slice stops
     before the target at a kept step that passed in the source run (a `ConfirmOrder` refused for
     stock a dropped `AddStockBatch` added), DID NOT RUN names the dropped writes on the entities
     the kept steps use and gives the `next:` command that keeps them. A minimal chain you write by hand (only the steps the defect needs, its own
     writes included) proves something narrower: run it, then verify the target on THAT chain with
     `shrt chain slice <minimal> -step <t> -run latest -keep writes -verify -write`. A plain
     `slice -verify` of it is not enough: closure still drops every write nothing references (an
     order created only so a list has something to filter), so it returns NOT REPRODUCED or
     INCONCLUSIVE. With `-keep writes` nothing that wrote state is dropped, so the verdict can be
     `reproduced`, and since that slice keeps every step, `-write` records it in the chain itself
     as `RE-RUN ... own run`: the minimal chain reproduces its OWN failure, by itself and again. It is
     not a receipt against the source run; nothing in it says the source chain failed the same way.
     To also keep that receipt, make the minimal chain out of the source chain's own steps and
     verify the source with them kept:
     `shrt chain slice <src> -step <t> -run <source-run> -keep <ids of the minimal chain> -verify -write <name>`.
     When that slice keeps no other write it can return `reproduced`, and its description records
     `VERIFIED ... source run <source-run>`: that file is the minimal receipt. A hand-written step the
     source chain does not have cannot be verified against its run; keep the two run ids (source and
     minimal) in the minimal chain's description, and say that it was checked by hand.
   Until you have a verdict, the slice is a hypothesis, and every slice prints a line saying so;
   when `-verify` reaches one (anything but DID NOT RUN), the verdict replaces that line, in the
   output and in the written slice's description: `VERIFIED`, `NOT REPRODUCED` or `INCONCLUSIVE by
   'shrt chain slice -verify': ...`, never next to the hypothesis paragraph. Re-verifying a slice
   file that keeps every step records a `RE-RUN by ...` line against its own run, which replaces the
   hypothesis paragraph and any earlier `RE-RUN` line, whatever the new verdict.
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
   nothing. `-keep <id>` keeps it anyway. The same holds for the target itself: a kept write
   whose body reads a pinned value (`ConfirmOrder` on the pinned `id_order`) acts on what the
   source run created and already changed, so re-sending it changes live data and answers for a
   second write (`OrderAlreadyConfirmed`). The printed slice says so in a `WARNING`, and
   `-verify` refuses before sending (exit 2), naming the closure command
   (`-keep writes -run <id> -var tag=<fresh> -verify`); `-resend-writes` sends it anyway. A var the kept steps read that the
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
   it too), and the slice output names the var. A written slice declares such a var with the
   `-var` value it was given (`-verify -var tag=slice2` writes `tag: slice2`), not the chain's default,
   which the chain's own runs already created with; that value is used too once `-verify` sent it, so
   every later run of the slice still passes a fresh `-var`. Pin mode keeps the run's value when no kept
   write interpolates the var, and when a kept write sends exactly a string a dropped step before
   the target sent (it names what that step created, not something new). A string that also
   carries `${uuid}` or `${now}` is fresh on every run and does not count.
   Pin mode needs a run record and refuses without `-run` rather than quietly falling back to
   closure.
6. **A pinned slice reproduces one incident, not the flow.** Its ids are the ids of that run, so it
   is dead the moment that data is. Confirm a safe spot from a closure slice, never a pinned one.
