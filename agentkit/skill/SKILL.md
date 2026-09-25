---
name: shrt
description: Use when defining, running, or verifying an ordered chain of internal API calls to reproduce a state — writing a shrt chain YAML, authoring or reading an RPC contract, composing a chain from the contract graph, replaying a chain against a running backend, or diffing a replay against a confirmed safe spot. Triggers on "shrt", "chain", "contract", "replay the API flow", "reproduce this state", "safe spot", "which API broke".
---

# shrt

Record, replay and verify ordered chains of internal API calls. A **chain** is an ordered list of
RPC calls that reproduces a backend state. A **safe spot** is a run a person approved as correct;
later replays are diffed against it, so a regression names the rpc that changed.

## Before the first command

```bash
cd "$(git rev-parse --show-toplevel)"
shrt init -agents=false -build=false # only if .shrt/docs/ is missing, as on a fresh clone
shrt catalog build                   # the descriptor; rebuild after any proto change
shrt catalog ls -filter invoice      # what rpcs exist
```

`.shrt/docs/` and the descriptor are gitignored build output; the first line restores the docs
from the binary and says if it wrote anything else. Commit `.claude/` (this skill and the
`shrt-contract-author` subagent); after upgrading shrt, `shrt init -force -build=false` refreshes
docs and kit but never your config.

## Building a regression suite

Follow `.shrt/docs/README.md` "Quickstart" in order: export the login credentials, `shrt init`,
fill the contracts, `shrt contract plan -all -write`, run, pin real defects with `chain slice`,
`shrt confirm -all`, then `shrt gate` per release. Do not hand-write the chains: the planner's
probes find what hand-written chains miss.

## The rules

`.shrt/docs/README.md` owns the four rules: approve a safe spot only on the user's yes to a
proposal you presented, never reorder steps, never hand-write a body from memory, never leave a
step asserting only that the server did not crash.

## Route

| question | file |
|---|---|
| commands, exit codes, the loop, the rules | `.shrt/docs/README.md` |
| which keys exist, with types; `${...}` forms; what verify compares | `.shrt/docs/GRAMMAR.md` |
| compose, fill, assert, probe, principals, author a contract, verify, find, slice | `.shrt/docs/PLAYBOOK.md` §1-§11 |
| it did something strange | `.shrt/docs/PITFALLS.md` |

Every command prints its flags and exit codes with `-h`.

## Contracts

- `shrt contract show <rpc>` prints the generated schema and the curated layer; `shrt contract plan
  <rpc> -write` composes a chain from it.
- `shrt contract init -all` scaffolds every domain; existing curation is kept. A domain is the
  package segment after the organisation root (`acme.billing.invoice.v1.InvoiceService` is
  `billing`).
- To fill unexplored domains, dispatch one `shrt-contract-author` subagent per domain.
- Then do the `before:` pass yourself over the finished library, since a per-domain author cannot
  see consumers in other domains: every `read rpc with no producer` line of `shrt contract quality`
  and every one-step `shrt contract plan <read>` is a candidate for `before:` on the write that
  creates those rows, or `no_producer:` on the read.
