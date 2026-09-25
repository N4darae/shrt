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
| `shrt gate` | run every chain and verify every safe spot with a fresh tag; one line per chain and changes grouped by rpc and path |
| `shrt catalog build` | rebuild the descriptor after a proto change |
| `shrt catalog ls [-filter x]` | list the rpcs |
| `shrt catalog describe <rpc>` | request and response schema with proto comments |
| `shrt contract init <domain>` | scaffold a contract overlay (`-all` for every domain); keeps what you wrote |
| `shrt contract show <rpc>...` | schema, paste-ready step, exportable paths and the curated contract |
| `shrt contract lint` | validate overlays against the descriptor |
| `shrt contract plan <rpc>[@alias]...` | compose one ordered chain from the contracts, with its probes (`-write`) |
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
| `chain slice` (no `-verify`) | printed or written | refused | — | — |
| `chain slice -verify` | reproduced | NOT REPRODUCED | DID NOT RUN, or refused before sending | INCONCLUSIVE; 4 intermittent |
| `diff` | runs do not differ | they differ | could not compare | — |
| `chain hollow` | nothing unexplained; `-gate` at baseline | hollow reads; `-gate` off baseline | no run records | — |
| `chain which` | matched | nothing matched | — | — |
| `doctor` | no FAIL | a FAIL, or a warning under `-strict` | — | — |
| `contract lint` | no error | an error, or no overlay | — | — |
| `contract plan` | printed or written | nothing planned | — | — |
| `contract quality -gate` | at the baseline | off the baseline, or a contract error | — | — |

### CI gate

`shrt init` writes the script below to `.shrt/ci-gate.sh`, byte for byte (with a `#!/usr/bin/env
bash` line on top), so CI runs `bash .shrt/ci-gate.sh` instead of a copy cut out of this page;
commit it. Re-running `init` keeps an edited copy; `shrt init -force` rewrites it from the binary's
own README after an upgrade.

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
early=""
shopt -s nullglob
check() {
  local what="$1" c="$2" file="$3" prefix="$4" try rc args out
  for try in 1 2; do
    args=(-quiet)
    if grep -qs 'vars\.tag' "$file"; then args+=(-var "tag=$tag-$prefix$c-$try"); fi
    rc=0; out="$(shrt "$what" "$c" "${args[@]}")" || rc=$?
    printf '%s\n' "$out"
    early+="$(printf '%s\n' "$out" | sed -n 's/^ *WARNING: token refused .*(auth profile \([^,]*\),.*/\1/p')"$'\n'
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
early="$(printf '%s' "$early" | sed '/^$/d')"
if [ -n "$early" ]; then
  echo "gate: WARNING: $(printf '%s\n' "$early" | wc -l) run(s) had a token refused long before the expiry its login stated (the WARNING: token refused lines above)" >&2
  if [ -n "$(printf '%s\n' "$early" | sort | uniq -d)" ]; then
    echo "gate: FINDING: tokens of one auth profile were refused early in two runs of this gate: the backend ends sessions long before the expiry its login states" >&2
    fail=1
  fi
fi
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

Under `-quiet` a green chain prints one line, `<chain>: PASSED in 32ms` from `run` (a chain that
fails exactly as its `kept_red` pins, `<chain>: FAILED AS PINNED (kept red) in 25ms`) and
`<chain>: no drift vs safe spot <id>` from a clean `verify`, with no run-record path and no notes on
what was masked or covered; step warnings, `LATENCY`, `FINDING`, `REGRESSION` and `CHAIN DEFECT`
lines are printed whatever the verdict, and a chain that fails keeps its detail and the path of its
run record (`  run <id> -> <path>`). Drop `-quiet` to see the rest.

A token refused long before the expiry its login stated is invisible in a green gate otherwise:
shrt logs in again and re-sends the call, and the run passes. Each such run prints `WARNING: token
refused <N>s after issue although the login said it expires in <M>s (auth profile <p>, ...)`, and the
gate collects those lines. One is a warning at the end of the gate, since a deploy between the
previous gate and this one explains a cached token refused on its first use. Two for the same auth
profile in one gate fail it: the second token was issued during this gate, so a single restart does
not explain both (a restart during the gate, on top of a deploy before it, would; re-run the gate
to tell). A run that sees the re-login's own token refused early too, or the same early refusal as
the chain's previous run, exits 1 with `FINDING: token refused ...` by itself.
To prove it in one short run instead, hold a token past the lifetime you suspect with a step's
`wait:` (GRAMMAR §1, at most 10m, never counted as latency): two held reads after the login, each
waiting longer than that lifetime, reach the `FINDING` (PITFALLS 67). `contract plan` does not write
such a chain; keep it out of the per-commit gate, since its waits are its runtime.

A slowdown fails the gate only through `verify`, and only with `latency: {fail: true}` in the
config, which `shrt init` writes into every config it creates. Without it a step 700 times slower
than in the safe spot's run prints a `LATENCY` line and the gate exits 0: check that an older config
has it. The verdict stays safe from one slow answer: a slow read is re-sent (`latency.remeasure`,
default 2) and judged on its fastest answer, and a write, which is not re-sent, is judged only when
the previous run was slow there too (`LATENCY (unconfirmed)` otherwise, never a failure); a step must
be both 250ms slower and 3 times as slow (`floor_ms`, `ratio`).

A `run` in the gate stops at its chain's first failure, so under `-quiet` a red chain shows that
one failing step, and the steps after it were never sent: they may pass or fail. The run says so
on a line of its own, `N later step(s) were not run (...)`, naming them. Treat the first failure as
a lower bound on the blast radius, and re-run the chain with `-keep-going` to see which of the later
steps fail too. A `verify` in the gate replays as `-keep-going` does and lists every step that did
not pass, and so does a chain with `kept_red`.

Each run and each replay gets a fresh tag, or the second CI run of a chain trips its own
uniqueness constraints; a `-var` the chain never reads is ignored with a `warning:` line, so a loop may pass the tag
to every chain, but one whose name is close to a var the chain reads (`-var tga=...` for `tag`) is
refused as a likely typo. Build the tag INTO other text (`sku: sku-${vars.tag}`, or a header value `X-Tag: t-${vars.tag}`): verify
treats a var inside other text, in a body field or a header alike, as a fixture name and does not count a new one as a change (a list filter on a prefix built this way needs a terminator after the var, `sku_prefix: sku-${vars.tag}-`, or a tag that is a prefix of another, `ci1-x-1` and `ci1-x-10`, lists the other run's items too), but a
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
purpose; a bare file name, `-write <name>.yaml`, lands there too, beside the source chain, and
`-write <chain>.yaml` replaces the chain itself. Write an exploratory one outside it by giving
`-write` a path, a value with a slash, which is written exactly there, relative to the current
directory (`./<name>.yaml` for the current directory itself), and never over a file that is not the
same slice: `shrt chain slice <chain> -step <id> -write
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
both. The opposite gap is as real: a backend that merges lines naming one product, or takes stock
once per product rather than per line, passes every chain whose items always point at different
resources, so `-gaps` lists such a field as `no repeat`, and the plan adds
`<step>_same_<noun>_twice` (`create_order_same_product_twice` with its total,
`add_stock_batch_same_product_twice` with each line's level and a read after it,
`confirm_order_same_product_twice` with the stock read after it).

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

Left of `edit` is derivation, and the tool does it. Right of it is evidence. Yours is the middle:
the test data, and the assertions that say what correct means. A gate runs
`shrt chain lint -strict`, which fails the assertion-quality warnings plain lint lets through.
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
