---
name: shrt
description: Use when defining, running, or verifying an ordered chain of internal API calls to reproduce a state — writing a shrt chain YAML, authoring or reading an RPC contract, composing a chain from the contract graph, replaying a chain against a running backend, or diffing a replay against a confirmed safe spot. Triggers on "shrt", "chain", "contract", "replay the API flow", "reproduce this state", "safe spot", "which API broke".
---

# shrt

Record, replay and verify ordered chains of internal API calls. A **chain** is an ordered list of
RPC calls that reproduces a backend state. A **safe spot** is a run a person approved as correct;
later replays are diffed against it, so a regression names the rpc that changed.

## Check a release for bugs

Run `shrt gate -repro`. It leaves out the chains that wait by design (marked `W` in `shrt chain
ls`, then `SKIPPED`; `shrt gate <chain>` runs one), so it takes seconds. Each row of `failures by
suspect rpc:` gets the read that settles an unclear write or read, or two writes on a counter, and
a verified one-line repro (`repro: shrt run <path>  (5 of 119 steps, reproduced 3/3)`, as small as a
re-run shows it can be, with the read-back that contradicts a write's answer kept in it); its
`trigger:` line names the requests that fail against those that pass (profile, a list's length or
repeated key, a field's value or byte length, or two of these together), or says every call fails,
and how got relates to what was sent: take it as the pattern instead of working it out from the
runs. A row of a
counter a write moved (`increase:`/`decrease:` in its contract) says how far against the approved run
(`fell 4 from 10 to 6 where the approved run fell 2 from 10 to 8: 2x`), measured only on that record.
One `masks:` line says whether a mask hid more than run tags, ids and timestamps; each `KEPT RED`
line names every pin of its slice and the day it was pinned, and a
fold `(+N kept-red slice(s), every pin held, ...)` under a failing parent says those pins held, so
no `shrt run` of the slices is needed. `shrt verify
<chain> -run latest` shows every changed value of the gate's run offline; `shrt diff <chain> -step
a,b` shows those steps as recorded. For what the chains
cover, these lines are the answer: report them, no slice or `verify -masked` adds to them. The
`gaps:` block names each state no chain calls a gated write from; the gate has planned and run
each into `.shrt/scratch/` and says under it `passes:` or a row with its trigger and verified repro,
a fault unless the contract is wrong (no safe spot covers that state, so it is not compared to an
approved run); the closing line, printed last, counts the gap probes that failed apart from the
chains. Probe further only for a gap marked `not
probed:`, or a support ticket that no row explains. Never re-plan over an approved chain:
`contract plan <rpc> -write <name>.yaml` writes to `.shrt/scratch/`.

## Start here

On a fresh clone `.shrt/docs/` is missing: `shrt init -agents=false -build=false` restores it. Then
read `.shrt/docs/README.md`: "Before the first command", then "Quickstart" to build a regression
suite (let the planner compose the chains, do not hand-write them), and "Four rules that are never
negotiable", which hold for every command. Commit `.claude/` (this skill and the
`shrt-contract-author` subagent); `shrt init -force -build=false` refreshes docs and kit after an
upgrade and never touches your config.

## Route

| question | file |
|---|---|
| commands, exit codes, the loop, the rules | `.shrt/docs/README.md` |
| which keys exist, with types; `${...}` forms; what verify compares | `.shrt/docs/GRAMMAR.md` |
| compose, fill, assert, probe, principals, author a contract, verify, find, slice | `.shrt/docs/PLAYBOOK.md` §1-§11 |
| it did something strange | `.shrt/docs/PITFALLS.md` |
| contracts for many domains | one `shrt-contract-author` subagent per domain, then the `before:` pass of `.shrt/docs/PLAYBOOK.md` §7 over the whole library yourself: a per-domain author cannot see other domains |

Every command prints its flags and exit codes with `-h`.
