# shrt docs

`shrt init` installs these files into `.shrt/docs/`. Every command prints its flags and exit codes with `-h`.

- `README.md`: the gate, setup, commands, rules.
- `GRAMMAR.md`: every key, generated from the code. A key not in it does not exist.
- `PLAYBOOK.md`: procedures.
- `PITFALLS.md`: symptom, cause, fix.

## Check a release: `shrt gate -repro`

It runs every chain once and groups what failed by suspect rpc. For each row it then:

- settles an `unclear` suspect with a read that tells the two apart;
- writes a minimal repro to `.shrt/scratch/` and runs it 3 times;
- firms a `trigger:` that rests on few calls by sending the contrasting calls itself.

Last it checks the masks, and plans and runs up to 4 states no chain covers.

Report its rows, repros and gap probes: for what the chains cover, they are the answer.

### Chain lines

- `PASS a, b, c`: green chains with no drift, joined on one line.
- `KEPT RED`: a known defect, failing exactly as pinned on the date shown. Nothing to do.
- `FAIL pins held, new change:`: a new defect beside a kept-red chain's pins. A regression.
- `FAIL not as pinned:`: a pin moved, or the chain passed. A regression.
- `FAIL <chain>  <step> ... want=... got=...`: the first new change verify found. Fix the suspect. `order changed:` marks a list order change.
- `FAIL different input:` or `chain change:`: the chain or its vars changed since approval (PITFALLS.md §34).
- `FINDING intermittent:` or `repeated:`: an rpc that fails on some calls only. A real defect.
- `NO VERDICT`: backend down, restarting or refusing auth. Re-run.
- `SKIPPED`: left out by `-repro` or `-skip-waits`. Never counts as passing.

The end of a `FAIL` line:

- `suspect ...` names who changed the field. Verdict words: PLAYBOOK.md §8. `, settled by -repro` means the row below settled it.
- `<step> fails as in <chain>` or `same fault as <chain>`: that chain's line names this fault. Fix it once, there.
- `+N slices fail the same: ...`: slices folded into their parent's line.
- `+N kept red (<date>): ...` or `+N kept red fail too (<date>): ...`: known defects, every pin held. No need to run those slices.

### Summary parts

- `failures by suspect rpc:`: one row per suspect rpc and field, with an example step. The answer.
Each row holds its own lines: the counter move, `settled ...`, `trigger:` and `repro:`.

- `trigger:`: what the failing calls' requests have that the passing calls' lack. Take it as the pattern. Where it rested on few calls, `-repro` has already sent the contrasting calls; the line shows the result and ends `[firmed by -repro]`.
- No `trigger:` line: nothing in the requests splits them. Probe from the example step.
- `trigger: none: sent again ...`: a contrasting call contradicted the split. There is no trigger.
- A counter's move ending in a ratio such as `2x`: how far a suspect write moved it, against the approved run.
- `settled on the write` or `settled on the read`: fix that side. `not settled:` keeps both.
- `repro:`: a minimal slice in `.shrt/scratch/`, reproduced 3 of 3 times. `repro: none:` says why there is none.
- `masks:`: whether a mask hid more than run tags, ids and timestamps. Check each value it lists.
- `gaps:`: states no chain calls a gated write from, each planned and run once. A row there is a fault unless the contract is wrong; no approved run is compared.
- `not probed:`: a gap past the first 4, or one it could not run. Probe it with the printed command.

### Re-run and inspect

- `shrt run <path>` as the `repro:` line prints it.
- `shrt verify <chain> -run latest` re-diffs the gate's run offline. With no safe spot it lists the failed expectations.
- `shrt diff <chain> -step a,b` prints those steps as recorded.
- `shrt gate -v <chain>` re-sends one chain and prints each change with its want and got.
- `shrt run <path> -repeat 3` proves a repro you wrote by hand.
- A suspect is a lead, not proof. Test a suspect write with `shrt chain slice <chain> -without <step> -verify`.

### What `-repro` leaves out

- Chains with `wait:` steps, marked `W` in `shrt chain ls`. With nothing failed, a skipped chain makes the gate exit 3.
- Keep them with `-skip-waits=false`, or name chains: `shrt gate <chain>...`.
- Not for CI: the wrapper below runs plain `shrt gate`.

## Quickstart: a regression suite for a service

Write contracts and let `plan` compose the chains. Hand-written chains miss most of what it probes.

```bash
export API_USER=... API_PASSWORD=...     # the login; <ROLE>_USER/_PASSWORD add a profile
shrt init                                # observes the envelope from one real login
shrt contract init -all                  # one overlay per domain; fill each (PLAYBOOK.md §7)
shrt contract lint && shrt contract status -gaps
shrt contract plan -all -write           # fix each fill:/gap: line, re-plan with -force
shrt run <chain>                         # red on a backend you trust: shrt chain pin <chain>
shrt confirm -all -note "..."            # show the user the table; approve only on their yes
shrt gate                                # later, against each new release
```

## Before the first command

```bash
cd "$(git rev-parse --show-toplevel)"
shrt init -agents=false -build=false   # only when .shrt/docs/ is missing
shrt catalog build                     # the descriptor every catalog command needs
shrt doctor                            # is this installation sound?
```

- `.shrt/docs/`, the descriptor, `.shrt/runs/`, `.shrt/tokens.json` and `.shrt/safespots/pending/` are gitignored.
- Rebuild the descriptor after a proto change and the binary after a change to shrt. Both go stale quietly.
- After upgrading shrt, `shrt init -force -build=false` refreshes docs and kit, never your config. Commit `.claude/`.
- `shrt init` guesses the `auth:` block. Check the login it picked (GRAMMAR.md §4).
- A role gets a profile when `<ROLE>_USER` and `<ROLE>_PASSWORD` are exported and the repo's README names it. Re-run `shrt init` to add one.
- Export the env vars `auth.body` reads before a run.
- Only `run`, `verify`, `gate`, `chain pin` and `chain slice -verify` or `-minimize` send traffic, besides init's one login. `run -dry-run` and `verify -run <id>` do not.

## Commands

Exit 3 means no verdict: re-run.

| command | does |
|---|---|
| `shrt init` | set up `.shrt/`, the agent kit and `.shrt/ci-gate.sh` |
| `shrt version` | which build, and the docs it carries |
| `shrt doctor` | check this repo's `.shrt/` |
| `shrt catalog build` | rebuild the descriptor |
| `shrt catalog ls` | list the rpcs |
| `shrt catalog describe <rpc>` | request and response schema |
| `shrt contract init <domain>` | scaffold an overlay; keeps what you wrote |
| `shrt contract show <rpc>` | schema, paste-ready step, curated contract |
| `shrt contract lint` | validate overlays |
| `shrt contract plan <rpc>[@alias]...` | compose a chain with its probes; `-write` saves it |
| `shrt contract status [-gaps]` | coverage; `-gaps` lists what no chain exercises |
| `shrt contract quality` | score what contracts lack |
| `shrt chain new -name <c> <rpc>...` | scaffold a chain |
| `shrt chain lint [<c>]` | static checks; `-strict` also fails `unfailable-assertion`, `asserts-nothing`, `inert-allow-fail`, `export-overwritten`, `interpolated-arithmetic`, `envelope-only` |
| `shrt chain ls` | `*` safe spot, `?` proposal pending, `R` kept red, `W` waits |
| `shrt chain which [-rpc r] [-code n]` | chains that call an rpc or assert a code |
| `shrt chain slice <c> -step <id>` | minimal sub-chain reproducing a step; exits 1 NOT REPRODUCED, 3 DID NOT RUN or INCONCLUSIVE |
| `shrt chain pin <c>` | keep each defect red in its own slice, run the rest green |
| `shrt chain hollow` | reads that passed with an empty response |
| `shrt run <c>` | send in order and record; exits 0 when a kept-red chain fails as pinned |
| `shrt confirm <c> -note "..."` | propose the safe spot; `-approve -by <email>` after the user's yes |
| `shrt verify <c>` | replay and diff against the safe spot |
| `shrt diff <c>` | compare two runs; not a verdict |
| `shrt gate` | the CI gate, below |

### CI gate

`shrt gate` runs each chain in `.shrt/chains` once, by `verify` if it has a safe spot, else by `run -keep-going`. Each gets a fresh `-var tag`; an exit 3 is retried once. It holds `shrt chain hollow` to `.shrt/hollow-baseline`. `shrt gate <chain>...` skips that ratchet.

`shrt init` writes this wrapper to `.shrt/ci-gate.sh`. Commit it.

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

- `.shrt/quality-baseline` starts at `0`. A gate failing on it names the score to write in, as a reviewed edit.
- The first gate outside CI writes `.shrt/hollow-baseline`; commit it. With `CI` set, a missing baseline fails.
- A token refused early twice is a `FINDING`; `-no-session-check` skips that check.
- Build the tag into other text, `name: item-${vars.tag}`. A field that is `${vars.tag}` alone drifts on every verify.
- A slowdown fails only with `latency: {fail: true}`, which init writes.

## The loop

```
      shrt catalog ls -filter <word>          what rpcs exist
   → shrt contract show <rpc>                 what the contract knows
   → shrt contract plan <rpc> -write          let the contract compose the chain
   → edit .shrt/chains/<name>.yaml            fill ONLY the test data
   → shrt chain lint <name>                   shape, refs, required fields
   → shrt run <name> -dry-run                 resolve everything, send nothing
   → shrt run <name>                          the receipt
   → shrt chain hollow                        did any read pass and find nothing?
   → shrt confirm <name> -note "..."          propose; show the user, ask
   → (user says yes) shrt confirm <name> -approve -by <their email>
   → shrt verify <name>                       replay and diff, later
```

Yours is the middle: the test data, and assertions that say what correct means.

Changing code? Run `shrt run <name>` and `shrt verify <name> -run <run-id>` before and after. `run` checks your assertions; `verify` checks every response field against the approved run.

## Rules that are never negotiable

1. **Approve only on the user's yes.** Propose with `shrt confirm <c> -note "..."`, present it (PLAYBOOK.md §8) and ask. Silence is not a yes.
2. **Never reorder, skip or parallelise steps.** Order is the contract.
3. **Never hand-write a body from memory.** Names and enum values come from `shrt catalog describe`.
4. **Never leave a step asserting only that it did not crash.** `error.code == OK` is the floor (PLAYBOOK.md §4).
5. **Never re-pin a moved pin.** `not as pinned` and `pins held, new change` are regressions; compare with `shrt diff`.
6. **Never overwrite an approved chain.** Plan beside it with `-write <name>.yaml`; `-force-approved` only when the user says so.
7. **Never delete a probe that fails.** Keep the defect red with `shrt chain pin`.
8. **Never hand-merge a safe spot's JSON** (PLAYBOOK.md §8).
