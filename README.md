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
#   a real defect: shrt chain pin <chain> keeps it red in a slice, the rest green
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
`.shrt/safespots/pending/` are gitignored build output, so a fresh clone has none of them, and a
run id quoted in a document is no evidence it can check. Run `shrt doctor` before trusting a green. Rebuild the descriptor after any proto change and the binary
after any change to shrt itself (`go build -o shrt ./cmd/shrt`): both go stale quietly.

`shrt init` guesses the `auth:` block from the descriptor and says so; check the login it picked
(`GRAMMAR.md` §4). A second role on the same login gets a profile when `<ROLE>_USER` and
`<ROLE>_PASSWORD` are exported at init and the repo's README names the role; export them and
re-run `shrt init` to add one later. Export the env vars
`auth.body` reads before a run (read their names from `.shrt/config.yaml`); without them
`shrt run` refuses before sending anything.

Only `run` (not `-dry-run`), `verify` (not `-run <id>`), `gate` and `chain slice -verify` send
traffic.

## Commands

Any command exits 2 for an unknown command, 1 for a bad flag or a setup it cannot load, 0 for `-h`
and otherwise as below; 3 is no verdict, neither red nor green: re-run.

To check a release for bugs, run `shrt gate -repro` (first row): it does the work that otherwise
follows the gate, a settled suspect, a verified repro per suspect rpc and a check of the masks, and
names the states no chain covers. Its rows and repros are the answer for what the chains cover;
probe only what its `gaps:` lines name, or a support ticket no row explains.

| command | does | exits other than 0 |
|---|---|---|
| `shrt gate -repro` | the gate without the chains that wait by design (each `SKIPPED`, never counted as passing; `-skip-waits=false` keeps them), then for each row of `failures by suspect rpc:` the read that settles an unclear write or read, and `repro: shrt run <path>  (reproduced 3/3)`, a slice in `.shrt/scratch/` verified 3 times; one `masks:` line says whether a mask hid more than run tags, ids and timestamps; `gaps:` lists the states no chain calls a gated write from (`contract status -gaps`), each with its `shrt contract plan <rpc> -write -force`; not for CI | 1 a failure; 3 no verdict, or nothing failed but a chain was skipped |
| `shrt init` | write `.shrt/`, build the descriptor, install the skill, subagent and `.shrt/ci-gate.sh` | 2 descriptor not built; 3 credentials not exported |
| `shrt version` | version, commit, build time and the docs it carries | |
| `shrt doctor` | check this repo's `.shrt/` installation: prints each WARN and FAIL, `-v` every check | 1 a FAIL, or a warning under `-strict` |
| `shrt catalog build` | rebuild the descriptor after a proto change | |
| `shrt catalog ls [-filter x]` | list the rpcs | |
| `shrt catalog describe <rpc>` | request and response schema with proto comments | |
| `shrt contract init <domain>` | scaffold a contract overlay (`-all` for every domain); keeps what you wrote | |
| `shrt contract show <rpc>...` | schema, paste-ready step, exportable paths and the curated contract | |
| `shrt contract lint` | validate overlays against the descriptor | 1 an error, or no overlay |
| `shrt contract plan <rpc>[@alias]...` | compose one ordered chain from the contracts, with its probes (`-write`); `-all` plans one per rpc | 1 nothing planned |
| `shrt contract status [-gaps]` | coverage per domain; `-gaps` lists what no chain exercises | |
| `shrt contract quality [-domain d]` | score contracts for what is missing; `-gate -baseline <file>` ratchets it | 1 off the baseline, or a contract error |
| `shrt chain new -name <c> <rpc>...` | scaffold a chain from the descriptor and contracts | |
| `shrt chain lint [<c>]` | static checks; `-strict` also fails `unfailable-assertion`, `asserts-nothing`, `inert-allow-fail`, `export-overwritten`, `interpolated-arithmetic`, `envelope-only` | 1 a lint error |
| `shrt chain ls` | one line per chain: `*` safe spot, `?` pending proposal, `R` kept red, `W` waits by design (its total wait at the end) | |
| `shrt chain which [-rpc r] [-code n]` | which chains exercise an rpc or assert a code, with a slice command; under `-rpc`, the state and item count each write step acts on | 1 nothing matched |
| `shrt chain slice <c> -step <id>` | the minimal sub-chain reproducing one step (`-keep writes,<id>`); `-verify` proves it against the latest run or `-run <id>`; a chain you wrote by hand is proven with `run -repeat 3` | 1 refused, NOT REPRODUCED, intermittent, STILL FAILS without; 3 `-run latest` did not evaluate the step, DID NOT RUN, INCONCLUSIVE, FAILS DIFFERENTLY without |
| `shrt chain pin <c>` | pin a red chain: each defect kept red in a verified slice of its own, the chain rewritten without it until it runs green | 1 refused, or a slice did not reproduce |
| `shrt chain hollow` | read steps that passed with an empty response, from run records | 1 hollow reads, or `-gate` off baseline; 2 no run records |
| `shrt run <c>` | execute in order and record (`-dry-run`, `-keep-going`, `-var k=v`, `-quiet`); 0 when kept red as pinned; `-repeat n` runs it n times unchanged, 0 when every run failed the same way (`reproduced n/n`) | 1 failed, or refused before sending (a chain error such as an expect path not in the response); `-repeat`: the runs differ, or none failed; 3 |
| `shrt confirm <c> -note "..."` | propose a passing run as the safe spot: a short summary to show the user, the full report in `.shrt/safespots/pending/`; `-all` proposes every chain whose latest run passed and whose safe spot is missing or differs | 1 refused |
| `shrt confirm <c> -approve -by <email>` | write the safe spot after the user's yes (`-all` for each pending one); `-reject`, `-pending`; `<new> -rename-from <old>` carries one across a pure rename | 1 refused |
| `shrt verify <c>` | replay and diff against the safe spot; `-run <id>` re-diffs a record offline | 1 drift, replay failed, no safe spot, a `FINDING`; 3 |
| `shrt diff [<c>] <run-a> <run-b>` | compare two recorded runs; no safe spot needed, not a verdict | 1 they differ; 2 could not compare |
| `shrt gate` | the CI gate, below | 1 a failure, a `FINDING` or the ratchet; 3 |

### CI gate

`shrt gate` sends every chain in `.shrt/chains` once (by verify when it has a safe spot, by
`run -keep-going` otherwise, so every failing step reaches the gate's suspects, with a fresh
`-var tag` as short as run's own), retries an exit 3 once, and holds `shrt chain hollow` to
`.shrt/hollow-baseline`. One line per chain:

| line | means |
|---|---|
| `PASS` | ran green, no drift from its safe spot |
| `KEPT RED` | failed exactly as its `kept_red` pins |
| `FAIL pins held, new change:` | every pin held; a change outside them is a regression, not a reason to re-pin |
| `FAIL regression:` / `order changed:` / `different input:` / `chain change:` | what verify calls the first new change |
| `FINDING intermittent:` / `repeated:` | its only failures are calls of an rpc this gate found failing on some calls, and the steps they explain; one `FINDING:` line at the end counts them over every chain and says once what that means |
| `FAIL` over `FINDING: ... failure at <rpc>, below` | such a call failed and something else changed too; the `FAIL` line names that change |
| `FAIL not as pinned:` | a kept-red chain that failed otherwise or passed; the moved pin and its suspect are named, judged against the pinned value, a pin that held counting as no change |
| `NO VERDICT` | exit 3: backend down, restarting or refusing auth |
| `SKIPPED` | `-repro` or `-skip-waits` left it out; never counted as passing, so with nothing failed the gate exits 3 |

Each `FAIL` line ends with its suspect and `also <suspect>` for the first other one (`at <field>` when that one is a
write, the field it changed; `at transport code <code>` when it was refused before a body existed), or
`same fault as <chain>` when that chain's line names it and every other suspect of this chain (the first chain with a
safe spot that fails so, which may be below; else the first above); a suspect that line leaves out keeps this line's
own suspect and `also` for it; a slice failing at its parent's first change has no line of its own, the parent's says
`(+N slice(s) fail the same: ...)`; a kept-red slice folds so only when its pins held and its parent fails every way
it does (by suspect rpc and field), and one failing not as pinned keeps its line. Then `failures by suspect rpc:`, one
line per suspect rpc (or per `unclear` set of rpcs), headed by the field each failing step changed, wherever a read
shows it, its example from a chain with a safe spot when one fails so. `-v` adds, under each failing chain, the
suspect's request and every change with its want and got as `verify` prints it (`run`'s failed expectations for a
chain with no safe spot; a change repeated at more steps or list items once, `(and N more at ...)`), and the knock-on
counts: no separate `verify` is needed to see the values. How a suspect is chosen: `PLAYBOOK.md` §8.

A chain with `wait:` steps is marked `W` by `shrt chain ls`, with its total wait, and named on stderr
as the gate starts. `-skip-waits` leaves it out, and so does `-repro` unless `-skip-waits=false` is
given or chains are named (never in CI: the wrapper below passes neither). Sent, it starts at
once, beside the other chains, when it writes nothing (each step is the configured login or a
read) and each read's request carries `${vars.tag}`, the gate's fresh tag: it then changes nothing
another chain reads, and no other chain can change what it reads. Otherwise it runs in its turn and
the stderr line names the step that keeps it there. Every line still comes in its place. A gate with
such a chain, or one that took 30s or more, ends with `time: <total>; slowest: <chain> <time> (waits
<d> by design, ...)`, so a long gate is not mistaken for a hang.

`-repro` follows the summary with one block per row: for an `unclear` suspect whose field another
read rpc also returns, `settled on the write` or `settled on the read`, from a copy of the chain up to
that read plus the other read (`.shrt/scratch/<chain>-tell-apart-<read>.yaml`); then `repro: shrt run
<path>  (reproduced 3/3)`, `chain slice -verify` of the row's step (in a chain with a safe spot when
the row has one) written to `.shrt/scratch/<chain>-slice-<step>.yaml`, kept with more writes when
the slice says so, or `repro: none:` and why. One `masks:` line closes it: `verify -run latest -json`
of each chain with a safe spot, offline, then each value a volatile path hid that is not a run tag, an
id or a timestamp, listed; the items of a whole list a step marks volatile (an unscoped list, which
holds whatever else the backend holds) are only counted. Then `gaps:` lists the state gaps `shrt
contract status -gaps` reports for the writes the gated chains call, each ending with `shrt contract
plan <rpc> -write -force`, or says there is none; the closing line says to probe only those, or a
support ticket no row explains.

A token refused early once makes the gate hold a
fresh one (at most 30s) and re-send a read: refused twice is a `FINDING` that sessions end early
(`-no-session-check` skips it). `shrt gate <chain>...` gates a subset, without the ratchet. With no
overlay, or an rpc whose contract no chain calls, it ends with one `coverage:` line.

`shrt init` writes this wrapper to `.shrt/ci-gate.sh` (commit it; `init -force` refreshes it); it
skips the contract checks while `.shrt/contracts` holds no overlay. Init also writes `0` into a
missing `.shrt/quality-baseline`, never over one; a gate failing on it names the current score, to
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
the test data, and the assertions that say what correct means. The gate's `shrt chain lint -strict`
fails the six assertion-quality warnings in the command table; other warnings exit 0.
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
