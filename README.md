pytanie naukowe: minimalny koszt obliczeniowy potrzebny do wiarygodnego badania algorytmów load balancingu

Jaki jest minimalny poziom szczegółowości symulacji sieci, który pozwala wiarygodnie oceniać i porównywać algorytmy load balancingu?
Punkt odniesienia: ns-3 z dokładnym TCP, kolejkami, przepustowością i opóźnieniami.

Twój symulator: stopniowo usuwasz lub upraszczasz mechanizmy — TCP congestion control, jitter, retransmisje, kolejki, model serwera itd.

Porównanie: sprawdzasz, kiedy wyniki zaczynają znacząco odbiegać od ns-3.

Optymalizacja: identyfikujesz najtańszy obliczeniowo model, który zachowuje wiarygodność.

# Intelligent load balancer

This repository contains both parts of the project:

- `simulator/` - Go backend, including `main.go`, `go.mod`, and all Go packages.
- `visualizer/` - React, TypeScript, and Vite frontend.

## Code organization

The simulator keeps one owner for the world and reads it synchronously before
publishing detached data. Packages under `simulator/` divide the work as follows:

| Location                                                     | Responsibility                                                                                  |
| ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------- |
| `engine/`                                                    | Ordered events, virtual time, cancellation and execution limits.                                |
| `network/`, `client/`, `server/`, `balancer/`, `algorithms/` | Links and queues, transport endpoints, forwarding, backend selection and local measurements.    |
| `model/`                                                     | Native shared packet, connection and measurement types.                                         |
| `scenario/`                                                  | Scenario documents, exact integer encoding, validation and normalization.                       |
| `internal/simulation/`                                       | World construction, client arrivals, wiring, RNG, typed operations and virtual-time sampling.   |
| `internal/presentation/`                                     | Snapshot and CLI report projection, metric descriptions, display units and transfer aggregates. |
| `internal/contract/`                                         | The existing HTTP, WebSocket and CLI data types and commands.                                   |
| Application files in `simulator/`                            | Start/Stop, admission, run lifecycle, command handling, pacing and wall-time measurement.       |
| `http/`                                                      | HTTP and WebSocket routing, encoding and delivery of prepared data.                             |

`internal/contract` describes the current frontend and CLI interfaces. The shared
control-plane contract and adapters for other balancer implementations remain
future work.

## Run with Docker Compose

Install Docker Desktop with Linux containers and start it. From the repository
root, build and start both applications:

```sh
docker compose up --build
```

In PowerShell, you can also run `./start.ps1` from the repository root. The script
builds both images and starts the services in the background.

Open `http://localhost:5173`. The backend is available at `http://localhost:8081`
and its WebSocket at `ws://localhost:8081/ws`. Set the initial configuration in
the frontend and click **Start simulation**. Starting the containers or opening
the page does not start an experiment. Press `Ctrl+C` to stop both applications.
Subsequent starts can reuse the built images:

```sh
docker compose up
```

There are no source bind mounts or automatic rebuilds. After changing source
files or dependencies, run `docker compose up --build` again. To rebuild only
the backend, use `docker compose up --build -d backend`. To run everything in
the background, use `docker compose up -d`; stop and remove containers with
`docker compose down`. Read logs with `docker compose logs -f`.

The frontend is built into static files and served by `local-web-server` inside
a Node.js container. Frontend and backend publish separate local ports; the
browser connects directly to the backend WebSocket and reconnects automatically
when the backend restarts. Dependencies and build tools are installed inside
the images.

To use different ports in PowerShell:

```powershell
$env:FRONTEND_PORT = '5174'
$env:BACKEND_PORT = '8082'
docker compose up --build
```

The backend WebSocket URL is passed to Vite during image build. Changing
`BACKEND_PORT` therefore requires rebuilding the frontend. Compose maps host
port 8081 to port 8080 inside the backend container by default.

## Run locally without Docker

Requirements: Go 1.23.2 or newer, Node.js 24, and npm.

## Run the simulator

From the repository root:

```sh
cd simulator
go run .
```

The Go module remains named `load-balancer`, so package imports stay the same.
The backend starts in an idle state and sends the Go configuration defaults to
the frontend. Edit the form and click **Start simulation** to create and run an
experiment. The default is 200 clients, 10 backends, 10000 segments per client,
120 virtual seconds and speed 1. Set speed to 0 in the form to run without pacing.
Go runtime tracing is optional: `go run . -trace trace.out`.

The UI shows idle, running, finished or failed status and the virtual clock.
After completion, the final snapshot remains available and another Start creates
a fresh experiment. **Stop and clear** interrupts a run and discards its event
queue, links, connections and counters. It also clears a finished experiment;
the form configuration is preserved for the next Start.
Reconnecting or refreshing the page restores the current
state without starting or restarting the simulation.

For a quick run without HTTP, from `simulator/`:

```sh
go run . -headless -clients 6 -servers 2 -bytes 2050 -duration 1s
```

`-headless` disables pacing, prints a JSON result and exits. The result includes
completed and incomplete streams, closed clients, executed events, final backend
state and per-link queue/loss counters. `ReceivedPayloadBytes` counts unique
payload accepted by backends; `AcknowledgedPayloadBytes` counts payload confirmed
to clients by ACK. Both include unfinished transfers and retain historical work
after connections close. Headers, control packets and repeated data are excluded.
`CompletedPayloadBytes` still counts only fully completed streams.
`Transfers` separates succeeded, failed and still-active attempts from scheduled
clients not yet created. It includes failure reasons, cumulative SYN/data retry
counts and virtual duration summaries with sample count, mean, minimum and maximum.
`VirtualTime` describes the model;
`WallTime` describes the cost of running it. Clients still active at the end time
remain in the final state; reaching that time does not imply successful completion.

### Run a full JSON scenario from CLI

The same scenario exported from the frontend can run directly from a file.
From the repository root, for example in PowerShell:

```powershell
cd simulator
$scenario = '../examples/scenarios/reference-near-fast-ack-short.json'
go run . -scenario $scenario -output run.json
```

`-scenario` starts one experiment immediately, without HTTP, WebSocket or the
frontend. It always runs without pacing, even if the document requests a positive
speed. The runner uses the same strict decoder, validation and normalization as
the scenario API, including decimal strings for integers outside JavaScript's
safe range. Profiles, directional links, groups and individual overrides remain
part of the full configuration.

With `-output`, the report goes into that file and stdout stays empty. Omit
`-output` to print the report as one JSON document to stdout. Output and trace
paths must be new files in this mode; existing files are not overwritten. The
parent directory must already exist.

Scenario settings come entirely from the document. Combining `-scenario` with
flags such as `-seed`, `-duration`, `-clients` or `-speed` is an error, including
when their values equal the defaults. For a single run, process flags `-scenario`,
`-output`, `-headless` and `-trace` may be combined. `-headless` is optional,
and `-output` requires `-scenario`. The older `-headless -clients ...` command
continues to print its original result format.

The version-1 report groups the result with information needed to repeat it:

| Field      | Meaning                                                            |
| ---------- | ------------------------------------------------------------------ |
| `version`  | Report schema version, currently `1`.                              |
| `status`   | Runner outcome: `finished`, `failed` or `cancelled`.               |
| `error`    | Error message for a failed or cancelled run, when present.         |
| `manifest` | Model, input, effective execution settings and build metadata.     |
| `result`   | The existing CLI result, including transfer outcomes and counters. |

`manifest.scenario` retains the canonical input document, including its original
requested speed. `scenarioSha256` is the SHA-256 of its compact canonical JSON,
so formatting changes to the source file do not change this identifier.
`manifest.execution` records `headless: true`, the effective `speed: 0` and
whether tracing was enabled. `manifest.model` identifies the current
`tcp-byte-stop-and-wait-v1` profile. Build metadata records the Go/module versions,
OS and architecture; VCS revision and modification status appear when available.

Invalid input or configuration returns a nonzero exit code without a report.
An execution limit also returns nonzero, but saves a `failed` report containing
the work completed so far. Cancellation saves the partial result with
`status: cancelled` and returns nonzero. `finished` means the runner reached the
virtual end time; individual transfers may still have failed or be incomplete.

The runner keeps the document's sampling interval and samples virtual traffic
without building frontend snapshots. As in the original headless mode,
`result.WallTime` measures execution after world construction and before report
assembly/encoding. It excludes loading, validation and world setup; it is not
the same measurement boundary as the interactive UI run.

### Run a series with different seeds

From `simulator/`, provide the scenario, an ordered seed list and a new directory:

```powershell
$scenario = '../examples/scenarios/reference-near-fast-ack-short.json'
go run . -scenario $scenario -seeds '1,2,3' -output-dir runs-001
```

Each seed starts a fresh world and produces its own `run-0001.json`,
`run-0002.json`, and so on. Runs execute sequentially without pacing. The input
file is loaded once and stays unchanged. Each report uses the single-run format
above; its scenario and checksum include the seed actually used for that run.

Seeds are comma-separated, signed base-10 `int64` values. Surrounding whitespace
is allowed. Order and duplicates are preserved; for example,
`-seeds '-7,9007199254740993,-7'` deliberately repeats the first seed. Ranges
and an automatic repeat count are not supported. `-seeds` and `-output-dir`
must be supplied together with `-scenario`. Series cannot use `-output` or
`-trace`. The output directory must be new, with an existing parent directory.
Invalid options, seeds or input are rejected before creating it.

The runner updates `index.json` as it progresses and prints the final index as
one JSON document to stdout. The index contains:

- `version`, overall `status`, and an optional `error`;
- `manifest`, identifying the original base scenario and executable;
- ordered `runs`, each with an `id`, exact decimal-string `seed`, and `status`;
- a relative `report` path only after that run's report was saved successfully,
  plus its `error` when applicable.

Run statuses are `notStarted`, `running`, `finished`, `failed`, or `cancelled`.
Execution failures such as event limits save partial reports and allow later
seeds to run; the series then returns a nonzero exit code. Cancellation or an
I/O failure stops the series, retains existing reports and progress, and leaves
remaining runs as `notStarted`. The series does not yet calculate aggregate
statistics or confidence intervals.

Today, seeds mainly affect random packet loss and `Random` backend selection.
With round-robin, zero loss and no other random operations, different seeds
produce the same virtual results. Duplicate seeds check repeatability rather
than supplying independent replications. `WallTime` can still vary between runs.

### Simple CLI settings

CLI flags provide the form defaults, and configure immediate headless runs.
The browser sends its selected configuration when Start is clicked. Useful
parameters (see `go run . -help` for all options):

| Option                                  | Default              | Meaning                                                                                              |
| --------------------------------------- | -------------------- | ---------------------------------------------------------------------------------------------------- |
| `-clients`, `-servers`, `-packets`      | `200`, `10`, `10000` | Initial clients, backend count (at most 10000) and full segments per stream.                         |
| `-bytes`                                | `-1`                 | Override stream payload size, including a partial final segment; `-1` uses `packets * 1024`.         |
| `-duration`, `-seed`                    | `2m`, `1`            | Virtual end time (inclusive) and experiment seed.                                                    |
| `-client-gap`, `-packet-gap`            | `0s`, `0s`           | Virtual inter-arrival interval and sender pause before each data segment.                            |
| `-bandwidth`, `-packet-travel-time`     | `1000000000`, `1ms`  | Bits/s and virtual travel time per access link or outgoing LB link.                                  |
| `-lb-bandwidth`                         | `1000000000`         | One shared forwarding budget in bits/s for all LB outputs, toward both clients and backends.         |
| `-lb-ingress-bandwidth`                 | `1000000000`         | Bitrate of the shared physical link delivering client and backend traffic into the LB.               |
| `-lb-ingress-packet-travel-time`        | `0s`                 | Additional virtual travel time after transmission on the shared input link.                          |
| `-lb-ingress-queue`, `-lb-ingress-loss` | `1024`, `0`          | Waiting slots in each input FIFO (clients and backends), and the shared receiver's loss probability. |
| `-link-queue`, `-loss`                  | `1024`, `0`          | Waiting slots and independent loss on each access or outgoing LB link.                               |
| `-algorithm`                            | `roundRobin`         | `roundRobin` or seeded `random`.                                                                     |
| `-snapshot-interval`, `-speed`          | `100ms`, `1`         | Virtual snapshot interval and virtual seconds per real second; speed `0` is unpaced.                 |
| `-max-events`                           | `50000000`           | Execution limit, including equal-time events, to stop runaway scenarios.                             |

For example, to prefill a shorter experiment in the frontend (then click Start):

```sh
go run . -clients 20 -servers 2 -packets 500 -duration 15s -speed 1
```

The same flags can be passed to the backend image, for example:

```sh
docker compose run --rm --service-ports backend -clients 20 -packets 500 -duration 15s
```

Stop an existing backend container before using `--service-ports` on the same
published port.

## Scenario configuration foundation

`simulator/scenario/` defines version 1 of the scenario configuration:

| Part        | Contents                                                           |
| ----------- | ------------------------------------------------------------------ |
| `execution` | Virtual duration, snapshot interval, seed, pacing and event limit. |
| `network`   | Link defaults and named profiles.                                  |
| `resources` | Load balancer settings and individually configured backends.       |
| `workload`  | Stream defaults, compact client groups and individual overrides.   |

`scenario.Normalize` resolves profiles and overrides into independent values,
validates identifiers and settings, and resolves stream sizes. Patch fields use
pointers so an explicit zero differs from an omitted value. Groups remain compact;
normalization does not allocate one object for every configured client.

The runtime now uses the normalized scenario. Each client gets its own access
and return link when it arrives; each backend also has its own pair. Groups,
profiles and individual overrides can assign different settings in each direction.
The CLI creates one group with shared defaults. The frontend edits one scenario
document, preserving its client groups, profiles and individual overrides.
Its single **Start simulation** button sends that document through HTTP.
Native JSON files can be imported and exported through the frontend's
**Scenario files** panel; ready examples are in [examples/scenarios](examples/scenarios/README.md).

Access links feed two LB input FIFOs: one for clients and one for backends.
Both use one shared physical input link. All LB outputs share a separate
forwarding budget. Both limits remain independent of endpoint link speeds.
`resources.loadBalancer.ingress` configures
the common input; `scenario.DefaultIngressConfig()` supplies its Go defaults.
Each output has one FIFO, configured by the destination's
`network.fromLoadBalancer.queueCapacity`, falling back to `network.defaults`.

The `spawnClient` command uses the group's network and stream settings. Its
optional `groupId` selects that group and is required when more than one group
exists. Spawned clients arrive from the current virtual time, separated by the
group's `clientGapNanoseconds`. Their indices follow the group's initial range,
even when some initial clients have not arrived yet.

Scenario duration fields have explicit `Nanoseconds` JSON names and retain native
`time.Duration` precision in Go. HTTP documents use JSON numbers for safe integers
and decimal strings outside ±(2^53−1), for example `"18446744073709551615"`
for a large event limit. Integer strings are accepted for all integer fields;
unsafe unquoted numbers, fractional integers and unknown fields are rejected.
`scenario.EncodeDocument` and `DecodeDocument` handle this transport boundary;
the model and normalizer continue using native Go types.

## Scenario HTTP API

All routes use the same `scenario.Normalize` rules, including profiles and
directional overrides. POST requests require `Content-Type: application/json`
and one complete scenario object, with a maximum body of 2 MiB.

| Method | Route                               | Result                                                                                       |
| ------ | ----------------------------------- | -------------------------------------------------------------------------------------------- |
| GET    | `/api/scenarios/defaults`           | Initial scenario derived from the Go/CLI settings.                                           |
| POST   | `/api/scenarios/validate`           | Canonical scenario, counts and resolved effective configuration; no model is created.        |
| POST   | `/api/simulations`                  | `202` with `runId` and `status: "accepted"`.                                                 |
| GET    | `/api/simulations/current`          | Run ID, lifecycle status and exact initial scenario; `404` when none exists.                 |
| GET    | `/api/simulations/current/topology` | Latest graph definition with `runId` and `revision`; `404` before publication or after Stop. |

The HTTP and WS paths share one start reservation. A concurrent Start returns
`409`; malformed documents return `400`, invalid settings `422`, oversized bodies
`413`, unsupported content types `415`, and an unavailable command queue/service
`503`. Errors have the shape `{ "error": "description" }`.
Accepted runs belong to the service, so completing or canceling the HTTP request
does not stop them. WS snapshots identify their run with `simulation.runId`.
The current document remains available after completion or failure and reconnect;
**Stop and clear** removes it. Runtime commands do not rewrite that initial document.

For example, with Docker Compose running:

```powershell
$base = 'http://localhost:8081'
$scenario = Invoke-RestMethod "$base/api/scenarios/defaults"
$scenario.execution.durationNanoseconds = 1000000000
$scenario.execution.speed = 0
$scenario.workload.defaults.bytes = 1024
$scenario.workload.clientGroups[0].count = 8
$body = $scenario | ConvertTo-Json -Depth 32

Invoke-RestMethod "$base/api/scenarios/validate" -Method Post -ContentType 'application/json' -Body $body
Invoke-RestMethod "$base/api/simulations" -Method Post -ContentType 'application/json' -Body $body
Invoke-RestMethod "$base/api/simulations/current"
```

The frontend **Scenario library** has 26 presets across links, queues, loss,
workloads, routing, edge cases, performance and reference experiments. Search or filter the catalog;
six cards appear per page. Choose a preset and click **Validate and load preset**
to load it into the form, then use the single **Start simulation** button.
The catalog lives in `visualizer/src/scenarios/presets.ts`, with factories split
into `scenarios/presets/`. The original four topology experiments inherit backend
defaults; the other network and reference scenarios use an explicit repeatable baseline.
Ordinary experiments use 180 virtual seconds, speed 1 and 1 GiB per client,
except deliberately short or empty streams. The active run's exact configuration
can be expanded below the catalog.
The [reference matrix](examples/scenarios/REFERENCE_EXPERIMENTS.md) contains eight
60-second cases with two backends: backend 2 varies between 4/40 ms base travel
RTT and a 1 Gbit/s/64 kbit/s return link, with 16 KiB/1 MiB per client.
Backend 1 stays fixed; all other settings are shared across the matrix.
Editing ordinary fields changes only their part of the scenario. Link settings
edit network defaults; endpoint profiles and directional overrides remain intact.
With multiple groups, changing the total client count redistributes it
proportionally and shows the resulting group counts. Existing configured
backends are retained when resizing the list; new backends use inherited settings.
Validation and Start keep their errors visible and do not change the running model.

The **Simulator throughput** preset measures the simulator's real execution rate:
50 clients, 5 backends, 10 TB/s client links, 100 TB/s backend links and 1 PB/s
shared LB ingress and forwarding limits (decimal bytes per second; the JSON uses
bits/s). Travel times, sender pauses and random loss are zero. It runs unpaced
(`speed: 0`) for 5 virtual milliseconds, with 50-microsecond snapshots, 1 GiB of
demand per client and a 50-million-event safety limit. Its 4096-slot queues avoid
buffer pressure with this stop-and-wait workload. The time horizon ends the
benchmark while streams still have data to send.

**Absolute simulator limits** is pinned first in the catalog. It schedules
50,000 clients and 100 backends with 10 TB/s client links, 100 TB/s backend links
and shared LB ingress/forwarding limits of 10 PB/s. The LB's 80,000,000,000,000,000
bit/s settings are decimal strings to retain every digit. This unpaced run lasts
1 virtual millisecond, with one 1-ms snapshot window, 1 GiB per client and a
20-million-event limit. Endpoint queues have 4096 slots; each LB input FIFO has
131072 slots to accommodate the initial burst. Travel, loss and sender gaps are zero.
This stresses endpoint construction, DES, telemetry and UI together. The backend's
existing 100 half-open connection slots still apply, so 50,000 created clients
does not imply 50,000 established streams. Transmission still reaches the 1 ns floor.

The **Simulation performance** card shows LB packets and engine events per real
second, elapsed wall time and total work. Packets are counted once at LB output
transmission start, including ACKs, control packets and retransmissions. Rates
are cumulative averages from model creation through run execution, including
setup, pacing and snapshot work. Finished and failed runs freeze the elapsed
time; Stop clears the measurement. These are simulator throughput measurements,
not a modeled CPU capacity of the load balancer or a kernel benchmark.

At these bitrates, transmission of both a header-only ACK and a full segment
rounds up to the engine's minimum 1 ns. The preset therefore approaches that
numerical ceiling, rather than providing literally unlimited bandwidth. Compare
real-time performance with the same scenario, snapshot interval and machine load;
virtual packet rates describe the model and are a separate measurement.

## Scenario files and effective configuration

The **Scenario files** panel imports and exports the editable native scenario.
**Import JSON** submits the original UTF-8 file text to backend validation before
browser JSON parsing, preserving integer tokens for exact validation. Files are
limited to 2 MiB of UTF-8 bytes. A successful import loads the draft; a failure
preserves the current draft. Import never starts an experiment.

**Export JSON** downloads the current draft, including manual edits, profiles,
groups and individual overrides. **Validate draft** checks the draft without
starting it. The **Native scenario** preview is the source used for export and
Start; **Effective configuration** shows the resolved defaults, inherited links,
streams and explicit overrides returned by validation. The resolved preview is
read-only and is not the import format. Correct invalid form values before
validation or export. The active run's original document remains separate.

Twenty-seven ready documents, their units and expected settings are described in
[examples/scenarios/README.md](examples/scenarios/README.md). Twenty-six mirror the UI
preset catalog; `advanced-overrides.json` demonstrates directional and individual
client settings. Nanosecond fields store exact virtual time, bandwidth is in
decimal bit/s, queue capacities count waiting packets and `bytes: -1` uses
`packets * 1024`. Integers beyond JavaScript's safe range use decimal strings.

For reproducible virtual results, retain the native file, seed, code revision
and Go/model version, and replay runtime commands at the same virtual times.
Wall time and simulator throughput depend on host load, pacing and snapshot work.
The stop-and-wait model can remain ACK-limited even when configured client rates
sum above an LB limit; their sum does not guarantee saturation. Queue/loss counters
refer to separate physical hops, not unique application packets.

## Live telemetry

The browser retrieves graph structure separately from frequent measurements:

- HTTP `/api/simulations/current/topology` supplies node identities, addresses,
  metric descriptions, physical link settings, input queue capacities and backend
  identities. The owner caches one encoded definition before publishing its metrics.
- WebSocket frames have `type: "simulationSnapshot"` and `version: 1`. They carry
  lifecycle metadata, the live algorithm, backend counters and routed client IDs,
  node metric values and measured traffic, and link/input queue counters.
  They contain neither `simulation.config` nor graph labels, addresses or link settings.
- `simulation.runId` and `topology.revision` identify the definition required by a
  frame. Revisions are opaque decimal strings. Adding clients or removing a backend
  replaces the definition; changing counters, activity or the algorithm does not.

Each frame is a complete measurement for its revision, so the hub can keep only the
latest frame without maintaining a chain of deltas. Routed client IDs resolve to
connection keys in the HTTP definition. The full initial scenario remains available
through `/api/simulations/current`, independently of runtime changes.

The frontend coalesces graph requests and applies metrics only to a matching run
and revision. Reconnect invalidates the old graph cache, including after a backend
restart. Finished runs remain readable; Stop clears the graph and counters while
preserving the form draft. The topology definition endpoint retains no history.
The backend and browser use the same telemetry protocol and should be rebuilt together.

For an active fixture with 2000 routed clients at 100 virtual milliseconds, the
previous JSON frame measured 3,790,079 bytes and the compact frame 1,730,760 bytes
(54.33% less). The one-time definition measured 2,411,176 bytes. One definition
plus 100 frames for that unchanged topology uses 53.70% fewer JSON bytes. This
measurement excludes framing and compression and does not measure simulation speed.

## Virtual-time architecture

- `simulator/engine/engine.go` owns one global event queue ordered by virtual
  nanoseconds and increasing event ID. Cancellation removes timers from the heap.
  No model event is scheduled after the experiment's end time.
- `simulator/network/link.go` models directed links: one transmission at a time,
  packet travel time and seeded loss. Incoming links own a finite FIFO. A packet
  consumes bandwidth even when random loss suppresses delivery. Queue capacity
  excludes the active transmission and packets already travelling.
- `simulator/network/egress.go` owns the LB's outgoing FIFO queues, one per
  destination link. It starts the oldest queued packet among ready outputs;
  busy links do not block other destinations. Managed links have no second
  waiting queue. Readiness follows transmission completion, without waiting for
  the packet to arrive. Reports and topology include the managed queue's counters.
  A packet pacer shares one forwarding budget across all LB outputs. Each link
  also enforces its own rate.
- `simulator/network/ingress.go` owns separate client and backend input FIFOs.
  One shared link serves the oldest waiting packet across both queues. Each FIFO
  has its own waiting slots; neither side reserves a share of the common rate.
- `client/`, `server/` and `balancer/` use short packet handlers and scheduled
  callbacks. Connections coexist as state objects. There is no waiting goroutine
  or channel per client, connection or backend.
- `internal/simulation/world.go` builds the normalized runtime and connects
  endpoints. `internal/simulation/clients.go` creates clients lazily from
  compact groups. The application `runner.go` advances virtual steps and applies
  runtime commands at their boundaries; `internal/presentation` builds snapshots.
  Client access links and backend return links reach their respective input
  FIFOs, followed by one shared physical input link into the LB.
  Link IDs and random sources follow resource identities instead of creation
  order. Topology uses separate namespaces for infrastructure and endpoints.
  The optional real clock pacing belongs to this application runner.
- `http/` handles real HTTP/WebSocket I/O. It receives detached JSON snapshots,
  keeps the latest frame for new subscribers and replaces queued UI frames when
  a browser is slow. Model execution does not wait for a WebSocket writer.

The **LB ingress link** limits traffic before either LB packet handler runs:

```text
clients -------- access links ------> client FIFO --+
                                                   +--> shared RX --> LB handlers
backends ------- return links ------> backend FIFO -+
```

`Ingress` owns these two waiting queues and controls one physical `Link`.
That link has no second waiting buffer. These are network buffers before the
last physical section into the LB, separate from the future kernel RX model.
All client and backend packets use this receiver, including ACK, SYN, FIN and retransmissions.
Each packet completes its access-link transmission and travel, then its ingress
transmission and travel. These are two physical sections; the ingress travel
default is zero additional time. Outgoing LB packets retain their existing paths.

When one input FIFO fills, new packets from that side are tail-dropped before
the LB can handle them. The other side keeps its own waiting slots. For example,
a capacity of 1024 gives 1024 client slots and 1024 backend slots. The oldest
waiting packet across both FIFO heads gets the next transmission; there are no
fixed bandwidth shares. Capacity excludes active transmission and travelling
packets. Capacity zero admits only an immediate transmission. Random ingress
loss still consumes its bandwidth. Drops do not notify the remote sender
immediately; the existing transport reacts through its normal timers.

For example, with access links at 10 Gbit/s and ingress at 1 Gbit/s, a sufficiently
long actual input burst can overflow the client input FIFO. Configured access rates
are maximum capacities, not guaranteed generation rates: clients still use
stop-and-wait. With no congestion control or adaptive retransmission timers,
drop and retry results describe this simplified transport, not a validated TCP
response to overload. LB packet handlers still have zero modeled CPU cost.

The frontend has separate **LB ingress** settings for bandwidth, packet travel
time, slots per input queue and loss. Headless `Ingress` includes the shared
link's `Config` and aggregate packet/byte counters; `IngressQueues` reports each
side separately. `Links` contains only endpoint paths, each with its own `Config`.
Headless `PacketTravelTime` is in nanoseconds. The topology places clients left
of the LB and backends right. `topology.ingress` describes the common receiver
and its queues once, independently of arrows. Its counters distinguish input
drops from drops at LB outputs.

The **LB forwarding budget** counts each outgoing packet once, at transmission
start, including its modeled headers. SYN, ACK, FIN and retransmissions use the
same budget. Queue drops consume none; a packet lost after transmission starts
still consumes its grant. Incoming traffic is not charged a second time.

After starting a packet, the next LB grant becomes available after
`ceil(wire bytes * 8 * 1e9 / lb bandwidth)` virtual nanoseconds. This wait runs
alongside the destination link's transmission, with no additional network hop.
An occupied link does not block other ready outputs. The oldest ready queue head
wins; equally old packets follow their admission order. Idle time does not build
up credit for a later burst. The limit has packet granularity, including the first
packet starting immediately when the budget and link are free.

A zero-capacity output admits a packet only when it can start immediately;
otherwise it drops it even when its link is free but the shared budget is busy.
The pacer schedules at most one budget wakeup at a time, only while a ready output
is waiting. There are no periodic budget ticks or new model goroutines.

Headless results include `Forwarding`: the configured limit, packets and wire
bytes started, waiting packets, queue drops, `BudgetWait` and `BudgetBlocked`.
`BudgetWait` is virtual nanoseconds spent with at least one ready output waiting
for the shared budget. It counts each interval once across all outputs and
excludes time waiting only for a busy link. `BudgetBlocked` describes the current
state, including unfinished work at the end time. The topology's LB inspector
shows the same counters, with budget wait converted to milliseconds.

HTTP Start carries the native scenario through the bounded Start queue to the
simulation owner. Configuration is validated before models are created. The
legacy WebSocket `startSimulation` remains available for compatibility. During
a run, `setAlgorithm` and `spawnClient` are applied at virtual
step boundaries and logged with their virtual timestamp. After a run, these
controls are disabled until another Start. Invalid requests are reported in the
UI; starting an already running experiment is rejected.

Command names are defined once in `simulator/internal/contract/commands.go` as `CommandType`
constants. After adding or renaming an action, run `go generate ./http` from
`simulator/` to update the frontend enum in `visualizer/src/commands.ts`.
The generated file is committed, so frontend and Docker builds need no generator.

The current **byte-based stop-and-wait model** uses separate `Seq` and `Ack`
positions instead of segment indices. `Size` remains the payload length, and
`WireSize()` still adds 40 modeled IPv4/TCP header bytes. Data advances sequence
positions by its length; SYN and FIN each consume one position, while pure ACK
consumes none. The two directions keep separate sequence state.

The `tcp-byte-stop-and-wait-v1` profile uses logical `uint64` byte positions and
fixed initial sequence numbers of zero. It does not model the 32-bit sequence
number wraparound of real TCP. Large streams are checked against native integer
limits rather than the former 32-bit segment-counter limit. Payload remains
metadata; no buffer is allocated for every byte in a stream.

Only one data segment may await acknowledgment. Retransmission repeats its
original byte range, and duplicate data/ACKs do not add payload twice. A segment
is released only after its full range is acknowledged; receive buffering,
advertised windows and a general partial-ACK send queue remain future work.
SYN and data retries still use fixed virtual timers. Server SYN-ACK timers,
FIN retries, a complete TCP state machine, congestion control and UDP are deferred.
The same-flow SYN generator remains a limited model, not a validated SYN flood.
The L4 model has no application workers or business-operation queues.

Snapshots include lifecycle status, selected configuration and virtual time in
`simulation`, alongside the balancer and server state. The `topology` graph
contains the actual model nodes, directed links and per-link configuration and
counters. It is empty before a run starts and after **Stop and clear**.
`LatencyP95Milli` still
measures SYN-to-FIN connection lifetime over the last 100 finished connections;
`CpuUsage` remains the legacy packet-count proxy, not CPU utilization. NAT/SNAT/DSR
remain mode labels rather than implemented translation paths. These fields are
not validation of the model against a real network stack.

## Run the visualizer

In a separate terminal, from the repository root:

```sh
cd visualizer
npm ci
npm run dev
```

The frontend connects directly to `ws://localhost:8080/ws`. The backend streams simulation
updates and stays available after the simulated streams finish. Set
`LB_HTTP_ADDR` to override the backend listen address, or `VITE_WS_URL` before
starting Vite to override the browser WebSocket URL.

## Simulation and topology pages

| Page       | Path        | Purpose                                                           |
| ---------- | ----------- | ----------------------------------------------------------------- |
| Simulation | `/`         | Configure, start or stop an experiment and monitor backend state. |
| Topology   | `/topology` | Explore the current graph and inspect nodes and directed links.   |

Both pages share the WebSocket connection and current experiment in `App`.
Switching pages preserves the configuration draft and keeps the run advancing.
The topology starts with client groups collapsed. Bundled arrows such as
`100 × 100 Mbit/s` represent separate links with matching settings. Expand a
group or search for one client to see its physical links. Inspect a node, link
or bundle to see its configuration, member links and counters.
The LB shows its client-side and backend-side input queues, common RX limit and
common TX budget. Finished runs retain their final graph and state.

The view displays each link's own settings from the graph DTO. Separate summaries
show incoming access links, the two input FIFOs with one shared receiver, and LB
outputs with their common forwarding budget. Configured rates are limits;
delivery and drop counters refer
to each physical hop. Profiles and endpoint overrides can give links different
settings; the current simple form sets uniform endpoint defaults.
Additional topologies or load-balancer resources require corresponding backend
models and snapshot projections. See [the visualizer guide](visualizer/README.md)
for the frontend extension points.

Node rectangles show **measured TX/RX** in B/s, kB/s or MB/s. Rates use the
wire-byte counter increase over the latest virtual-time sampling window
(`snapshotInterval`, 100 ms by default). TX counts completed link transmission,
including packets subsequently lost; RX counts delivered bytes. Waiting packets
and queue drops do not count as transmitted. Headers, ACKs and retries are included.
The LB's RX uses the common receiver once; its TX sums outgoing links. Client
groups sum only the members still represented by that rectangle.

The Simulation page's **Payload progress** panel shows cumulative unique received
and ACK-confirmed payload bytes, including unfinished transfers. ACKs in flight
or lost can leave the confirmed total behind the received total. Backend cards
show their received payload; the topology inspector exposes backend receipts,
client confirmations and run totals on the LB. These are payload totals rather
than wire-byte rates, and survive connection cleanup until Stop/reset.

The adjacent **Transfer results** panel reports attempts independently of the
connection lifecycle. A transfer succeeds when all its payload is ACK-confirmed;
a later FIN timeout does not undo that result. Existing abort paths record a
failure reason without changing the simulated protocol. At the time horizon,
unfinished created transfers remain **In progress**; reserved future clients are
**Not started**. The counts satisfy `started = succeeded + failed + inProgress`.
The legacy headless `IncompleteStreams` includes both failed and active attempts.

All transfer durations use virtual seconds. **Handshake** runs from the connection
request (the initial SYN is scheduled at the same timestamp) to the first accepted
SYN-ACK and includes SYN retries. **Payload** runs from the first original data
send to the final payload ACK: it excludes the initial sender pause, but includes
later pauses and retries. **Completion** runs from the connection request to that
final ACK. Empty streams succeed after handshake with zero payload duration.
Payload/completion statistics contain successes only; handshake statistics include
all completed handshakes, even if the transfer later fails. A sample count of zero
means unavailable; a measured zero is valid. These mean/min/max statistics do not
replace the backend's existing SYN-to-FIN p95 connection-lifetime proxy.

Live snapshots expose this summary as `simulation.transfers`; routed clients also
carry their own `transfer` measurements. Historical clients remain in the run
summary after leaving the routing table. Retry totals never reset when an ACK
resets the current per-segment retry counter. Stop discards the whole run rather
than turning discarded work into failures.

The runner samples after all events at a boundary have completed, including a
short final interval. Reading topology or dropping WS frames changes no measurement.
Before positive virtual time passes, rates show `—`; finished runs retain the
last measured window. These are packet-completion averages, so a long transmission
may produce a zero window followed by a burst when it finishes.

## Verify the project

Run `go test ./...` from `simulator/`. From `visualizer/`, run `npm test`,
`npm run build` and `npm run lint`.

Dependencies, build output, runtime traces, and local repository backups are
excluded from Git by the root `.gitignore`.
