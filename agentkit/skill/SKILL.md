---
name: shrt
description: Use when defining, running, or verifying an ordered chain of internal API calls to reproduce a state — writing a shrt chain YAML, authoring or reading an RPC contract, composing a chain from the contract graph, replaying a chain against a running backend, or diffing a replay against a confirmed safe spot. Triggers on "shrt", "chain", "contract", "replay the API flow", "reproduce this state", "safe spot", "which API broke".
---

# shrt

Record, replay and verify ordered chains of internal API calls. A **chain** is an ordered list of
RPC calls that reproduces a backend state. A **safe spot** is a run a person approved as correct;
later replays are diffed against it, so a regression names the rpc that changed.

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
