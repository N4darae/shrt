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

`shrt init` adds `.shrt/descriptor.binpb`, `.shrt/docs/`, `.shrt/runs/` and `.shrt/tokens.json` to
`.gitignore`, so a fresh clone has no descriptor and no installed docs. The binary is not in the repo
at all. Build the descriptor from the repo root, wherever that clone lives — `shrt init` copies these
files into every repo that adopts shrt, so nothing here may assume one machine's path:

```bash
cd "$(git rev-parse --show-toplevel)"
shrt catalog build                   # the descriptor; without it every catalog command fails
shrt doctor                          # is this installation sound?
shrt catalog ls -filter <word>
```

**`shrt doctor` is the check to run before you trust a green.** The four files you are reading are
installed build output, copied out of the binary by `shrt init`; upgrade the binary and the copy
stays where it was, so the rules you are reading can be older than the rules being enforced.
Nothing else notices that. `doctor` compares them, and in the same pass reports which build of
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
run. Without them a run dies at the first step that needs a token, with status `error`, and that
step **sends nothing**, which is a fixture problem, not a backend one.

If the target is a remote box whose credential must not be copied around, run shrt on that box
instead of forwarding the secret; where that is written down is this repo's business, not the
kit's.

## Commands

| command | does |
|---|---|
| `shrt init` | write `.shrt/`, build the descriptor, install the Claude skill and subagent |
| `shrt version` | which build this is — version, commit, build time, and the four docs it carries; `-short` prints the version alone |
| `shrt doctor` | check this repo's own `.shrt/`: which build is running, installed docs against the copy embedded in the binary, descriptor against a rebuild, `.gitignore` against the paths that must never be committed, the token cache's mode, the auth profiles and every `${env.*}` they read, and the envelope conventions against the response messages. `-strict` fails on warnings too |
| `shrt catalog build` | rebuild the descriptor after a proto change |
| `shrt catalog ls [-filter x]` | list the RPC surface |
| `shrt catalog describe <rpc>` | request and response schemas with proto doc comments |
| `shrt contract init <domain>` | scaffold the curated contract; re-running keeps what you wrote |
| `shrt contract show <rpc>...` | generated schema — example body, paste-ready step YAML, exportable paths — plus the curated semantics. `-json` for tooling, `-filter <word>` for every rpc whose name contains the word |
| `shrt contract lint` | validate contracts against the descriptor |
| `shrt contract plan <rpc>[@alias]...` | compose one ordered chain reaching every target from the dependency graph, references pre-wired |
| `shrt contract status [-gaps]` | contract coverage per domain |
| `shrt contract quality [-domain d]` | score each contract against the curation terms, and name what is missing |
| `shrt chain new -name <c> <rpc>...` | scaffold a chain from real proto fields |
| `shrt chain lint [<c>]` | static validation against the catalog; `-strict` turns the assertion-quality warnings into errors (an assertion that cannot fail, a step asserting nothing, an expect path that can never match, a reference nothing can produce, an export reading a field the response does not have, an `allow_fail` that does nothing), which is the form a CI gate should run. Other warnings, such as the `-var`s and environment a run needs, are not promoted |
| `shrt chain ls` | one line per chain, marking which have a safe spot; `-long` for full descriptions |
| `shrt chain which [-rpc <rpc>] [-code <n>]` | which chains exercise an rpc or assert a failure code, marking each step `OBSERVED` when a local run record reached it, citing the newest such run and what it got even when that contradicts the assertion, and printing the `chain slice` command that reproduces the best match |
| `shrt chain slice <c> -step <id>` | the minimal ordered sub-chain that reproduces one step; `-write [name]` it (`-force` to replace another chain), `-mode pin -run <id>` to pin values from a run instead of rebuilding their producers, `-keep <id,…>` to force earlier steps back in, `-var k=v` to supply a var the chain does not declare, `-verify -run <id>` to prove the slice still fails the same way (the slice's run record is kept only with `-write`) |
| `shrt chain hollow` | read steps that passed while the response carried nothing |
| `shrt run <c>` | execute in order and record |
| `shrt confirm <c> -by <name> -i-verified` | promote a run to the safe spot — **human only** |
| `shrt verify <c>` | replay and diff against the safe spot |
| `shrt diff [<c>] <run-a> <run-b>` | compare two recorded runs of one chain step by step — status changes, where the first failure moved, steps no longer reached, response fields — with declared volatile paths, ids and timestamps masked. Needs no safe spot, and is a comparison between two runs, not a verdict. `shrt diff <c>` is `latest~1` against `latest` |

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
   → (human) shrt confirm ...                 promote to safe spot
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
   → shrt verify <name> -run <run-id>         does it still match what a human confirmed?
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
masked. It sends nothing and needs no human. It is a comparison between two runs, NOT a verdict
against a confirmed baseline: if run A was already wrong, "no differences" means B is wrong the
same way. `PLAYBOOK.md` §9.

**Expect that road to be barely paved at first**, because only a human can create a safe spot
(rule 1 below) and each needs its `volatile` list worked out — so the corpus grows chains far
faster than it grows baselines. `shrt chain ls` marks which chains have one; read the ratio there
rather than assuming it, and treat a wide gap as the normal early state, not a defect.

## Four rules that are never negotiable

1. **Never run `shrt confirm`.** A safe spot is a human's claim that a run is correct. Produce the
   candidate, show it, and give the user the exact command. "This looks right" is not confirmation.
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
| is this run correct | a human, via `shrt confirm` | a green tick |
| are these four files the rules being enforced | `shrt doctor`, which compares them to the binary | the fact that they are installed |

Run records are **gitignored**: they exist only on the machine that produced them. A run id quoted
in a document is not evidence a fresh clone can check.
