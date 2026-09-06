---
name: shrt
description: Use when defining, running, or verifying an ordered chain of internal API calls to reproduce a state — writing a shrt chain YAML, authoring or reading an RPC contract, composing a chain from the contract graph, replaying a chain against a running backend, or diffing a replay against a confirmed safe spot. Triggers on "shrt", "chain", "contract", "replay the API flow", "reproduce this state", "safe spot", "which API broke".
---

# shrt

Record, replay and verify ordered chains of internal API calls.

A **chain** is an ordered sequence of RPC calls that reproduces a specific backend state.
A **safe spot** is a chain run **a human confirmed as correct**. It is the ground truth a later
replay is diffed against, so a regression names the RPC that changed instead of guessing.

This file is the overview and the router. The working surface is `.shrt/docs/` — four files that
`shrt init` installed into this repo alongside the chains. Read them before composing anything.

## Before the first command

Run from the repo root, whichever clone this is — nothing here assumes one machine's path:

```bash
cd "$(git rev-parse --show-toplevel)"
shrt catalog build                   # the descriptor; without it every catalog command fails
shrt catalog ls -filter invoice      # what rpcs exist
```

The descriptor is build output and normally gitignored, so a fresh clone has none. Rebuild it
after any proto change: it fails quietly rather than loudly, which is `.shrt/docs/PITFALLS.md` §2.

`shrt` itself comes from `go install github.com/N4darae/shrt/cmd/shrt@latest`. Inside a
clone of the shrt repo the binary is gitignored and built from source instead, and must be rebuilt
after any change to that source, because a stale binary lints with the OLD rules and tells you
everything is fine — `.shrt/docs/README.md` has the two lines for that case.

## The rules

`.shrt/docs/README.md`, "Four rules that are never negotiable", owns them. Read it there; this
file deliberately keeps no second copy. The shortest of the four: never run `shrt confirm`, never
reorder steps, never hand-write a body from memory, never leave a step asserting only that the
server did not crash.

## Route

| question | file |
|---|---|
| what am I allowed to do, and what is the loop | `.shrt/docs/README.md` |
| which keys exist, with types — chain, contract, config, run record, `${...}` forms, volatile patterns, the command list | `.shrt/docs/GRAMMAR.md` (generated from the Go structs and gate-checked, so an invented key is not in it) |
| compose a chain from a contract · with no contract · fill bodies · assert something that can fail · probe one failure code · cross a principal boundary · author a contract · run, hand off, verify | `.shrt/docs/PLAYBOOK.md` §1-§8 |
| it did something strange | `.shrt/docs/PITFALLS.md`, symptom → cause → fix |

## Things the route does not cover

- **The contract is two layers.** The *generated* layer comes from the descriptor and is always
  right about shape; the *curated* layer is what someone wrote down about behaviour. `shrt contract
  show <rpc>` prints both (`-json` for tooling); `-filter <domain>` does a whole domain at
  once. If the descriptor is stale because
  the proto changed, rebuild it with `shrt catalog build`.
- **What `shrt chain lint` checks:** every body against its proto message, `${...}` references to
  steps that do not run earlier, and exports reading response fields that do not exist.
- **What a `shrt verify` diff tells you:** each change names the step, the JSON path, the confirmed
  value and the value now returned — so a non-empty diff points at an RPC, not at a chain.
- **No safe spot? `shrt diff <chain>` compares the last two runs** (or `shrt diff <chain> <run-a>
  <run-b>`): status changes, where the first failure moved, steps no longer reached, response
  fields, with volatile paths, ids and timestamps masked. It is a comparison between two runs, not
  a verdict — never create the safe spot yourself to get one; `shrt confirm` is human only.
- **`shrt contract init -all`** scaffolds every domain at once into `.shrt/contracts/`; existing
  curation is carried forward, never overwritten. A domain is the package segment after the
  organisation root once the trailing version is dropped, so
  `acme.billing.invoice.v1.InvoiceService` is domain `billing`, and a package with no organisation
  root — `inv.v1.InventoryService` — is domain `inv`.
- **To fill an unexplored domain, dispatch one `shrt-contract-author` subagent per domain** — they
  share no state and each owns one file.
- **Then do the `before:` pass yourself, once, over the finished library.** It cannot be delegated
  with the rest: `before:` is the edge a PRODUCER declares about a consumer in another domain, and
  a per-domain author cannot see consumers that are still being written beside it. Measured on the
  first surface this kit was used against, three parallel authors wrote **zero** `before:` entries
  while the finished library needed thirteen. After every domain is authored:

  ```bash
  shrt contract quality            # every read_with_no_producer is a candidate
  shrt contract plan <that-rpc>    # a one-step order confirms the producer edge is missing
  ```

  For each, find the write that puts those rows there and add `before:` on it, or `no_producer:` on
  the read if nothing in this API does. A read whose plan is one step long is the signal.
