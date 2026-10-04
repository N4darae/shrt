# PITFALLS

Symptom → cause → fix. Each entry is something an agent can still hit and has to act on. Keys are
in `GRAMMAR.md`, procedures in `PLAYBOOK.md`.

---

# Setup and tooling

## 1. A key is rejected, or a key you wrote seems to do nothing

**Cause.** Every loader rejects unknown keys: `unknown key "expects" at line 16 (did you mean
"expect"?)`. A key borrowed from another tool (`allow_failure:`, `one_of:`) does not exist.
**Fix.** Look the key up in `GRAMMAR.md`; a key not listed there does not exist.

## 2. A field that is plainly in the response reads as missing, or lint passes an old shape

**Cause.** The descriptor is stale. Lint validates against whatever the descriptor says, so a stale
descriptor is self-consistent and quiet. **Fix.** `shrt catalog build` after every proto change.
`shrt doctor` rebuilds and fails when the bytes differ.

## 3. Lint says everything is fine with a rule you know changed

**Cause.** A stale `shrt` binary lints with its old rules, and installed docs can be older than the
binary. **Fix.** `shrt version` and `shrt doctor`; rebuild or reinstall the binary, then
`shrt init -force -build=false` to refresh `.shrt/docs/` and the agent kit.

## 4. A run says `error`, and you blame the backend

**Cause.** `error` usually means no request was sent: a reference or login body did not resolve, an
env var is unset, or the login failed. Less often the transport failed after sending or the answer
was not JSON. **Fix.** Read the step's `error` line before the step name. `failed` is evidence
about the server; `error` is almost always about the fixture.

## 5. `shrt run` refuses with `env NAME is not set` although a cached token is valid

**Cause.** The token cache is keyed by target, login rpc and resolved login body, so the body must
resolve before the cache is read. **Fix.** Export the variables `auth.body` in
`.shrt/config.yaml` names. There is no cache-only path.

## 6. Exit 3 from `run` or `verify`

**Cause.** No verdict: the backend was unreachable, answered unavailable, restarted mid-run, a
fixture was reused, or authentication was refused. The output says which and ends in `re-run`.
**Fix.** Re-run once (with a fresh `-var tag` if the line says so). Do not count 3 as red or green.

## 7. A streaming rpc is skipped, or refused

**Cause.** A backend's auth interceptor often wraps unary calls only, so a server-streaming rpc
(`WatchInvoice`) can answer with no token at all. **Fix.** `shrt contract plan WatchInvoice` plans
the happy call (`messages.0`) and the two token probes; `contract status -gaps` lists it until a
chain calls it. Client- and bidi-streaming rpcs are refused: test those by hand.

---

# Writing chains

## 8. `not_empty` where `exists` was meant

**Cause.** `not_empty` is false for `""`, `0`, `false`, `[]`, `{}`, and for an int64 `"0"`. On a
count it asserts non-zero, not present. **Fix.** Use `exists: true` for presence; see the truth
table in `GRAMMAR.md` §1.

## 9. `exists` cannot tell zero from unset

**Cause.** A proto3 scalar without `optional` sends nothing for `false`, `0`, `""` or an empty
list. **Fix.** Assert the value (`equals: false`) or the emptiness (`items.0 exists: false`). Mark
the field `optional` in the proto if presence matters.

## 10. Two rules on one expectation are rejected

**Cause.** One entry holds one rule; a second would replace the first. **Fix.** Repeat the path in
a second entry.

## 11. `${uuid}` in `vars:` is rejected

**Cause.** `vars` values are not resolved. **Fix.** Put `${uuid}` in the body that needs it, or
pass the value with `-var key=...`.

## 12. A green `-dry-run` and a red first real run

**Cause.** Dry run has no responses, so references resolve to scaffold values (`""`, `"0"`). It
proves the chain's shape and step 1's body, not bodies built from references. It also does not run
lint. **Fix.** `chain lint` first, then a real run.

## 13. An expectation comparing a number fails on equal-looking values

**Cause.** `equals` compares text and does no arithmetic: `${a.qty}+${b.qty}` is the text `0+5`.
`${nowunix}` is a string of digits. **Fix.** Work the result out from the inputs you chose and
assert it as a literal or a var. Compare clock values with `within`, `between`, `gt`/`lt`.

## 14. A clock assertion flakes at a second boundary

**Cause.** `equals: ${nowunix+3600}` against a backend stamp one second off. **Fix.**
`within: {of: "${nowunix+3600}", by: 5}`.

## 15. A step asserting only `error.code == OK` passes whatever the call did

**Cause.** The envelope says the server did not crash. **Fix.** Assert what the call produced: a
value read back, a state, a count, an invariant against an earlier step. `chain lint -strict` fails
such a step (`envelope-only`) when its contract declares response facts.

## 16. A read passed and the response was empty

**Cause.** A green envelope on an empty body. **Fix.** `shrt chain hollow` names every such read
from the run records. Assert what the read should find. Where empty is correct, add a line
`<chain> <step-id> <reason>` to `.shrt/hollow-allow.txt`; an entry without a reason is refused.

## 17. A step fails with an `envelope` entry although its expectations held

**Cause.** The call was refused in-band (or sent no verdict) and no expectation pins the verdict,
so `qty equals: 0` held only on the zeros a refusal leaves. **Fix.** Assert `equals: <ok>` on the
envelope for a call that must succeed, or the refusal code for one that must be refused. A
`not_equal: ""` or a rule on a sibling such as `message` pins nothing.

## 18. A refusal probe collects `is required ... while expecting success` errors

**Cause.** Lint treats a step as a refusal probe only when it pins a refusal on the envelope path
(`not_equal: <ok>` or `equals: <code>`) or on `transport.code` / `transport.http_status`.
`allow_fail` and an `app_code` alone do not count. **Fix.** Add the envelope or transport line; do
not fill the fields the probe omits on purpose.

## 19. A Connect refusal fails every body assertion as `unevaluated`

**Cause.** A 4xx Connect error has no response message. **Fix.** Assert `transport.code`,
`transport.http_status` and `transport.message` (`GRAMMAR.md` §1).

## 20. `allow_fail` did not let the chain go on

**Cause.** It tolerates only a transport refusal on a step with no expectations, never a failed or
`error` step. **Fix.** Assert the refusal instead, or run `shrt run -keep-going` to see what lies
behind the first red.

## 21. A hand-written `Authorization` header is a lint error

**Cause.** The auth middleware would overwrite it. **Fix.** Declare a profile under `auth.profiles`
and put `auth: <profile>` on the step. Probe missing and invalid tokens with `skip_auth: true` and
`auth: invalid`.

## 22. A login step in the chain did not seed the profile's token

**Cause.** A login seeds a profile only when it sent that profile's `body` exactly. **Fix.** Log in
with the profile's credentials, or let shrt log in by itself.

## 23. The second run of a chain is refused as a duplicate

**Cause.** A unique field is a literal, or built from a var left at the value an earlier run used.
**Fix.** Build unique values from `${vars.tag}` and leave `tag` undeclared, so each run gets a
fresh one; a declared `tag:` needs a fresh `-var tag=...` per run. `fixture reused` / `fixture
collision` (exit 3): re-run with a fresh var. `CHAIN DEFECT: the chain collides with itself`
(exit 1): rebuild the literal field it names from a var. An idempotency key must be `${uuid}`
(lint: `literal-idempotency-key`).

## 24. A prefix list counts another run's fixtures

**Cause.** A prefix ending at the var (`inv-${vars.tag}`) matches `inv-${vars.tag}0-...` of tag
`x0`. **Fix.** End it with the separator every fixture carries (`inv-${vars.tag}-`). Lint warns
`unterminated-prefix`.

## 25. An exact count passes once and fails on the next run or another database

**Cause.** The list is not scoped to the run. **Fix.** Filter it by something the run created, or
assert membership (`includes:`) and a lower bound. Lint warns `unscoped-count`.

## 26. A `-var` is ignored with a warning, or refused as a typo

**Cause.** A var the chain never reads is dropped with a `warning:`; a name within two edits of a
real var is refused so a typo cannot collapse runs onto one key. **Fix.** Check the spelling; the
refusal lists the vars the chain reads.

## 27. A scratch chain run by path is refused

**Cause.** Its `name:` is that of a chain under `paths.chains`, so its runs would count as that
chain's. **Fix.** Rename it (`name: <name>-scratch`).

## 28. Perturbing a var for a drift test broke the next replay

**Cause.** The var reached shared state that other chains read. **Fix.** Perturb only values the
chain's own tag isolates.

---

# Contracts and plans

## 29. A planned chain lints, runs green and means nothing

**Cause.** `plan` wires what the contracts say; it cannot judge business sense. **Fix.** Read the
`order:` line as a claim about the flow. A missing step means a missing `needs:`/`from:` in the
contract: fix the contract and re-plan, rather than adding the step by hand. A `gap:` saying an rpc
`says nothing of` a number means nothing is asserted after it: paste the `effects:` it prints.
`summary` is prose for people; wording it differently does not help.

## 30. The plan leaves a numeric zero that lint accepts

**Cause.** Lint cannot tell a scaffold `"0"` from a deliberate zero. Only the plan header reports
it (`still carries the scaffold's numeric zero`). **Fix.** Fill it, or say `value: "0"` in the
contract. A field `note:` does not silence it.

## 31. Quality score 0, and plans that cannot compose a chain

**Cause.** The score counts what is present; an incomplete `needs:` scores the same as a complete
one, and `before:` has no term. **Fix.** Run `shrt contract plan <read>` for every read and ask
whether that order could have produced the row the read returns. A one-step order means no
producer: add `needs:` or `before:`, or `no_producer:` if nothing in this API writes it.

## 32. A field stays unfilled and no `from:` can reach it

**Cause.** `from` reads a response path; the value exists only in a request. **Fix.** `same_as:
<rpc>->request_path`.

## 33. Two supposedly independent entities turn out to be one

**Cause.** An `@alias` the target never declares under `aliases:` makes a second identical step.
**Fix.** Declare the alias with the fields that make it differ.

## 34. A `before:` edge points at an rpc that always refuses

**Cause.** Edges name their target by string; nothing checks it still works. **Fix.** When an rpc
is retired, grep the overlays for its name and re-point the edges.

## 35. `contract lint` says a failure repeats another

**Cause.** Same code, reason and field. **Fix.** If they are different branches, give each its own
`field:`; do not merge them.

## 36. A backend code no chain can send

**Cause.** Every request is validated against the descriptor, so a body the proto cannot express
never leaves. **Fix.** Declare the failure with `unreachable:` and the reason.

## 37. A code the backend raises is in no contract

**Cause.** Contracts are hand-maintained. **Fix.** Script a comparison of your backend's error
constructors against every `failures:` entry, per rpc and domain-wide, and run it in your gate.

## 38. A `source:` path nobody can find

**Cause.** Written from memory, or a `:line-range` that drifted. **Fix.** Resolve every path before
committing; anchor on function names, not line numbers.

## 39. Batch steps pass while every line was refused

**Cause.** `conventions.item_envelope_path` is unset. **Fix.** Set it to the per-item verdict
(`results[].error.code`) and check it once against a response you know refused a line. `doctor`
warns when it sees an unconfigured per-item verdict.

## 40. Every assertion misses the envelope

**Cause.** The backend reports its verdict somewhere other than `error.code`. **Fix.** Set
`conventions.envelope_path` and `envelope_ok` (`PLAYBOOK.md` §3b); `doctor` names the path the
responses carry.

---

# Runs, safe spots and verify

## 41. `confirm` refuses a run

**Cause.** The run did not pass, its record was edited (seal mismatch), or the chain is kept red.
`-approve` also refuses when the chain file now differs from the one the proposed run ran (another
branch checked out). **Fix.** Run the chain again and propose the new run; approve on the branch the
proposal came from.

## 42. `confirm` warns that fields differ from the earlier passing run

**Cause.** Values that change every run and are not id-, timestamp- or fixture-shaped. **Fix.**
Declare them `volatile` (or the list `unordered` when only its order changes), re-run, propose
again, unless the difference is real. Run a chain twice before proposing so the check is made.

## 43. Verify fails on a volatile pattern you just added

**Cause.** The safe spot stores the patterns it was approved with; a wider mask hides values the
approver saw. The same holds for a new `redact` pattern. **Fix.** Propose a run under the new mask
with `-supersede` and have it approved.

## 44. A `volatile` field that became null or disappeared is reported

**Cause.** A volatile pattern tolerates a changed value, not a lost one. `null` to absent loses
nothing and stays masked. **Fix.** Treat it as a real change.

## 45. `order changed`, or `same items in another order`

**Cause.** The list holds the safe spot's items in another order. **Fix.** Declare
`unordered: [<list>]` only if its order varies between runs of one release; otherwise, or when a
positional expectation fails, it is a regression.

## 46. A step-level `volatile` did not mask another step

**Cause.** Step patterns apply to that step only; config and chain patterns apply to all. **Fix.**
Declare it where it belongs.

## 47. A timestamp change is reported although `*_at` is masked

**Cause.** It changed unit (seconds to milliseconds) or jumped outside 400 days of its run.
**Fix.** Treat it as a real change.

## 48. `FINDING: intermittent failure at <rpc>`

**Cause.** A server error on a request answered on its one re-send (reads only, judged on that
answer), elsewhere in the run or in the previous run; in the gate, also one failing every Nth
call, reported on every chain it explains. A step whose suspect is that call (a read missing the
refused write) counts with it. **Fix.** A real backend defect (exit 1), just not deterministic.

## 49. A token refused long before the expiry its login stated (`note:` or `WARNING:` line)

**Cause.** Sessions end before their stated expiry, or the backend restarted. **Fix.** A short
chain whose reads carry `wait:` between the suspected and the stated lifetime, twice after the
login: early expiry prints `FINDING: token refused ...` (exit 1). Keep it out of the per-commit gate.
Build it of the login and reads whose request carries `${vars.tag}`, with no write: `shrt gate`
then runs it beside the other chains instead of adding its waits to the gate's time.

## 50. `drift after a chain change` or `drift with different input`, not `regression`

**Cause.** The chain file or its vars changed since approval. **Fix.** Restore the input, or bring
the chain in line, run it green and propose it with `-supersede`.

## 51. Renamed a chain and lost its safe spot

**Cause.** A safe spot belongs to the chain name. **Fix.** `shrt confirm <new> -rename-from <old>
-by <email>` for a pure rename.

## 52. A merge conflict in `.shrt/safespots/<chain>.json`

**Cause.** Two branches each superseded it. **Fix.** Take one side whole (`git checkout --ours` or
`--theirs`), run `verify` on the merged backend, and re-propose if it drifts (`PLAYBOOK.md` §8).
Never hand-merge the JSON.

## 53. `shrt diff` says no differences, and both runs were wrong

**Cause.** `diff` compares two runs; it is not a verdict. **Fix.** Use a safe spot for a verdict.
`shrt diff <c>` skips `verify` replays; pass run ids to compare them.

## 54. The response carries fields the proto does not declare

**Cause.** The backend is newer than the proto. They are dropped and named in a warning; with
`validate_output: true` the step fails with `"drift": true` and asserts nothing. **Fix.** Update the
proto and `shrt catalog build` if they should be compared; otherwise rebuild before reading drift
as a backend defect.

## 55. A secret still appears in a run record

**Cause.** Redaction covers `redact` paths and known secrets by value, including common encodings,
not a secret hashed, reversed or encoded twice, nor a credential in a header whose name does not say
so. **Fix.** Cover the field with `redact`, read credentials from env vars named like credentials,
and keep such echoes out of committed runs.

## 56. A kept-red chain says `PINNED DEFECT GONE`

**Cause.** The defect no longer reproduces: a fix was deployed, or the target runs another build.
**Fix.** Check the `build` of the run first (`target.build_header` or `run -build`). If the fix is
real, remove `kept_red`, run, propose. Never confirm a chain while it has `kept_red`.

## 57. A kept-red chain says `FAILED, NOT AS PINNED` or `NEW FAILURE outside the pinned defect`

**Cause.** Something else failed, or a pinned step now returns something different from the last
run that failed as pinned. **Fix.** Treat it as a regression; re-pin (`shrt chain pin`) only when
the change is understood.

## 58. One real defect keeps a long chain from a safe spot

**Fix.** `shrt chain pin <c>` keeps the defect red in a slice of its own; confirm the rest
(`PLAYBOOK.md` §9).

---

# Slices and search

## 59. A slice went green where the chain was red

**Cause.** It dropped a write whose state the target needed. **Fix.** Always `-verify`; follow its
`next:` line, which keeps the writes it names. Read `WARNING possible under-inclusion`.

## 60. A slice landed in the directory every gate runs

**Cause.** `-write` was given a path under `paths.chains`; without a path it writes
`.shrt/scratch/`, which no sweep reads. **Fix.** Move it to `.shrt/scratch/` and run it by path, or
keep the defect red with `shrt chain pin`, which writes its slices beside the chain on purpose.

## 61. `-verify` refuses up front asking for `-var name=<fresh>`

**Cause.** A kept write interpolates that var into what it creates, and its earlier values already
exist on the backend. **Fix.** Pass a value never used before.

## 63. "220 of 229 steps kept" read as 220 passing steps

**Cause.** The count is what the file holds. **Fix.** Run the rest before proposing it.

## 64. `chain which` lists a match you cannot trust

**Cause.** `asserted` means a chain claims it; only `OBSERVED` means a local run record reached the
step, and run records are machine-local. **Fix.** Run the chain, then query again; paste the printed
`reproduce:` line rather than retyping it.

## 65. `chain hollow` counts runs of a chain you deleted or renamed

**Cause.** Run records are evidence and are never deleted for you. They are listed apart as
`orphan` and excluded from the counts. **Fix.** Delete `.shrt/runs/<name>/` when done with it.

## 66. A gate of `lint && run` is green on chains that prove nothing

**Cause.** Plain `chain lint` exits 0 on assertion-quality warnings. **Fix.** Gate on
`chain lint -strict`, or run `bash .shrt/ci-gate.sh`, which runs it (`shrt gate` alone does not).
