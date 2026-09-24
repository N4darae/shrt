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
another domain. It says so when it does, because a guess is not a fact: **check the call it picked**
before the first run. If nothing looked like a login it writes no block at all and says that too;
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
| `shrt doctor` | check this repo's own `.shrt/`: which build is running, installed docs and the `.claude/` agent kit against the copy embedded in the binary, descriptor against a rebuild, `.gitignore` against the paths that must never be committed, the token cache's mode, the auth profiles and every `${env.*}` they read, and the envelope conventions against the response messages. `-strict` fails on warnings too |
| `shrt catalog build` | rebuild the descriptor after a proto change |
| `shrt catalog ls [-filter x]` | list the RPC surface |
| `shrt catalog describe <rpc>` | request and response schemas with proto doc comments |
| `shrt contract init <domain>` | scaffold the curated contract; re-running keeps what you wrote, and leaves an overlay alone (`unchanged`) when its content would not change, whatever its YAML layout |
| `shrt contract show <rpc>...` | generated schema — example body, paste-ready step YAML, exportable paths — plus the curated semantics. `-json` for tooling, `-filter <word>` for every rpc whose name contains the word |
| `shrt contract lint` | validate contracts against the descriptor |
| `shrt contract plan <rpc>[@alias]...` | compose one ordered chain reaching every target from the dependency graph, references pre-wired |
| `shrt contract status [-gaps]` | contract-entry coverage per domain (how many rpcs have a curated contract, not how much the chains exercise); `-gaps` lists each rpc with no contract ('no contract') or in no multi-step plan ('no path to'), then streaming rpcs |
| `shrt contract quality [-domain d]` | score each contract against the curation terms, and name what is missing |
| `shrt chain new -name <c> <rpc>...` | scaffold a chain from real proto fields |
| `shrt chain lint [<c>]` | static validation against the catalog; `-strict` turns the assertion-quality warnings into errors (an assertion that cannot fail that is reported as a warning, a step asserting nothing, an `allow_fail` that does nothing, an export a later step silently overwrites), which is the form a CI gate should run. Other warnings, such as the `-var`s and environment a run needs (including the env vars the login body of each auth profile the chain's steps run under reads), are not promoted. An expect path that can never match, `exists: false` on a path the message has no field for, an export reading a field the response does not have (the run fails that step), and a reference to a field an earlier step's response does not have (the run refuses the chain), are errors with or without `-strict` |
| `shrt chain ls` | one line per chain, marking which have a safe spot; `-long` for full descriptions |
| `shrt chain which [-rpc <rpc>] [-code <n>]` | which chains exercise an rpc or assert a failure code, marking each step `OBSERVED` when a local run record reached it, citing the newest such run and what it got even when that contradicts the assertion, and printing the `chain slice` command that reproduces the best match. Under `-code`, when no chain asserts the code but a local run record carried it, it lists those steps with a reproduce command instead of failing |
| `shrt chain slice <c> -step <id>` | the minimal ordered sub-chain that reproduces one step; `-write [name]` it (`-force` to replace another chain), `-mode pin -run <id>` to pin values from a run instead of rebuilding their producers, `-keep <id,…>` to force earlier steps back in, `-var k=v` to supply a var the chain does not declare, `-verify -run <id>` to prove the slice still fails the same way (`-build <id>` stamps that run, and the recorded verdict names the build), comparing also the refusal's message, reason and app_code, a transport refusal, and the value a failing expectation got (ids and timestamps masked as `verify` masks them) (the slice's run record is kept only with `-write`). `-run latest` with `-mode pin` or `-verify` uses the newest run that reached the step; when the slice keeps every step, `-write` records the verdict in that chain instead of writing a copy |
| `shrt chain hollow` | read steps that passed while the response carried nothing |
| `shrt run <c>` | execute in order and record |
| `shrt confirm <c> -note "..."` | propose a passing run as the safe spot; prints the summary table to show the user and writes a full report. It writes no safe spot |
| `shrt confirm <c> -approve -by <user email>` | write the safe spot, only after the user said yes to that proposal in the conversation; `-reject` discards it, `-pending` lists proposals |
| `shrt verify <c>` | replay and diff against the safe spot, masking volatile paths and id- or timestamp-shaped values, and counting both. A replay against another target than the safe spot's is said first (`targets differ: ...`). A volatile pattern the safe spot did not approve (added to the config or chain after approval) fails it, naming what the pattern hid; `-masked` lists every masked value. It replays as `-keep-going` does, so every step a failure does not block is compared; a step held back behind a failure is reported `not_reached`, not as a change of length, and the report names the first failing step |
| `shrt diff [<c>] <run-a> <run-b>` | compare two recorded runs of one chain step by step — status changes, where the first failure moved, steps no longer reached, response fields — with declared volatile paths, ids and timestamps masked. Needs no safe spot, and is a comparison between two runs, not a verdict. `shrt diff <c>` is `latest~1` against `latest` |

### Exit codes

A gate reads these, so they are part of the interface. Any command also exits 2 for an unknown
command or group subcommand, 1 for a flag it cannot parse or a setup it cannot load (no
`.shrt/config.yaml`, a config that does not parse, a missing descriptor, and, in every command that
reads contracts, an overlay under `paths.contracts` that does not parse, named with its parse
error), and 0 for `-h`.

| command | 0 | 1 | 2 | 3 |
|---|---|---|---|---|
| `run` | passed; a `-dry-run` resolved and validated | `failed`: an expectation did not hold; also a refusal before anything was sent (unknown chain, a `-var` it never reads, a missing var, an unset env var read by a step or by the login body of an auth profile a step runs under, or a reference to a step or export that does not exist or runs later, or to a response field the producing step's message does not declare, an unknown auth profile, an rpc the catalog does not have, a streaming rpc, a conventions path no response declares) | — | `error`: a step could not complete (unresolved reference, invalid request, login failed, target unreachable), so the run is not a verdict about the backend |
| `verify` | no drift and the replay passed | drift vs the safe spot, the replay did not pass, or no safe spot | — | could not verify: a step never got an answer (target unreachable, login failed) and nothing else drifted |
| `confirm` | proposal written, approved, rejected or listed | refused (no passing run, no `-note`, no `-by`, nothing pending) | — | — |
| `chain slice -verify` | `reproduced` | `NOT REPRODUCED` | `DID NOT RUN`; also a refusal before anything was sent (an unknown chain or step, no `-run`, a run that does not reach the step, a missing or not-fresh `-var name=<fresh>`), so nothing was verified | `INCONCLUSIVE` |
| `diff` | the two runs do not differ | they differ | could not compare (unknown run, runs of two chains, the wrong number of arguments) | — |
| `chain hollow` | no unexplained hollow read; under `-gate`, at the baseline | hollow reads reported; under `-gate`, worse or better than the baseline | no run records to read | — |
| `chain which` | a chain or run record matched | nothing matched, or bad flags | — | — |
| `doctor` | no FAIL (warnings allowed) | a FAIL, or a warning under `-strict` | — | — |
| `contract lint` | no contract error (warnings allowed) | a contract error, an overlay that does not parse, or no overlay to check | — | — |
| `contract quality -gate` | the score equals the baseline | the score is worse than the baseline, better without the baseline being lowered, or the baseline file is missing | — | — |

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
last two recorded runs of a chain (`shrt diff <name> <run-a> <run-b>` for any two; ids, `latest`,
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
   not the assertion. What did the call *do*? See `PLAYBOOK.md` §4.

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
