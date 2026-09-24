# shrt

Snapshot the valid API call chains of a Connect/gRPC backend so an agent can look one up
instead of tracing for it.

## Why

A backend with 300 RPCs has flows that run 5 to 15 calls back to back, which comes
out to something like 30 to 50 valid chains. When an agent needs to change one of
those chains or reproduce it, it starts at the last call and traces backwards. That
works until the chain branches. Once the trace dead ends, the agent spends context
and tokens hunting for the rest of the path, and pays that cost again next time.

## How it works

shrt builds a descriptor set from the backend's proto sources (`shrt catalog build`, which runs
`buf build` by default), so every method and message type comes from the protos the server is built
from. It calls the backend as unary Connect requests: JSON over HTTP POST.

A chain is a YAML file under `.shrt/chains/`. It is an ordered list of steps, and a step names an
rpc, its request body, its assertions and what it exports. A body value is a literal or a `${...}`
reference to an earlier step's request or response, an export, a chain var, an environment
variable, a fresh uuid or the clock. The references are what make a chain reproducible rather than
just a list of calls.

Chains are written, not recorded from traffic. `shrt contract plan` composes one from the curated
contract in `.shrt/contracts/<domain>.yaml`, which holds what the descriptor cannot: which fields
the server requires, where each value comes from, which calls must run first, and how each rpc
refuses. `shrt chain new` scaffolds one from the descriptor alone.

`shrt run` executes a chain in order and writes a run record under `.shrt/runs/`. An agent proposes
a passing run with `shrt confirm <chain> -note`, which prints a summary to show the user; when the
user says yes, `shrt confirm <chain> -approve -by <their email>` makes it the chain's safe spot. `shrt verify` replays the chain and
diffs every response field against the safe spot, so a regression names the rpc that changed.
Before a chain has a safe spot, `shrt diff` compares two of its recorded runs.

## Limits

Unary RPCs only. `shrt catalog ls`, `shrt catalog describe` and `shrt contract show` mark a
streaming rpc as out of scope, `shrt chain new` refuses to scaffold one, and `shrt chain lint`
rejects a step that calls one, and `shrt run` refuses a chain with one before sending anything.

Every step runs, in order. shrt has no notion of an external side effect, so a chain that takes
payments, sends mail or calls a third party needs a test environment that can absorb it or be reset.

Coverage is whatever the chains exercise. shrt does not discover chains nobody has written;
`shrt chain which` says which existing chains exercise an rpc or assert a failure code.

The first design also planned to perturb each data edge to prove it was not a coincidence, to
record a trace id or test location on every step, and to diff descriptor sets to choose which chains
to replay. None of those is implemented.
