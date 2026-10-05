---
name: shrt
description: Use when checking a release for bugs with shrt, replaying an RPC call chain, writing a chain or contract, or diffing a run against a safe spot. Triggers on "shrt", "chain", "contract", "safe spot", "replay the API flow", "which API broke".
---

# shrt

**To check a release for bugs, run `shrt gate -repro`.**

A safe spot is a run the user approved; replays are diffed against it.

## Read the gate

- Each row under `failures by suspect rpc:` is one fault, with its `trigger:` and a verified `repro:`. Report them: they are the answer.
- `KEPT RED` lines, and folds saying `every pin held`, are known defects. Every `FAIL` is new.
- A `gaps:` row is a fault in a state no chain covers.
- Re-run a repro with its printed `shrt run <path>`.
- `-repro` leaves out chains that wait (`W` in `shrt chain ls`); `shrt gate <chain>` runs one.
- Probe further only for a `not probed:` gap or a ticket no row explains.

## Never

- Approve a safe spot without the user's yes.
- Re-pin a moved pin.
- Plan over an approved chain: `shrt contract plan <rpc> -write <name>.yaml` writes to `.shrt/scratch/`.

## Read more

- `.shrt/docs/README.md`: gate output, commands, rules.
- `.shrt/docs/GRAMMAR.md`: every key.
- `.shrt/docs/PLAYBOOK.md`: procedures.
- `.shrt/docs/PITFALLS.md`: symptom to fix.
- Contracts for many domains: a `shrt-contract-author` subagent each, then PLAYBOOK §7's `before:` pass yourself.

No `.shrt/docs/`? Run `shrt init -agents=false -build=false`. Every command has `-h`.
