# PITFALLS

Symptom, cause, fix. Keys are in `GRAMMAR.md`, procedures in `PLAYBOOK.md`.

## Setup and tooling

### 1. A key is rejected, or seems to do nothing
Loaders reject unknown keys and suggest the nearest. A key from another tool (`allow_failure:`, `one_of:`) does not exist. Look it up in `GRAMMAR.md`.

### 2. A step says `error`, and you blame the backend
`error` usually means nothing was sent: a reference or login body did not resolve, an env var is unset, or the login failed. Read the step's `error` line. `failed` is evidence about the server; `error` is almost always the fixture.

### 3. A field plainly in the response reads as missing, or lint passes an old shape
The descriptor is stale, and lint trusts it. Run `shrt catalog build` after every proto change; `shrt doctor` fails when it differs.

### 4. Lint passes a rule you know changed
The `shrt` binary or the installed docs are stale. Check `shrt version` and `shrt doctor`, rebuild the binary, then `shrt init -force -build=false`.

### 5. `shrt run` refuses with `env NAME is not set` although a token is cached
The token cache is keyed by the resolved login body, so the body must resolve first. Export the variables `auth.body` names.

### 6. Exit 3 from `run` or `verify`
No verdict: the backend was unreachable or restarting, a fixture was reused, or auth was refused. Re-run once, with a fresh `-var tag` if the line says so. Exit 3 is neither red nor green.

### 7. A streaming rpc is skipped or refused
`shrt contract plan` plans a server-streaming rpc's first message (`messages.0`) and its token probes; many auth interceptors skip streams. Client- and bidi-streaming rpcs are refused: test them by hand.

## Writing chains

### 8. `not_empty` or `exists` asserts the wrong thing
`not_empty` is false for `""`, `0`, `false`, `[]`, `{}` and an int64 `"0"`. `exists` reads what the server sent, and a proto3 scalar sends nothing for its zero value. Use `exists: true` for presence; to tell zero from unset, assert the value (`equals: false`).

### 9. `${uuid}` in `vars:` is rejected
`vars` values are not resolved. Put `${uuid}` in the body, or pass `-var key=...`.

### 10. A green `-dry-run`, then a red first run
A dry run has no responses, so references resolve to scaffold values. It proves the chain's shape, not bodies built from references. Run `chain lint`, then a real run.

### 11. A number or clock comparison fails on equal-looking values
`equals` compares text and does no arithmetic: `${a.qty}+${b.qty}` is the text `0+5`. Work the result out and assert a literal. Compare clocks with `within: {of: "${nowunix+3600}", by: 5}`, never `equals`.

### 12. A step fails with an `envelope` entry although its expectations held
The call was refused in-band and no expectation pins the verdict, so a check on zeros held by accident. Assert the envelope: `equals: <ok>` for a call that must succeed, the refusal code for one that must be refused.

### 13. A refusal probe is asked for every required field, or its body assertions are `unevaluated`
Lint treats a step as a probe only when it pins a refusal on the envelope or on `transport.code` / `transport.http_status`. A 4xx Connect error has no body: assert `transport.*`.

### 14. `allow_fail` did not let the chain go on
It tolerates only a transport refusal on a step with no expectations. Assert the refusal, or run with `-keep-going`.

### 15. The second run of a chain is refused as a duplicate
A unique field is a literal, or built from a var left at an earlier run's value.

- Build unique values from `${vars.tag}` and leave `tag` undeclared.
- `fixture reused` or `fixture collision` (exit 3): re-run with a fresh var.
- `CHAIN DEFECT: the chain collides with itself` (exit 1): build the literal field it names from a var.
- A conflict with a create that got a server error earlier in the run: the backend stored it anyway.
- An idempotency key must be `${uuid}`.

### 16. A prefix list counts another run's fixtures
`inv-${vars.tag}` also matches tag `x0`'s fixtures. End the prefix with the separator: `inv-${vars.tag}-`.

### 17. An exact count passes once and then fails
The list is not scoped to the run. Filter by something the run created, or assert membership (`includes:`) and a lower bound.

### 18. A `-var` is ignored, or refused as a typo
A var the chain never reads is dropped with a warning. A name within two edits of a real var is refused; the refusal lists the real ones.

### 19. A scratch chain run by path is refused
Its `name:` is a chain's under `paths.chains`. Rename it: `name: <name>-scratch`.

### 20. Perturbing a var for a drift test broke the next replay
The var reached shared state other chains read. Perturb only values the chain's own tag isolates.

## Contracts and plans

### 21. A planned chain lints, runs green and means nothing
`plan` wires what the contracts say. Read `order:` as a claim; a missing step is a missing `needs:` or `from:`. Paste the `effects:` each `gap:` prints.

### 22. A field stays unfilled and no `from:` reaches it
The value exists only in a request. Use `same_as: <rpc>->request_path`.

### 23. Two supposedly independent entities are one
An `@alias` not declared under `aliases:` makes an identical step. Declare it with the fields that differ.

### 24. A backend code is in no contract, or no chain can send it
Contracts are hand-kept: script a comparison of your error constructors against every `failures:` entry. A code only a body the proto cannot express would reach: declare it `unreachable:` with the reason.

### 25. Contract lint says a failure repeats another
Same code, reason and field. If they are different branches, give each its own `field:`.

### 26. A `before:` edge or `source:` path points at nothing
Nothing checks edges or paths. When an rpc is retired, grep the overlays for it. Write `source:` paths without line ranges.

### 27. Batch steps pass while every line was refused
Set `conventions.item_envelope_path` and check it against a refused line. A backend whose verdict is not at `error.code` needs `envelope_path` and `envelope_ok` too (PLAYBOOK.md §3b).

## Runs, safe spots and verify

### 28. `confirm` refuses a run
The run did not pass, its record was edited, or the chain is kept red. `-approve` also refuses when the chain file changed since the run. Run again and approve on the proposal's branch.

### 29. `confirm` warns that fields differ from the earlier passing run
Values change every run without looking like ids or timestamps. Declare them `volatile`, or the list `unordered`, unless the change is real. Run twice before proposing.

### 30. Verify fails after you add a `volatile` or `redact` pattern
A safe spot keeps the patterns it was approved with. Propose a new run with `-supersede`.

### 31. A masked value is reported anyway
A volatile value that became null or vanished was lost, not changed. A timestamp that changed unit, or jumped over 400 days, is real too. Treat both as real changes.

### 32. `FINDING: intermittent failure at <rpc>`
A server error answered on its one re-send, elsewhere in the run, or in the previous run; in the gate, also an rpc failing every Nth call. A real defect, just not deterministic (exit 1).

### 33. A token refused long before its stated expiry
Sessions end early, or the backend restarted. Settle it with a chain of the login and reads carrying `${vars.tag}`, `wait:` between, no writes, so `shrt gate` runs it beside the others. Keep it out of the per-commit gate.

### 34. `drift after a chain change` or `drift with different input`
The chain or its vars changed since approval. Restore them, or run the chain green and propose it with `-supersede`.

### 35. A renamed chain lost its safe spot, or its JSON conflicts in a merge
A safe spot belongs to the chain name: `shrt confirm <new> -rename-from <old> -by <email>`. For a merge conflict, follow PLAYBOOK.md §8.

### 36. `shrt diff` says no differences, and both runs were wrong
`diff` compares; it is not a verdict. Use a safe spot.

### 37. The response carries fields the proto does not declare
The backend is newer than the proto. Update it and run `shrt catalog build`.

### 38. A secret still appears in a run record
Redaction misses a secret hashed or double-encoded, and a credential in a header not named like one. Add a `redact` path.

### 39. A kept-red chain says `PINNED DEFECT GONE`
A fix was deployed, or the target runs another build. Check the run's `build` first. If the fix is real, remove `kept_red`, run, propose.

### 40. A kept-red chain says `FAILED, NOT AS PINNED` or `NEW FAILURE`
Something else failed, or a pinned step now answers differently. Treat it as a regression; do not re-pin it away.

## Slices and search

### 41. A slice went green where the chain was red
It dropped a write whose state the target needed. Always `-verify`, follow its `next:` line, and read `WARNING possible under-inclusion`.

### 42. A slice landed in the directory every gate runs
`-write` got a path under `paths.chains`. Move it to `.shrt/scratch/`. Only `shrt chain pin` writes slices beside the chain, on purpose.

### 43. `-verify` asks for `-var name=<fresh>`
A kept write puts that var into what it creates, and the old values exist. Pass one never used before.

### 44. `chain slice -write` refuses the chain's own file
The slice drops part of the chain you wrote. Prove that chain with `shrt run <file> -repeat 3`, or write the slice under another name.

### 45. `chain hollow` counts runs of a deleted chain
Run records are never deleted for you; they show as `orphan`, outside the counts. Delete `.shrt/runs/<name>/`.

### 46. A gate of `lint && run` is green on chains that prove nothing
Plain `chain lint` exits 0 on assertion-quality warnings. Gate with `bash .shrt/ci-gate.sh`, which runs `chain lint -strict`.
