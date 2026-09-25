# shrt docs

What an agent needs to compose an RPC chain and a contract. `.claude/skills/shrt/SKILL.md` routes
here; `shrt init` installs these four files into `.shrt/docs/`:

| file | job |
|---|---|
| `README.md` | the commands, exit codes, the loop, the rules |
| `GRAMMAR.md` | every key shrt accepts, generated from the code; a key not in it does not exist |
| `PLAYBOOK.md` | procedures: compose, fill, assert, probe, author a contract, verify, slice |
| `PITFALLS.md` | symptom → cause → fix |

A package path such as `runner/runner.go` is a file in the shrt module
(`github.com/N4darae/shrt`). Every command prints its flags and exit codes with `-h`.

## Quickstart: a regression suite for a service

This is the path that finds regressions. Chains written by hand miss most of what the planner
probes (boundaries, the last item of a list, other roles, missing tokens, read-backs after every
write, repeats), so write contracts and let `plan` compose the chains.

```bash
export API_USER=... API_PASSWORD=...           # the login's credentials, and <ROLE>_USER/_PASSWORD
shrt init                                      # observes the envelope from one real login
shrt contract init -all                        # one overlay per domain under .shrt/contracts/
#   fill each overlay from the service's code: required, failures, effects, fields.<f>.value
shrt contract lint && shrt contract status -gaps
shrt contract plan -all -write                 # one chain per rpc; fix every fill:/gap: line, re-plan -force
shrt run <chain>                               # every chain; a red one on a correct backend is
#   a real defect: pin it with chain slice -kept-red, and slice -without failed for the rest
shrt confirm -all -note "..."                  # show the user the table; approve only on their yes
shrt gate                                      # later, against every new release
```

A contract states behaviour; `effects:` states what a write does to numbers (`GRAMMAR.md` §3).
`summary` and `note` are prose for people.

## Before the first command

```bash
cd "$(git rev-parse --show-toplevel)"
shrt init -agents=false -build=false # only if .shrt/docs/ is missing, as on a fresh clone
shrt catalog build                   # the descriptor; without it every catalog command fails
shrt doctor                          # is this installation sound?
shrt catalog ls -filter <word>
```

`.shrt/docs/`, the descriptor, `.shrt/runs/`, `.shrt/tokens.json` and
`.shrt/safespots/pending/` are gitignored build output, so a fresh clone has none of them.
`shrt doctor` compares the installed docs and the `.claude/` kit with the binary, the descriptor
with a rebuild, `.gitignore`, the token cache, the `auth:` profiles and their env vars, the envelope
conventions, the overlays and every safe spot; run it before trusting a green. Rebuild the
descriptor after any proto change and the binary after any change to shrt itself
(`go build -o shrt ./cmd/shrt`): both go stale quietly.

`shrt init` guesses the `auth:` block from the descriptor and says so; check the login it picked
(`GRAMMAR.md` §4). A second role on the same login gets a profile when `<ROLE>_USER` and
`<ROLE>_PASSWORD` are exported at init and the repo's README names the role; export them and
re-run `shrt init` to add one later. Export the env vars
`auth.body` reads before a run (read their names from `.shrt/config.yaml`); without them
`shrt run` refuses before sending anything.

Only `run` (not `-dry-run`), `verify` (not `-run <id>`), `gate` and `chain slice -verify` send
traffic.

## Commands

| command | does |
|---|---|
| `shrt init` | write `.shrt/`, build the descriptor, install the skill, subagent and `.shrt/ci-gate.sh` |
| `shrt version` | version, commit, build time and the docs it carries |
| `shrt doctor` | check this repo's `.shrt/` installation; `-strict` fails on warnings |
| `shrt gate` | run every chain and verify every safe spot with a fresh tag; one line per chain, failures grouped by suspect rpc |
| `shrt catalog build` | rebuild the descriptor after a proto change |
| `shrt catalog ls [-filter x]` | list the rpcs |
| `shrt catalog describe <rpc>` | request and response schema with proto comments |
| `shrt contract init <domain>` | scaffold a contract overlay (`-all` for every domain); keeps what you wrote |
| `shrt contract show <rpc>...` | schema, paste-ready step, exportable paths and the curated contract |
| `shrt contract lint` | validate overlays against the descriptor |
| `shrt contract plan <rpc>[@alias]...` | compose one ordered chain from the contracts, with its probes (`-write`); `-all` plans one per rpc |
| `shrt contract status [-gaps]` | coverage per domain; `-gaps` lists what no chain exercises |
| `shrt contract quality [-domain d]` | score contracts for what is missing; `-gate -baseline <file>` ratchets it |
| `shrt chain new -name <c> <rpc>...` | scaffold a chain from the descriptor and contracts |
| `shrt chain lint [<c>]` | static checks; `-strict` also fails `unfailable-assertion`, `asserts-nothing`, `inert-allow-fail`, `export-overwritten`, `interpolated-arithmetic`, `envelope-only` |
| `shrt chain ls` | one line per chain: `*` safe spot, `?` pending proposal, `R` kept red |
| `shrt chain which [-rpc r] [-code n]` | which chains exercise an rpc or assert a code, with a slice command |
| `shrt chain slice <c> -step <id>` | the minimal sub-chain reproducing one step; `-verify -run <id>` proves it |
| `shrt chain hollow` | read steps that passed with an empty response, from run records |
| `shrt run <c>` | execute in order and record (`-dry-run`, `-keep-going`, `-var k=v`, `-quiet`) |
| `shrt confirm <c> -note "..."` | propose a passing run as the safe spot; prints a short summary to show the user, the full report in `.shrt/safespots/pending/` |
| `shrt confirm <c> -approve -by <email>` | write the safe spot after the user's yes; `-reject`, `-pending` |
| `shrt confirm -all -note "..."` | propose every chain whose latest run passed and has no or a changed safe spot; `-all -approve -by <email>` after the user's yes to each |
| `shrt confirm <new> -rename-from <old> -by <email>` | carry a safe spot across a pure rename |
| `shrt verify <c>` | replay and diff against the safe spot; `-run <id>` re-diffs a record offline |
| `shrt diff [<c>] <run-a> <run-b>` | compare two recorded runs; no safe spot needed, not a verdict |

### Exit codes

Any command exits 2 for an unknown command, 1 for a bad flag or a setup it cannot load, 0 for `-h`.

| command | 0 | 1 | 2 | 3 |
|---|---|---|---|---|
| `run` | passed; dry run valid; kept red as pinned | failed, or refused before sending | — | error: no verdict, re-run |
| `verify` | no drift, replay passed | drift, replay failed, no safe spot, a `FINDING` | — | could not verify, re-run |
| `confirm` | proposed, approved, rejected, listed, renamed | refused | — | — |
| `chain slice` (no `-verify`) | printed or written | refused | — | `-run latest` did not evaluate the step |
| `chain slice -verify` | reproduced | NOT REPRODUCED | DID NOT RUN, or refused before sending | INCONCLUSIVE; 4 intermittent |
| `diff` | runs do not differ | they differ | could not compare | — |
| `chain hollow` | nothing unexplained; `-gate` at baseline | hollow reads; `-gate` off baseline | no run records | — |
| `chain which` | matched | nothing matched | — | — |
| `doctor` | no FAIL | a FAIL, or a warning under `-strict` | — | — |
| `contract lint` | no error | an error, or no overlay | — | — |
| `contract plan` | printed or written | nothing planned | — | — |
| `contract quality -gate` | at the baseline | off the baseline, or a contract error | — | — |

### CI gate

`shrt gate` runs every chain in `.shrt/chains` (not `.shrt/scratch/`) and verifies every safe spot,
each with a fresh `-var tag` when the chain reads one, retries an exit 3 once after `-retry-wait`
(20s), and holds `shrt chain hollow` to `.shrt/hollow-baseline`. It prints one line per chain,
`PASS`, `KEPT RED` (failed exactly as its `kept_red` pins), `FAIL` (with what verify calls it:
regression, order changed, different input or chain change; `intermittent` when a `FINDING` says
so; `not as pinned` for a kept-red chain that failed otherwise) or `NO VERDICT` with the first
failing step and path (a list that shrank as its length), and under a `FAIL` the request of the suspect (`-v` adds each changed path with the
steps it changed at). Failures are then grouped, one line per suspect rpc. The read itself is the
suspect when it fails with a server error, when its list holds another set of items while every
write before it answered as before, when only the order of a list changed, when the write it
observes returned the same field of the same record unchanged, or when the same change follows two
different writes. A change first seen in an earlier step's answer for the same field of the same
record belongs to that step, a write whose own answer changed included. Otherwise it is the write
the read observes (`<write>` in `<read>_after_<write>`, else the nearest earlier write on the same
entity), reads beneath it; steps left unevaluated behind a failed step fold into one line under it:

```
FAIL       items-move   regression: get_item_after_move_item (ItemService/GetItem) item.slot want=4 got=2
  suspect write move_item (ItemService/MoveItem) as operator sent {"id_item":"itm-1","slot":4}
failures by suspect rpc (the read itself, or the failing or changed write it observes), then the rest:
  ItemService/MoveItem: passed itself, but steps after it failed or changed; e.g. items-move move_item
    +3 step(s) after it: GetItem item.slot
    +2 step(s) in 1 chain(s) unevaluated because GetItem changed item.slot
```

Exit 0 is green; 1 is a failure, a `FINDING`, tokens of one auth profile refused early in two runs
of the gate, or the ratchet. Refused early once, the gate logs in afresh, holds the token as long as
the refused one lived (at most 90s) and re-sends a read that passed: accepted, it was a restart;
refused twice, a `FINDING` (`-no-session-check` skips this). 3 is no verdict
(the backend was down, restarting or refusing auth): re-run once it is up, and count it neither red
nor green. `shrt gate <chain>...` gates a subset, without the ratchet. With no overlay, or an rpc
whose contract no chain calls, the gate ends with one `coverage:` line naming the command that plans
the missing probes; it never changes the exit code.

`shrt init` writes this wrapper to `.shrt/ci-gate.sh` (commit it; `init -force` refreshes it); it
skips the contract checks while `.shrt/contracts` holds no overlay.
Write `0` into `.shrt/quality-baseline` first; a gate failing on it names the current score, to
write in as a reviewed edit. The first gate outside CI writes `.shrt/hollow-baseline` with today's
count and says so; commit it. With `CI` set, a missing baseline fails the gate.

```bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
[ -f .shrt/docs/GRAMMAR.md ] || shrt init -agents=false -build=false
shrt catalog build
shrt doctor -strict
if compgen -G '.shrt/contracts/*.y*ml' > /dev/null; then
  shrt contract lint
  shrt contract quality -gate -baseline .shrt/quality-baseline
fi
shrt chain lint -strict
exec shrt gate
```

Build the tag into other text (`name: item-${vars.tag}`): a field that is `${vars.tag}` alone is
input, so the fresh tag makes every verify of it drift. A slowdown fails the gate only with
`latency: {fail: true}`, which `shrt init` writes into every new config.

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

Left of `edit` is derivation, and the tool does it. Right of it is evidence. Yours is the middle:
the test data, and the assertions that say what correct means. A gate runs
`shrt chain lint -strict`, which fails the six assertion-quality warnings in the command table;
other warnings, such as `unasserted-timestamp`, stay warnings and exit 0.
Authoring the contract itself is a different loop, fed by `shrt contract quality`
(`PLAYBOOK.md` §7).

## The other loop: you are about to change code

```
      shrt run <name>                         a receipt on today's binary
   → shrt verify <name> -run <run-id>         does it still match what a person approved?
   → (change the code)
   → shrt run <name> ; shrt verify ...        the same two lines
```

`run` asks whether your assertions hold; `verify` asks whether every response field still matches
the approved run, including what nobody asserted. With no safe spot yet, `shrt diff <name>`
compares the last two runs; it is a comparison, not a verdict (`PLAYBOOK.md` §9). `shrt chain ls`
shows which chains have a safe spot; expect few at first, since only a person creates one.

## Four rules that are never negotiable

1. **Approve only on the user's yes.** Propose with `shrt confirm <c> -note "..."`, the note
   saying what you inspected in the responses and why it is right. Present the proposal in the
   conversation (`PLAYBOOK.md` §8) and ask. Run `-approve -by <user email>` only after the user
   answers yes to that proposal; silence or your own judgement is not a yes.
2. **Never reorder, skip or parallelise steps.** Order is the contract.
3. **Never hand-write a body from memory.** Field names and enum values come from the descriptor
   (`shrt catalog describe`).
4. **Never leave a step asserting only that it did not crash.** `error.code == OK` is the floor,
   not the assertion (`PLAYBOOK.md` §4). `chain lint -strict` fails such a step when its contract
   declares response facts.

## Where the authority is

| question | authority | not |
|---|---|---|
| what fields does this rpc take | `shrt catalog describe <rpc>` | the contract, which may be stale |
| what does this rpc require | the contract's `required`, read from backend source | the proto |
| what keys may I write | `GRAMMAR.md` | another tool's keys |
| does this code really fire | a run record under `.shrt/runs/` | the contract's `when:` |
| did that read find anything | `shrt chain hollow` | a green step |
| which roles may call this rpc | the backend's authorisation rules | an unchecked `requires_role:` |
| is this run correct | the user's yes, via `shrt confirm -approve` | a green run, or an agent |
| are these docs the rules enforced | `shrt doctor` | the fact that they are installed |

Run records are gitignored and machine-local: a run id quoted in a document is not evidence a
fresh clone can check.
