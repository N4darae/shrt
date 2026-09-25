# shrt docs

The distilled core of shrt: what an agent needs to **compose an RPC chain and a contract**, and
nothing else.

`.claude/skills/shrt/SKILL.md` is the skill Claude Code **loads automatically**; it is
the overview and it routes here. These four files are the working surface, each with one job. In
the shrt module they sit at the root; `shrt init` installs them into `.shrt/docs/` of the repo that
adopts shrt:

| file | job | hand-written? |
|---|---|---|
| `README.md` | route to the right thing; the loop; the rules that are never negotiable | yes |
| `GRAMMAR.md` | every key shrt accepts, with its type — **and nothing it does not accept** | **generated** |
| `PLAYBOOK.md` | the procedures: compose a chain, author a contract, probe a failure, assert an invariant | yes |
| `PITFALLS.md` | symptom → cause → fix, each entry tied to the receipt that taught it | yes |

`GRAMMAR.md` is produced by `go run ./distill` in the shrt module, which reflects over the Go
structs and **exercises** the resolver and the expectation rules rather than describing them.
`go run ./distill -check` fails when a key exists in code and not in `GRAMMAR.md`; nothing in the
module runs it automatically, so run it before committing a change to a key. That is the point: an
agent's characteristic failure is inventing a plausible key, and until 2026-09-11 both the loader
and `lint` accepted invented keys in silence.

**About the paths cited in these four files.** A package path such as `contract/plan.go` or
`runner/runner.go` is a file in the shrt module, `github.com/N4darae/shrt`: open it in a clone of the
module or in your Go module cache. A `scripts/...`, `internal/...` or `docs/...` path is **not**:
those belong to the development repo shrt grew up in, and exist neither in the module nor in your
repo. They are cited as provenance — how a rule came to be known — never as commands for you to run.

---

## Before the first command

`shrt init` adds `.shrt/descriptor.binpb`, `.shrt/docs/`, `.shrt/runs/`, `.shrt/tokens.json` and
`.shrt/safespots/pending/` (safe-spot proposals, per-machine review material) to `.gitignore`, so a fresh clone has no descriptor and no installed docs. The binary is not in the repo
at all. Build the descriptor from the repo root, wherever that clone lives — `shrt init` copies these
files into every repo that adopts shrt, so nothing here may assume one machine's path:

```bash
cd "$(git rev-parse --show-toplevel)"
shrt init -agents=false -build=false # only if .shrt/docs/ is missing, as on a fresh clone
shrt catalog build                   # the descriptor; without it every catalog command fails
shrt doctor                          # is this installation sound?
shrt catalog ls -filter <word>
```

`.shrt/docs/` is gitignored build output too, so on a fresh clone `doctor` FAILs with `.shrt/docs/
is missing README.md, GRAMMAR.md, PLAYBOOK.md, PITFALLS.md` until the first line writes them from
the copy embedded in the binary. It touches nothing that already exists, though on a clone missing
them it can also add `.gitignore` entries or a `.shrt/config.yaml`.

**`shrt doctor` is the check to run before you trust a green.** The four files you are reading are
installed build output, copied out of the binary by `shrt init`; upgrade the binary and the copy
stays where it was, so the rules you are reading can be older than the rules being enforced.
Nothing else notices that. `doctor` compares them, and the agent kit `init` installed under
`.claude/` (the skill and the contract-author subagent), and in the same pass reports which build of
shrt is running, checks the descriptor against a rebuild, the paths that must never be committed
against `.gitignore`, the token cache's mode, the `auth:` profiles (a literal credential, a body
reference it cannot resolve, an environment variable that is not exported), and whether
`conventions.envelope_path` and `item_envelope_path` fit the response messages. It exits non-zero
on a failure, prints the remedy under each finding, and `-strict` makes the warnings count too.

`shrt` comes from `go install github.com/N4darae/shrt/cmd/shrt@latest`. **If you are changing
shrt itself**, build it from a clone of the module instead — `go build -o shrt ./cmd/shrt` — and run
that binary in a repo that has a `.shrt/config.yaml`; the module has none, so its commands fail
inside the module's own clone. Rebuild it after any change to the shrt source: a stale binary lints
with the OLD rules and tells you everything is fine. Rebuild the descriptor after any proto change; that one
fails quietly rather than loudly, which is `PITFALLS.md` §2.

Everything works offline except the commands that send traffic: `shrt run` (not with `-dry-run`),
`shrt verify` (not with `-run <id>`, which re-diffs a recorded run), and `shrt chain slice -verify`.
Those need a backend and a credential.

**`shrt init` GUESSES the `auth:` block from the descriptor** — it looks for an rpc that returns a
token and takes a credential, and writes one, plus a named profile for each additional login in
another domain. Two roles that log in through the SAME rpc (an admin and a clerk) get a second
profile only when init can see the second credential set: a pair of environment variables
`<ROLE>_USER` (or `_USERNAME`) and `<ROLE>_PASSWORD` (or `_PASS`) exported when init runs, whose
`<role>` the repo's README names (`CLERK_USER`/`CLERK_PASSWORD` and a README that says `clerk`
give profile `clerk`; `DB_USER` with no `db` in the README gives nothing). Otherwise init prints
how to add one per role, naming the accounts a README table lists. It says so when it does, because
a guess is not a fact: **check the call it picked** before the first run. If nothing looked like a login it writes no block at all and says that too;
`GRAMMAR.md` §4 is the key table for writing one by hand.

Either way, `auth.body` names the environment variables the login reads. **Read those names out of
`.shrt/config.yaml` rather than assuming them** — they differ per repo — and export them before the
run. Without them `shrt run` refuses the chain **before sending anything**: it exits 1, writes no
run record, and says `step "<id>" (step N) runs under auth profile "<profile>", whose login body
reads ${env.NAME}, and env NAME is not set, so nothing was sent`. That is a fixture problem, not a
backend one.

If the target is a remote box whose credential must not be copied around, run shrt on that box
instead of forwarding the secret; where that is written down is this repo's business, not the
kit's.

## Commands

| command | does |
|---|---|
| `shrt init` | write `.shrt/`, build the descriptor, install the Claude skill and subagent |
| `shrt version` | which build this is — version, commit, build time, and the four docs it carries; `-short` prints the version alone |
| `shrt doctor` | check this repo's own `.shrt/`: which build is running, installed docs and the `.claude/` agent kit against the copy embedded in the binary, descriptor against a rebuild, `.gitignore` against the paths that must never be committed, the token cache's mode and how many of its entries serve this target's logins (apart from those minted against another `base_url` or for another login), the auth profiles and every `${env.*}` they read, the envelope conventions against the response messages, and the contract overlays (FAIL if one does not load or two define the same rpc, WARN for overlay files in a subdirectory, which are not loaded, and for an entry naming an rpc the descriptor no longer has, which `contract lint` calls an error), every safe spot against the digest sealed at its approval (FAIL for one that does not load or was edited after it was approved, which `verify` refuses), every safe spot against its chain file (WARN for a safe spot whose chain was deleted or renamed, which `verify` and a gate looping over safe spots fail on with `chain "x" not found`, naming a chain with the same steps and the fix; among several with the same step ids and calls, the one with the same expectations, then the one with no safe spot of its own, and otherwise all of them), a chain file whose `name:` differs from its file name (WARN, as `chain lint` warns and `chain ls` notes: `verify <file name>` and `verify <name>` both verify it against its `name:`'s safe spot, so a safe spot left under the file name is an orphan; when another file also declares that name, or has that name as its file name, the WARN says so instead, since `run`, `verify` and `confirm` then refuse the name, naming both files), and, as `upgrade` WARNs each with its fix, what an older build or another target left behind: unsealed run records (re-run), safe spots with no `auth_principal` (`confirm -supersede`), and run records, safe spots and cached tokens from another `base_url`. `-strict` fails on warnings too, so a committed pre-upgrade safe spot fails a `doctor -strict` gate until it is superseded. `-json` prints `{"Root": ..., "Findings": [{"Check", "Level", "Detail", "Remedy"}]}`, with `Level` the same word as the text output: `ok`, `WARN` or `FAIL` |
| `shrt catalog build` | rebuild the descriptor after a proto change |
| `shrt catalog ls [-filter x]` | list the RPC surface; an rpc the proto marks `option deprecated = true` (or whose service it marks) reads `[DEPRECATED ...]` |
| `shrt catalog describe <rpc>` | request and response schemas with proto doc comments; a deprecated rpc says `DEPRECATED:` and a deprecated field reads `DEPRECATED`, and `chain lint` warns (kind `deprecated`) on a step calling a deprecated rpc, sending a deprecated body field or asserting a deprecated response field |
| `shrt contract init <domain>` | scaffold the curated contract; re-running keeps what you wrote, and leaves an overlay alone (`unchanged`) when its content would not change, whatever its YAML layout; when it does write (an rpc to scaffold, or a request field the proto gained that an existing entry does not document yet, added to that entry's `fields:` as a TODO), the entries already in the file keep their order, their comments and their flow- or block-style lists |
| `shrt contract show <rpc>...` | generated schema — example body, paste-ready step YAML, exportable paths — plus the curated semantics. `-json` for tooling, `-filter <word>` for every rpc whose name contains the word |
| `shrt contract lint` | validate contracts against the descriptor |
| `shrt contract plan <rpc>[@alias]...` | compose one ordered chain reaching every target from the dependency graph, references pre-wired to the producing step's response (`${create_order.order.id_order}`), so it writes no `export:` a step does not read; a repeated message field in a request is scaffolded with two items with different values, so per-item logic is exercised, and when the first item reads a resource a step of the chain creates, the second reads a second such step (`create_product_2`, with its own unique values and its own preparation, such as `add_stock_2`), so the two items point at different resources; a target whose contract declares a uniqueness refusal (`EmailTaken`, `SkuTaken`) gets a step sending the same value again, expecting that refusal, a step sending it with every other field changed (`create_product_same_sku_other_fields`: another name, another price), plus the value in another letter case when the contract says case is ignored and padded with spaces when it says whitespace is trimmed; a target whose contract declares a shortage refusal (`InsufficientStock`, a `when:` saying more than, exceeds, not enough) gets a refused attempt asking for 100000 on the first item and, in a second probe, on the last (on its own quantity field, or on a copy of the step it reads, `create_order_for_insufficient_stock`), with a read of every entity it touches before and after it asserting the numeric and enum fields unchanged (`get_product_after_confirm_order_insufficient_stock`); when the config declares auth, a target whose contract has `requires_role` gets `<step>_as_<profile>` for each profile whose name is not a required role, expecting the declared denial, and the plan's first target gets one `<step>_without_token` (`skip_auth`) and one `<step>_with_bad_token` (`auth: invalid`) expecting the declared unauthenticated failure, all between reads proving a write changed nothing; numbers vary in magnitude, not by one: a second producer's price is 1000 above the first (`create_product_2` at 1250), three list fixtures take 250, 1250 and 12345 (a quantity, `qty`, moves by one instead: up for a sort key, down where it can inside repeated items, so it stays within stock), every write asserts each numeric field it sent that its response carries back (`product.price_minor equals ${steps.create_product.request.price_minor}`), and a target gets `<step>_<field>_large` (12345, not for quantities) and, where a failure's `when:` or the field's note states a minimum (`zero or negative`, `at least 5`), `<step>_<field>_min` (accepted) and `<step>_<field>_below_min` (refused, between reads proving nothing changed); a list target scoped by a parent (`ListOrders` by `id_customer`) gets another parent with an item of its own (`create_customer_other`, `create_order_other_customer`), one scoped by a prefix gets an item containing the prefix not at the start (`create_product_prefix_inside`) and, when a contract says the field is case-sensitive, one starting with it in another case (`create_product_prefix_case`), none of which the list may show (it asserts its exact count); and when the list request has an enum filter matching an enum of its items (`status`), the fixtures are moved into different states after the list by the writes whose contract takes their id and names the state they leave (`cancel_order_2`, `confirm_order_3`), and one filtered list per reachable state asserts only the matching fixtures (`list_orders_confirmed`), with a note naming states no write reaches; with `conventions.item_envelope_path` set, a batch target whose contract says failures are reported per item (`on that line only`, `independently`) gets `<step>_partial`: three items, the middle one below a minimum a failure states (`qty` 0), asserting each item's verdict and code, followed by reads asserting that what each applied item reported (`results.2.qty_on_hand`) is what is stored; a create target with an idempotency key field (`idempotency_key`, `request_id`, `dedup…`) gets `<step>_replay` (same key, same body: the same id back), `<step>_replay_other_body` (same key, numbers raised by one: the first object back unchanged, or the declared conflict failure) and, when the key is not required, `<step>_no_key` and `<step>_no_key_2` (no key: two different ids) |
| `shrt contract status [-gaps]` | contract-entry coverage per domain (how many rpcs have a curated contract, not how much the chains exercise); `-gaps` lists each rpc with no contract ('no contract') or in no multi-step plan ('no path to'; a login the config's auth calls needs no path and is left out), each repeated message field of a request that chains send but never with two or more items ('one item'), or send with two or more items that all point at the same resource, the same `${step...}` reference or literal id ('same resource'), each unary rpc no chain calls, with the repeated message fields it takes ('no chain'; a login the config's auth calls counts as called), each role-gated rpc (`requires_role`) a chain calls but never as an auth profile whose name is not the role ('no role probe'), each chained rpc no chain calls with `skip_auth: true` or `auth: invalid` ('no token'), then streaming rpcs |
| `shrt contract quality [-domain d]` | score each contract against the curation terms, and name what is missing; an rpc in the catalog with no contract in any overlay is charged too, so deleting an overlay makes `-gate` fail |
| `shrt chain new -name <c> <rpc>...` | scaffold a chain from real proto fields |
| `shrt chain lint [<c>]` | static validation against the catalog, one status per chain: `ok`, `warn` (warnings only; exit 0 unless `-strict` promotes one) or `FAIL` (an error), its issues listed under it; `-strict` turns the assertion-quality warnings into errors (an assertion that cannot fail that is reported as a warning, `unfailable-assertion`; a step asserting nothing, `asserts-nothing`; an `allow_fail` that does nothing, `inert-allow-fail`; an export a later step silently overwrites, `export-overwritten`; arithmetic such as `${a.qty}+${b.qty}` in an `equals` on a numeric field, which is compared as text and never computed, `interpolated-arithmetic`; and a step expecting success that asserts only the verdict although its rpc's contract declares response facts, which is what every step `contract plan` writes starts as, `envelope-only`), which is the form a CI gate should run. Other warnings, such as the `-var`s and environment a run needs (including the env vars the login body of each auth profile the chain's steps run under reads), and a timestamp field of a step expecting success that no expectation reads (`unasserted-timestamp`: verify masks it, so a range such as `within: {of: "${nowunix+3600}", by: 5}` is its only check), and a step asserting the positions of list items that two or more candidate sort keys order alike (`indistinct-order`: fixtures whose names sort like their skus pass a backend sorting by name), are not promoted. An expect path that can never match, `exists: false` on a path the message has no field for, an export reading a field the response does not have (the run fails that step), and a reference to a field an earlier step's response does not have, or to a `request.` path its request message does not declare, and a whole-value reference whose declared type cannot fill the numeric field it is sent in, such as a timestamp (the run refuses the chain), are errors with or without `-strict` |
| `shrt chain ls` | one line per chain, marking which have a safe spot (`*`), a pending proposal (`?`) and which are kept red (`R`); `-long` for full descriptions |
| `shrt chain which [-rpc <rpc>] [-code <n>]` | which chains exercise an rpc or assert a failure code, marking each step `OBSERVED` when a local run record reached it, citing the newest such run and what it got even when that contradicts the assertion, and printing the `chain slice` command that reproduces the best match. Under `-code`, when no chain asserts the code but a local run record carried it, it lists those steps with a reproduce command instead of failing |
| `shrt chain slice <c> -step <id>` | the minimal ordered sub-chain that reproduces one step; `-write [name]` it (`-force` to replace another chain), `-mode pin -run <id>` to pin values from a run instead of rebuilding their producers (a kept write that would act on an entity the run created, a confirm of its order, is warned about, and `-verify` refuses to send it without `-resend-writes`: reproduce a write in closure mode with `-keep writes`), `-keep <id,…>` to force earlier steps back in (`-keep writes` for every earlier write step the source run did not show refused, combinable with ids), `-var k=v` to supply a var the chain does not declare, `-verify -run <id>` to prove the slice still fails the same way (`-build <id>` stamps that run, and the recorded verdict names the build), comparing also the refusal's message, reason and app_code, a transport refusal, and the value a failing expectation got (ids and timestamps masked as `verify` masks them) (the slice's run record is kept only with `-write`). `-run latest` with `-mode pin` or `-verify` uses the newest run that reached the step; when the slice keeps every step, `-write` records the verdict in that chain instead of writing a copy |
| `shrt chain hollow` | read steps that passed while the response carried nothing, counted over every run record of the chains under `paths.chains` (`shrt run` records and `verify` replays, runs of chains kept red and failed runs alike), and the output splits the record count that way |
| `shrt run <c>` | execute in order and record |
| `shrt confirm <c> -note "..."` | propose a passing run as the safe spot; prints the summary table to show the user and writes a full report. It writes no safe spot; a second proposal for the chain replaces the pending one and says `replaces pending proposal <run id>` |
| `shrt confirm <c> -approve -by <user email>` | write the safe spot, only after the user said yes to that proposal in the conversation; refused when the chain file has another `chain_digest` than the proposed run recorded (it names each difference the run record shows); `-reject` discards it, `-pending` lists proposals |
| `shrt confirm <new> -rename-from <old> -by <user email>` | carry an approved safe spot across a pure chain rename: refused unless `<old>`'s chain file is gone, `<new>` has no safe spot and is identical, apart from its name, to the chain file the approved run ran (the safe spot's `chain_digest`; a safe spot without one is refused); the approval is kept and the rename recorded |
| `shrt verify <c>` | replay and diff against the safe spot (`-run <id>` re-diffs a recorded run instead; `-run latest` takes the newest run record of the chain, a verify replay included, and names the run it picked), masking volatile paths and id- or timestamp-shaped values, and counting both. A replay against another target than the safe spot's is said first (`targets differ: ...`). A volatile pattern the safe spot did not approve (added to the config or chain after approval) fails it, naming what the pattern hid (a value that only echoes a fixture name or a renamed id is not listed, since verify masks it anyway; a stale echo, a value still holding the confirmed run's fixture name although this run sent another, is listed, since without the pattern it is a change); `-masked` lists every masked value, volatile and id- or timestamp-shaped alike, with its path and both values. A step not judged because its response, or one it reads, does not match the descriptor is folded into one `not_judged` line with the rebuild hint; `-v` lists its changes. It replays as `-keep-going` does, so every step a failure does not block is compared; a step held back behind a failure is reported `not_reached`, not as a change of length, and the report names the first failing step. A `not_reached` step is listed but is not a change: it is left out of the change count and the verdict, live and under `-run` alike (a run recorded without `-keep-going` stops at its first failure, and the steps after it are `not_reached`), and the header says how many were left out |
| `shrt diff [<c>] <run-a> <run-b>` | compare two recorded runs of one chain step by step — status changes, where the first failure moved, steps no longer reached, response fields — with declared volatile paths, ids and timestamps masked. Needs no safe spot, and is a comparison between two runs, not a verdict. `shrt diff <c>` compares the two latest runs that are not `shrt verify` replays (a replay records `replay_of`), and says which two it picked, so a gate's own run is not compared with that gate's replay against the same backend; `latest` and `latest~N` count replays too. The header names each side with the selector given and its run id (`run A = latest (<id>, passed) vs run B = latest~1 (<id>, passed)`) and which was recorded first, so `a=`/`b=` in the lines below read unambiguously. A response field one record declares and the other lacks because the backend never sent it (the proto3 default, the same bytes) is not a difference, by the rule `verify` applies, and is named on its own line |

### Exit codes

A gate reads these, so they are part of the interface. Any command also exits 2 for an unknown
command or group subcommand, 1 for a flag it cannot parse or a setup it cannot load (no
`.shrt/config.yaml`, a config that does not parse, a missing descriptor, and, in every command that
reads contracts, an overlay under `paths.contracts` that does not parse, named with its parse
error), and 0 for `-h`.

| command | 0 | 1 | 2 | 3 |
|---|---|---|---|---|
| `run` | passed; a `-dry-run` resolved and validated; a chain with `kept_red` failed exactly as it pins | `failed`: an expectation did not hold (a server error at a step whose request the backend answered elsewhere in the run, or which the previous run answered while failing at another step with the same error, prints `FINDING: intermittent failure at <rpc>` with the evidence) (for a chain with `kept_red`: it failed anywhere else or differently than pinned, or passed, so the pinned defect is gone); also a refusal before anything was sent (unknown chain, a `-var` it never reads, a missing var, an unset env var read by a step or by the login body of an auth profile a step runs under, or a reference to a step or export that does not exist or runs later, or to a response field the producing step's message does not declare or a request path its request does not declare, a reference whose declared type cannot fill the numeric field it is sent in (a bool, enum, bytes or timestamp into an int64; a string may hold digits and is only a lint warning), a whole message into a string, bytes, bool, enum or numeric field (`name: ${p.product}`, `${p.status}`, `${o.order.lines.0}`, or an export of a message; a Timestamp, Duration, FieldMask or wrapper renders as one JSON value and passes), or a whole list or map into a single-valued field or a single value into a list or map (`qty: ${o.order.lines}`), an unknown auth profile, an rpc the catalog does not have, a streaming rpc, a conventions path no response declares, a step body the proto rejects, checked for every step up front as `-dry-run` does) | — | `error`: a step could not complete (unresolved reference, a body that is only invalid with the values a real response gave, login failed, target unreachable, the connection closed before a response because the backend stopped or crashed, or a gateway answered for the service: a Connect `unavailable`, or HTTP 502/503/504 without a Connect body, the normal answer during a rolling restart), so the run is not a verdict about the backend; a step refused a token the backend had accepted earlier in the run says it likely restarted mid-run; a step refused a token a login in this run had just issued, on its first use, says instead that this may be an auth regression, since the credentials work; refused at the same step the same way in the previous run that sent it, it exits 1 as a finding. A token accepted earlier and refused at the same step as in the previous run that sent it exits 1 as a finding: a refusal specific to that rpc, not a restart, unless either run shows a restart (a call refused at authentication and accepted when re-sent after a fresh login, the refused rpc accepting a later call after it, data created before the refusal gone after the re-login: a later step reading it refused naming its id or as not found when it did not expect that, a list shorter than the same call before the refusal, a uniqueness conflict it expected accepted instead; or a step before the refusal that got no answer from the service), which keeps it exit 3; "the same way" means the same HTTP status and code, so an in-band refusal and a 401 do not repeat each other |
| `verify` | no drift and the replay passed | drift vs the safe spot, the replay did not pass, or no safe spot; `FINDING: intermittent failure at <rpc>` when every change is at a step that failed with a server error (Connect `internal`, `unknown`, `resource_exhausted`, `data_loss`, `aborted`, `deadline_exceeded`, or another HTTP 5xx with a Connect body, not `unavailable`) whose request the backend answered at another step of this run, or which the previous run of the chain answered while failing at a different step with the same error (the backend does fail, some of the time; a failure only the previous run that sent the step had answered stays a regression with a `looks intermittent` note) | — | could not verify: a step never got an answer (target unreachable, the connection dropped, sent but no answer before `target.timeout`, a Connect `unavailable` or a bare HTTP 502/503/504 from a gateway, login or auth refused) and nothing drifted before it; a change at or after that step is not judged (a dropped connection or a timeout at the same step in this run and the previous run that sent it, with later steps answered in both, is exit 1 instead, even when that rpc answered other steps: `FINDING: ... the backend fails this rpc every time while answering others`, or `... fails this step's request every time while answering other calls` naming the step and its request when the rpc answered other steps; it stays exit 3, and looks intermittent, when the previous run had that step answered); when no step got an answer at all it prints only why, with no change count or step status (a step a gateway answered for got no answer from the service, so it counts neither there nor among the steps after the first unanswered one that were compared); every could-not-verify verdict leads with its verdict line and lists the affected steps in one line instead of the drift dump; or `fixture reused`: the first failing step was refused as a uniqueness conflict on a field built from a var value a recorded run of the chain already used in that step without being refused, or sent there with no answer (whether it took effect is unknown), so re-run with a fresh `-var`; (also when a recorded run of another chain of the repo sent that value in a step it answered, naming that chain and run); or `fixture collision`: the same refusal when no recorded run of any chain used that value (another client or a shared backend created the record), with the same fresh `-var` hint (when the value is built only from `${uuid}` or a clock value, with no var to pass, it says instead that a plain re-run generates a fresh value), and when the previous run of the chain that sent that step was refused there the same way with a different value no recorded run had created, it says a repeat with fresh values points at the backend unless another client uses the same values, still exit 3; only when the conflicting field is built from `${uuid}` or a clock value, unique to its run, does such a repeat exit 1 with `FINDING:` naming both values; or the backend refused a token a login in this run had just issued, on its first use (the credentials work; a read re-sent after a fresh login and refused again counts, even when the old token had been accepted earlier, and is not called a restart): it says this may be an auth regression instead of blaming the credentials, and when the call was re-sent after a fresh login and the previous run that sent that step was refused there the same way, re-sent too, it exits 1 with `FINDING: auth refused at <rpc>` (a write refused with a just-issued token is not re-sent, so a restart between the login and the call explains it and a repeat stays exit 3); or the backend refused a token it had accepted earlier in the run (a `WARNING` line names the step): it likely restarted mid-run, so changes at or after that step are not judged; re-run; every could-not-verify verdict leads with its verdict line and lists the affected steps; or, with `validate_output` on, the first failing step drifted from a stale descriptor or by fields the proto does not declare (a wrong-typed value or an undeclared enum value against a descriptor that matches a rebuild is a regression instead, exit 1) |
| `confirm` | proposal written, approved, rejected or listed; a safe spot carried across a rename | refused (no passing run, no `-note`, no `-by`, nothing pending, a `-rename-from` that is not a pure rename) | — | — |
| `chain slice` (no `-verify`) | the slice was printed or written | a refusal: an unknown chain or step, `-mode pin` without `-run`, an unknown run, a slice file `-write` would overwrite. The same refusal exits 2 under `-verify`, where 1 would read as `NOT REPRODUCED` | — | — |
| `chain slice -verify` | `reproduced` | `NOT REPRODUCED` | `DID NOT RUN`; also a refusal before anything was sent (an unknown chain or step, no `-run`, a run that does not reach the step, a missing or not-fresh `-var name=<fresh>`, a `-mode pin` slice that would re-send a write on an entity the source run created, without `-resend-writes`), so nothing was verified | `INCONCLUSIVE`; also a source run recorded against another target. Exit 4 is `intermittent: reproduced k/N`: `-verify` runs the slice `-repeat` times (default 3) and only some runs matched |
| `diff` | the two runs do not differ | they differ | could not compare (unknown run, runs of two chains, the wrong number of arguments) | — |
| `chain hollow` | no unexplained hollow read; under `-gate`, at the baseline | hollow reads reported; under `-gate`, worse or better than the baseline | no run records to read | — |
| `chain which` | a chain or run record matched | nothing matched, or bad flags | — | — |
| `doctor` | no FAIL (warnings allowed) | a FAIL, or a warning under `-strict` | — | — |
| `contract lint` | no contract error (warnings allowed) | a contract error, an overlay that does not parse, or no overlay to check | — | — |
| `contract plan` | the plan was printed, or with `-write` written, including one whose required fields still have no usable value (a note names each) | nothing planned or written: no rpc named, an unknown rpc or alias, a streaming rpc in the graph, a dependency cycle, or with `-write` an existing chain file and no `-force` | — | — |
| `contract quality -gate` | the score equals the baseline | the score is worse than the baseline, better without the baseline being lowered, or the baseline file is missing | — | — |

### CI gate

In this order, with every env var the `auth:` bodies read exported first (a missing one makes
`doctor -strict` warn and `run` refuse). The static checks stop the gate at the first failure; the
runs, replays and the hollow ratchet each have their exit checked, so one red chain does not hide
the rest, and the gate fails at the end if any of them did:

```bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
[ -f .shrt/docs/GRAMMAR.md ] || shrt init -agents=false -build=false
shrt catalog build
shrt doctor -strict
shrt contract lint
shrt contract quality -gate -baseline .shrt/quality-baseline
shrt chain lint -strict
tag="ci$(date +%s)$RANDOM"
fail=0
unverified=0
shopt -s nullglob
check() {
  local what="$1" c="$2" file="$3" prefix="$4" try rc args
  for try in 1 2; do
    args=(-quiet)
    if grep -qs 'vars\.tag' "$file"; then args+=(-var "tag=$tag-$prefix$c-$try"); fi
    rc=0; shrt "$what" "$c" "${args[@]}" || rc=$?
    if [ "$rc" -ne 3 ] || [ "$try" -eq 2 ]; then break; fi
    echo "gate: $what $c: no verdict (exit 3); retrying once in 20s" >&2
    sleep 20
  done
  case "$rc" in
    0) ;;
    3) echo "gate: could not verify $c: $what exited 3 twice (backend unreachable, restarting or refusing auth); not a regression" >&2
       unverified=1 ;;
    *) echo "gate: $what $c exited $rc" >&2; fail=1 ;;
  esac
}
for f in .shrt/chains/*.yaml; do
  check run "$(basename "$f" .yaml)" "$f" ""
done
for s in .shrt/safespots/*.json; do
  c="$(basename "$s" .json)"
  check verify "$c" ".shrt/chains/$c.yaml" "v-"
done
rc=0; shrt chain hollow -gate -baseline .shrt/hollow-baseline || rc=$?
[ "$rc" -eq 0 ] || { echo "gate: chain hollow -gate exited $rc" >&2; fail=1; }
if [ "$fail" -ne 0 ]; then exit 1; fi
if [ "$unverified" -ne 0 ]; then echo "gate: could not verify every chain; re-run the gate once the backend is up" >&2; exit 3; fi
```

Exit 3 from `run` or `verify` is not a verdict: the backend was unreachable, a gateway answered for
it, it restarted mid-run, or it refused authentication, and the output says which, followed by
`re-run`. A rolling restart does that routinely, so the gate retries such a chain once, after a
short wait and with a fresh tag (the first attempt may have created some of its fixtures), and never
retries exit 1 or 2. Still 3 on the retry, it prints `gate: could not verify <chain>: ...`
instead of a failure line, and the gate exits 3 when nothing else failed (1 when something did, so a
regression is never reported as merely unverified). Treat 3 in CI as "no verdict": re-run the job
once the backend is up, or retry it automatically; do not mark the change red, and do not count it
as green either.

A `run` in the gate stops at its chain's first failure, so under `-quiet` a red chain shows that
one failing step, and the steps after it were never sent: they may pass or fail. The run says so
on a line of its own, `N later step(s) were not run (...)`, naming them. Treat the first failure as
a lower bound on the blast radius, and re-run the chain with `-keep-going` to see which of the later
steps fail too. A `verify` in the gate replays as `-keep-going` does and lists every step that did
not pass, and so does a chain with `kept_red`.

Each run and each replay gets a fresh tag, or the second CI run of a chain trips its own
uniqueness constraints; the tag goes only to a chain that reads `${vars.tag}`, since `run` refuses
a `-var` the chain never reads. Build the tag INTO other text (`sku: sku-${vars.tag}`, or a header value `X-Tag: t-${vars.tag}`): verify
treats a var inside other text, in a body field or a header alike, as a fixture name and does not count a new one as a change, but a
field that is `${vars.tag}` alone is input, so the fresh tag makes every CI `verify` of that chain
fail with `drift with different input`; `chain lint` warns on such a field. Every run must exit 0 (3 is retried once, as above),
and so must every `verify`. A chain kept
red on purpose, pinning a known defect, declares WHERE and HOW it fails with `kept_red` (GRAMMAR
§1): its run goes past every failure, pinned or not, as `-keep-going` would, with no flag, so every
pin and every later step is evaluated even after an unpinned failure (a step that reads a failed
step is not sent, and says so); it exits 0 only when it fails exactly there, and exits 1 when it
fails anywhere else, fails differently, leaves a step unsent, or passes (the defect is gone), so a
regression in an earlier or a later step of that chain fails the gate instead of hiding behind the
known red; the run names that regression under its verdict line and in its exit message, e.g.
`NEW FAILURE outside the pinned defect: create_order order.total_minor want=1250 got=500` (with the
step lines shown, no `-quiet`, the verdict line names only `create_order`, whose own line has the values). A list
of red chain names beside the gate, checked
only for exit 1, cannot tell those apart: do not keep one.

The loop runs every file directly in `.shrt/chains` (the glob does not descend), and that is where
`shrt chain slice -write <name>` writes, so a slice written by name joins the gate at once
(`slice -write` says so). Keep a slice you want gated there, with `kept_red` if it is red on
purpose. Write an exploratory one outside it by giving `-write` a path, a value with a slash or
ending in `.yaml`, which is written exactly there, relative to the current directory, and never
over a file that is not the same slice: `shrt chain slice <chain> -step <id> -write
.shrt/scratch/<name>.yaml`. No sweep reads `.shrt/scratch/`, and `shrt init` adds it to `.gitignore`
(`shrt doctor` suggests it where it is missing); run it by path:
`shrt run .shrt/scratch/<name>.yaml`. Its runs are stored under its `name:`, so `run` refuses, sending
nothing, a file given by path whose `name:` is that of a chain in `paths.chains` unless it IS that
chain's file: rename it (`name: <name>-scratch`), or its runs would be proposed and counted as that chain's.

The gate proves only what the chains send. A repeated request field that every chain sends with
one item (one order line) leaves per-item logic untested: a total computed wrong only for two or
more lines passes every chain above. `shrt contract status -gaps` lists each such field as
`one item`; cover it with a step that sends two items with different values and asserts what
depends on both, before trusting a green gate on that rpc. Two items that both read
`${create_product.product.id_product}` are still one product: a backend that prices every line at
the first line's product passes them, so `-gaps` lists such a field as `same resource`. `shrt
contract plan` scaffolds two items for that reason, the second reading a second producer step
(`create_product_2`, with its own sku and price) so a safe spot records a total that depends on
both.

Both baseline files are committed, and each holds one number, the score its gate must equal; a
missing file fails the gate. Create them once, before the first gate run: write `0` into each
(`echo 0 > .shrt/quality-baseline; echo 0 > .shrt/hollow-baseline`), run
`shrt contract quality -gate -baseline .shrt/quality-baseline`, and if it fails, the message names
the current score: read what it counts with `shrt contract quality` and write that number into the
file. Do the same for `shrt chain hollow -gate -baseline .shrt/hollow-baseline` after running the
chains, since it reads the run records they leave (run records are gitignored, so on a fresh clone
the gate's own runs are what it reads). From then on, change a baseline only as a reviewed edit.

## The loop

```
      shrt catalog ls -filter <word>          what rpcs exist
   → shrt contract show <rpc>                 what the curated layer knows
   → shrt contract plan <rpc> -write          let the contract compose the chain
   → edit .shrt/chains/<name>.yaml            fill ONLY the test data
   → shrt chain lint <name>                   shape, refs, required fields
   → shrt run <name> -dry-run                 resolve everything, send nothing
   → shrt run <name>                          the receipt
   → shrt chain hollow                        did any read pass and find nothing?
   → shrt confirm <name> -note "..."          propose it; show the user the summary, ask
   → (user says yes) shrt confirm <name> -approve -by <their email>
   → shrt verify <name>                       replay and diff, later
```

Everything left of `edit` is derivation, and the tool does it. Everything right of it is evidence.
The only part that is yours is the middle: **the test data, and the assertions that say what
correct means.**

`chain lint` reports a step that asserts nothing, or whose assertion cannot fail, as a warning and
still exits 0 — the right default while a chain is half-written. A gate is not half-written: run
`shrt chain lint -strict`, which makes those errors. `PITFALLS.md` §27 is the adopter who did not.

That loop assumes the contract already knows the domain. Authoring the contract itself is a
different loop, with `shrt contract quality` as its feedback signal rather than `chain lint` —
`PLAYBOOK.md` §7.

## The other loop: you are about to change code

Once a chain has a safe spot it stops being a test you wrote and becomes the baseline you refactor
against:

```
      shrt run <name>                         a receipt on today's binary
   → shrt verify <name> -run <run-id>         does it still match what a person approved?
   → (change the code)
   → shrt run <name> ; shrt verify ...        the same two lines
```

`shrt run` asks whether your assertions still hold. `shrt verify` asks whether the response still
matches, field by field, including everything nobody thought to assert — and `-run <id>` re-diffs a
recorded run offline, so the check costs no extra traffic. When a refactor moves a number, the diff
names the rpc instead of leaving you a failure somewhere downstream. `PLAYBOOK.md` §9.

**No safe spot yet? `shrt diff` compares two runs instead.** `shrt diff <name>` compares the
last two recorded runs of a chain that are not `shrt verify` replays, naming both (`shrt diff <name> <run-a> <run-b>` for any two; ids, `latest`,
`latest~N`): which step changed status, where the first failure moved, which steps are no longer
reached, and which response fields differ, with the chain's `volatile` paths, ids and timestamps
masked. Each difference is labelled with its kind (`changed`, `type`, `length`, `missing`, ...,
listed in `GRAMMAR.md` §7). It sends nothing and needs no human. It is a comparison between two runs, NOT a verdict
against a confirmed baseline: if run A was already wrong, "no differences" means B is wrong the
same way. `PLAYBOOK.md` §9.

**Expect that road to be barely paved at first**, because only a human can create a safe spot
(rule 1 below) and each needs its `volatile` list worked out — so the corpus grows chains far
faster than it grows baselines. `shrt chain ls` marks which chains have one; read the ratio there
rather than assuming it, and treat a wide gap as the normal early state, not a defect.

## Four rules that are never negotiable

1. **Approve only on the user's yes.** A safe spot is the user's claim that a run is correct.
   Propose with `shrt confirm <c> -note "..."`, where the note says what you inspected in the
   responses and why they are right, not that the run is green. Then present the proposal in the
   conversation (`PLAYBOOK.md` §8) and ask. Run `-approve -by <user email>` only after the user
   answers yes to that proposal; "looks fine" about something else, silence, or your own judgement
   is not a yes. Never hand the user a report path to read instead.
2. **Never reorder, skip or parallelise steps.** Order is the contract. A chain that runs out of
   order reproduces a different state and its green means nothing.
3. **Never hand-write a body from memory.** Field names and enum values come from the descriptor
   (`shrt catalog describe`), never from recall. A plausible-looking body that lints is the most
   expensive kind of wrong.
4. **Never leave a step asserting only that it did not crash.** `error.code == OK` is the floor,
   not the assertion. What did the call *do*? See `PLAYBOOK.md` §4. `contract plan` writes only the
   floor, since it cannot know the values; `chain lint -strict` fails each such step whose contract
   declares response facts until you assert one.

## Where the authority is

| question | authority | not |
|---|---|---|
| what fields does this rpc take | the descriptor: `shrt catalog describe <rpc>` | the contract, which may be stale |
| what does this rpc require | the curated contract's `required`, read from backend source | the proto, which has no `required` |
| what keys may I write | `GRAMMAR.md` | anything that "looks like" another tool's key |
| does this code really fire | a run record under `.shrt/runs/` | the contract's `when:`, which is a claim |
| did that read find anything | `shrt chain hollow`, which reads the record's body | a green step, which only says the server answered |
| which roles may call this rpc | the backend's own authorisation rows, read from its source | a hand-written `requires_role:` nothing compared |
| is this run correct | the user's yes, recorded by `shrt confirm -approve -by <email>` | a green tick, or an agent's proposal |
| are these four files the rules being enforced | `shrt doctor`, which compares them to the binary | the fact that they are installed |

Run records are **gitignored**: they exist only on the machine that produced them. A run id quoted
in a document is not evidence a fresh clone can check.
