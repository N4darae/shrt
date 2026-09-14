# shrt

Snapshot the valid API call chains of a gRPC backend so an agent can look one up
instead of tracing for it.

## Why

A backend with 300 RPCs has flows that run 5 to 15 calls back to back, which comes
out to something like 30 to 50 valid chains. When an agent needs to change one of
those chains or reproduce it, it starts at the last call and traces backwards. That
works until the chain branches. Once the trace dead ends, the agent spends context
and tokens hunting for the rest of the path, and pays that cost again next time.

## How it works

shrt reads the server's descriptor set, so every method and message type comes from
the server itself. It then calls the real RPCs, records what ran, and writes a
chain into the contract.

Nothing enters the contract on inference alone:

- A chain is replayed before it is accepted.
- Each data edge between steps is perturbed. Change the value at the source step and
  the chain has to break. If it still passes, the edge was a coincidence and gets
  marked unverified.
- Every step carries where it came from, either a trace id or a test location.

A contract is a snapshot taken at a point in time. Diffing descriptor sets between
snapshots tells you exactly which chains a proto change affects, so only those get
replayed.

## Contract

The format is proto based. A chain is an ordered list of steps; a step is a method
reference, its inputs, assertions, and provenance. An input is either a literal or a
reference to an earlier step's output, which is the part that makes a chain
reproducible rather than just a list of calls.


## Limits

Unary RPCs only. Streaming methods are recorded as unsupported.

Steps with external side effects (payments, mail, third party calls) are flagged and
skipped on replay. Chains containing them need a test environment that can be reset.

Coverage is whatever was exercised during recording. shrt does not discover chains
nobody has run.
