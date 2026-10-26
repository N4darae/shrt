# shrt

shrt tests a backend through chains of API calls. A chain is a YAML file: log in, create an item,
add stock, read it back. You run the chains, a person approves a passing run as the baseline, and
every later release is replayed against that baseline. When a field changes, shrt names the step,
the field, and the write it suspects.

It was built to be driven by a coding agent (`shrt init` installs a Claude Code skill and its docs),
but every command works by hand, and the demo below is all by hand.

shrt speaks the Connect protocol: each call is JSON over HTTP POST. A server built with
[connect-go](https://connectrpc.com/docs/go/getting-started) answers it, since connect-go serves
Connect and gRPC side by side. A server that speaks only gRPC (binary frames over HTTP/2, as plain
grpc-go does) does not. Unary and server-streaming rpcs are supported; a streaming step reads the
first message.

## Install

You need Go 1.26 or later, and [buf](https://buf.build/docs/installation) on your `PATH`: shrt
compiles the backend's `.proto` files with `buf build` to learn every rpc and message.

```bash
go install github.com/N4darae/shrt/cmd/shrt@latest
shrt version
```

Or build from a checkout with `go build -o shrt ./cmd/shrt` and put the binary on your `PATH`.

## A ten-minute demo

The demo runs shrt against `examples/inventory`, a small connect-go service with four rpcs: `Login`,
`CreateItem`, `GetItem` and `AddStock`. Started with `-release 2`, it serves `GetItem` from a stale
cache, and that is the regression the demo catches.

Copy the example out of the checkout first. `shrt init` writes `.shrt/`, `.claude/` and a
`.gitignore` into the directory it runs in.

```bash
git clone https://github.com/N4darae/shrt
cp -r shrt/examples/inventory /tmp/inventory-demo
cd /tmp/inventory-demo
```

### 1. Start release 1

```bash
go build -o inventory . && ./inventory &
```

It listens on `127.0.0.1:8080`, which is also where `shrt init` looks by default. If that port is
taken, start it with `-addr 127.0.0.1:9090` and pass `-base-url http://127.0.0.1:9090` to `shrt init`.

### 2. Set up shrt

```bash
export API_USER=demo API_PASSWORD=demo-password
shrt init
shrt doctor
```

`init` builds the descriptor from the protos, guesses which rpc is the login, and logs in once to
see how responses report success. Two of its lines say what it found:

```
write .shrt/config.yaml auth: inventory.v1.AuthService/Login, GUESSED from the descriptor: check it and the credential variables it names
write .shrt/config.yaml conventions: envelope_path status.code, envelope_ok OK (read from one Login answer)
```

Credentials stay in the environment. The config refers to them as `${env.API_USER}` and
`${env.API_PASSWORD}`, and passwords and tokens are redacted from every run record.

### 3. Add the contract

A contract says what each rpc should do: which fields it requires, where each value comes from, how
it refuses, and what a write does to numbers. `shrt contract init -all` writes a skeleton full of
TODOs, which you (or an agent) fill in from the service's code. The example ships a filled-in one:

```bash
shrt contract init -all
cp contracts/inventory.yaml .shrt/contracts/
shrt contract lint
```

The line that matters most for this demo is in `AddStock`: `effects: {qty_on_hand: {increase: qty}}`.
It tells the planner that stock read after an `AddStock` should have grown by `qty`.

### 4. Let the contract write the chains

```bash
shrt contract plan -all -write
shrt contract plan GetItem@before AddStock GetItem@after -write -name stock-flow
```

The first command writes one chain per rpc. Each one calls its rpc the normal way and then probes
it: the boundary values in the contract, a missing token and a bad one, an id nobody created, a
duplicate sku. The second composes a flow that reads an item, adds stock, and reads it again,
asserting the new stock from the `effects` line. The planner wrote all five chains, 83 steps
between them.

### 5. Run everything and approve the baseline

```bash
shrt gate
shrt confirm -all -note "release 1 of the inventory demo"
shrt confirm -all -approve -by you@example.com
```

`gate` sends every chain once, and all five pass. `confirm -all` proposes each passing run as a
chain's safe spot and prints a table of what was run and which refusals each chain asserts. Only a
person approves: an agent driving shrt is told to show that table and wait for a yes. Once approved,
each response field becomes part of the baseline, including fields no assertion checks.

### 6. Ship a regression

```bash
kill %1
./inventory -release 2 &
shrt gate
```

```
PASS       inventory-addstock
PASS       inventory-createitem
PASS       inventory-getitem
PASS       inventory-login
FAIL       stock-flow            regression: get_item_after (ItemService/GetItem) item.qty_on_hand want=5 got=0; unclear: write add_stock (ItemService/AddStock) or the read
...
shrt gate: FAIL: 1 of 5 chain(s) failed; details: shrt verify <chain>, or shrt run <chain> -keep-going
```

Restarting the service threw away the token shrt had cached. The gate notices the refusal and may
hold a fresh token for 30 seconds to tell a restart from sessions that end too early; that is the
pause you see. The gate exits 1, so in CI it fails the build.

`shrt verify stock-flow` shows the evidence. `AddStock` answered 5, but the read after it still says
0:

```
stock-flow: DRIFT (regression), 1 step(s) changed vs safe spot ...; first: get_item_after (ItemService/GetItem) item.qty_on_hand want=5 got=0; unclear: write add_stock (ItemService/AddStock) or the read: answered qty_on_hand=5, but GetItem read 0
  add_stock sent {"id_item":"item-16","qty":"5"}
  [get_item_after] changed    item.qty_on_hand want=5 got=0
```

Stop the service with `kill %1` when you are done. In a real repository you commit `.shrt/config.yaml`,
the contracts, the chains and the safe spots, and run `.shrt/ci-gate.sh` (written by `init`) in CI
against each new release.

## Keeping it away from production

Chains create records and send deliberately bad requests, so pointing one at production would
write test data there. shrt only sends to this machine (`localhost`, `127.0.0.1`, `::1`) unless
the host is listed in the config:

```yaml
target:
    base_url: https://staging.example.com
    allow_hosts: [staging.example.com]
```

With a host missing from that list, `run`, `verify`, `gate` and `init`'s login all refuse before the
first call and print the line to add. `run -dry-run` still works, because it sends nothing. The same
check applies to `target.host_override`. shrt cannot tell staging from production by itself, so list
only backends whose data you are willing to have changed.

## Using it with an agent

`shrt init` installs `.claude/skills/shrt/SKILL.md`, a subagent that writes contracts, and the agent
docs under `.shrt/docs/`. The sources of those docs are in [`docs/agent/`](docs/agent/): the command
reference, every config and chain key, the procedures, and a symptom-to-fix list. The design and its
limits are in [`docs/OVERVIEW.md`](docs/OVERVIEW.md).

Every command prints its flags and exit codes with `-h`.

## License

MIT. See [LICENSE](LICENSE).
