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
| `network/`, `client/`, `server/`, `balancer/`, `algorithms/` | Links and queues, client workloads, backend hosts, forwarding and backend selection.            |
| `protocols/tcp/`                                             | TCP sender and receiver state, byte ranges, windows, timers, recovery and transport observations. |
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

Clients own workload definitions and traffic generation; backend hosts own their
identity, health and local resource limits. Both use the TCP transport under
`protocols/tcp/`. Congestion algorithms live in `protocols/tcp/congestion_control/`,
and reusable SACK range tracking lives in `protocols/tcp/sack/`.
`protocols/udp/` reserves the protocol boundary for future UDP support.

TCP pulls new payload through a small source interface. The client's finite
stream owns its byte count and workload gap; TCP owns packetization, windows,
ACKs and retries. The gap begins when the connection and stream are both ready,
then restarts after each actual new send. A full congestion window does not
generate a backlog of independently scheduled client packets.

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
`tcp-sack-v1` profile. `result.TCP.ReceiveBufferBytes`
records the effective receive-buffer limit, including an omitted input's default.
`result.TCP.RTO` and `result.LBIdleTimeouts` record the effective timer policy.
`result.ClientTimings` retains each created client's latest RTT estimates and RTO,
including completed or removed clients. Duration fields ending in `Seconds` use
virtual seconds; `Samples: 0` means no unambiguous RTT sample is available.
`result.TCP.CongestionControl` records the effective sender policy, while
`result.ClientCongestion` retains each created client's last window, threshold,
phase and bytes in flight, including clients no longer present in the routing table.
Build metadata records the Go/module versions,
OS and architecture; VCS revision and modification status appear when available.

Invalid input or configuration returns a nonzero exit code without a report.
An execution limit also returns nonzero, but saves a `failed` report containing
the work completed so far. Cancellation saves the partial result with
`status: cancelled` and returns nonzero. `finished` means the runner reached the
virtual end time; individual transfers may still have failed or be incomplete.

The headless runner samples virtual traffic at completion without building
frontend snapshots; the real-time UI refresh interval does not control its steps.
As in the original headless mode,
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
| `-snapshot-interval`, `-speed`          | `100ms`, `1`         | Real-time UI refresh interval and virtual seconds per real second; speed `0` is unpaced.             |
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
| `execution` | Virtual duration, real-time UI refresh interval, seed, pacing and event limit. |
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

### Mutable virtual CPUs

Each LB and backend host has `cores`, `packetProcessingTimeNanoseconds` and
`queueCapacity`. A zero packet cost processes new packets immediately. A positive
cost uses virtual cores and a waiting FIFO; capacity counts waiting packets only.

Version-2 scenario states can change these settings together with network limits.
Every state is checked before execution. A transition commits before ordinary
events at its timestamp; queued work resumes after the complete configuration
has been applied. It keeps its FIFO order, counters and endpoint state.

An active packet keeps its original completion time when cost or cores change.
After a core reduction, excess active jobs finish on retiring cores; queued jobs
start only when fewer jobs are active than the new core count. Growing the pool
starts additional waiters. Reducing the FIFO limit retains admitted work and
rejects new waiting packets while full. Setting cost to zero releases queued
packets in FIFO order, while old active jobs finish at their original deadlines.
A later positive cost enables finite CPU processing again.

CPU utilization divides cumulative busy core time by cumulative available core
time, reported as `availableCoreTimeSeconds`. Available capacity is integrated
over the host's full lifetime, including idle intervals and periods with zero
packet cost. During core retirement it is `max(configured cores, active jobs)`;
this accounts for the capacity still occupied by old jobs. Earlier CPU use remains
visible after cost becomes zero. For example, two busy cores for 1 s followed by
one idle core for 1 s give 2 / 3 utilization.

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
(`speed: 0`) for 5 virtual milliseconds, with snapshots every 100 real ms, 1 GiB of
demand per client and a 50-million-event safety limit. Its queues have 4096 slots;
the current sliding-window workload can enqueue several segments per client
before acknowledgements return. The time horizon ends the benchmark while
streams still have data to send. Historical stop-and-wait measurements describe
their recorded model version and are not directly comparable to this profile.

**Absolute simulator limits** is pinned first in the catalog. It schedules
50,000 clients and 100 backends with 10 TB/s client links, 100 TB/s backend links
and shared LB ingress/forwarding limits of 10 PB/s. The LB's 80,000,000,000,000,000
bit/s settings are decimal strings to retain every digit. This unpaced run lasts
1 virtual millisecond, with snapshots every 100 real ms, 1 GiB per client and a
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

Measured baselines, exact TCP detail and repeatable CLI/HTTP-WebSocket benchmark
commands are recorded in [perf_results.md](perf_results.md).

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
The sliding-window model can remain limited by the receive window, round-trip
time or sender pauses even when configured client rates sum above an LB limit;
their sum does not guarantee saturation. Queue/loss counters
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
- `protocols/tcp/` and `balancer/` use short packet handlers and scheduled
  callbacks. Connections coexist as state objects. There is no waiting goroutine
  or channel per client, connection or backend.
- `internal/simulation/world.go` builds the normalized runtime and connects
  endpoints. `internal/simulation/clients.go` creates clients lazily from
  compact groups. The application `runner.go` executes bounded event batches,
  applies runtime commands between batches and publishes at real-time intervals;
  `internal/presentation` builds snapshots.
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
are maximum capacities, not guaranteed generation rates: clients may send only
within the receive and congestion windows. Reno reacts through duplicate ACKs
and fast recovery, with RTO as a fallback. Drop and retry results describe
this simplified transport, not a validated TCP
response to overload. LB packet handlers have zero modeled CPU cost by default;
the host configuration can assign a positive cost.

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

The current **byte-based sliding-window model** uses separate `Seq` and `Ack`
positions instead of segment indices. `Size` remains the payload length, and
`WireSize()` adds 40 modeled IPv4/TCP header bytes plus any padded SACK options. Data advances sequence
positions by its length; SYN and FIN each consume one position, while pure ACK
consumes none. The two directions keep separate sequence state.

The `tcp-sack-v1` profile uses logical `uint64` byte positions and
fixed initial sequence numbers of zero. It does not model the 32-bit sequence
number wraparound of real TCP. Large streams are checked against native integer
limits rather than the former 32-bit segment-counter limit. Payload remains
metadata; no buffer is allocated for every byte in a stream.

Each connection has a bounded receive buffer that stores merged byte ranges,
not payload bytes. Data arriving beyond a gap is retained inside the receive
window. ACK identifies the next missing byte and can jump across buffered ranges
when a gap is filled. Duplicate and overlapping data count only newly received
bytes; those bytes may be counted before they are cumulatively acknowledged.

Set `tcp.receiveBufferBytes` in a scenario, use the **TCP receive buffer per
connection** form field, or pass `-tcp-receive-buffer` with the legacy CLI.
The default is 65535 bytes, and supported capacities are 1..65535 without window
scaling. An omitted scenario setting inherits the default; explicit zero is
invalid. Received contiguous bytes are consumed immediately, so the advertised
sequence window normally remains equal to the capacity. Out-of-order occupancy
is tracked separately and does not shrink an already advertised right edge.

Several data segments may await acknowledgment at once. An ordered queue retains
their metadata until cumulative ACKs release them. One ACK can confirm several
segments and part of the next; confirmed bytes count once and a partial head
retains only its unconfirmed suffix. Unsent payload is tracked separately from
all payload still awaiting confirmation, so filling the window cannot oversend
the stream. Each new segment fits the remaining receive-window credit and the
1024-byte segment limit. A zero window pauses new data; a current window update
resumes sending. Stalled receivers and zero-window persist probes remain outside
this immediate-consumption profile.

In-flight payload means data sent but not yet cumulatively acknowledged from
the client's perspective. It can still be travelling or already be buffered
by the backend while an ACK is returning or was lost. With the default Reno mode,
new data must fit both the receiver's advertised window and the sender's `cwnd`.
For example, `cwnd=4096 B` with `3072 B` still in flight permits another `1024 B`,
provided the receiver also has enough window credit. Retrying already-sent bytes
does not add another copy to this unique byte count. SYN/FIN positions are excluded;
closing or terminated clients report zero pending payload after abandoning their queue.

### Selective acknowledgments

`tcp.sackEnabled` enables the optional SACK profile; its default is `false`.
Use **Selective acknowledgements (SACK)** in the TCP traffic form or
`-sack-enabled=true` in the CLI. The effective choice appears in
`result.TCP.SACKEnabled` and in the experiment's scenario manifest.
This profile supports Reno, NewReno and CUBIC with fast retransmit enabled;
other combinations are rejected explicitly.

Endpoints exchange SACK-permitted during handshake. The backend reports up to
four buffered byte ranges, most recent first, using the existing receive buffer.
The sender merges these reports in a scoreboard and follows a bounded
[RFC 6675](https://www.rfc-editor.org/rfc/rfc6675.html) recovery procedure:
loss inference, a fixed recovery boundary, Pipe accounting, selective retries,
new-data priority over speculative retries, and a single eligible rescue retry.
Reno/NewReno halve the outstanding flight; CUBIC keeps its selected 70% loss
reduction. Recovery uses the reduced window without Reno's duplicate-ACK inflation.
SACK replaces the classic retry selection while enabled; it does not run a
second NewReno recovery loop. Optional Limited Transmit is not implemented.

SACK reports do not release the retransmission buffer, complete the transfer,
or increment ACK-confirmed payload. Only cumulative ACKs do that. After RTO,
old reports are discarded, the oldest pending bytes are retried, and a new fast
recovery is blocked until the timed-out flight is cumulatively acknowledged.
The receiver retains its buffered ranges; active receiver reneging is not modeled.
Recovery retries restart the virtual RTO as permitted by RFC 6675 section 6;
Karn's rule and timeout backoff remain in effect.

The selected option profile follows [RFC 2018](https://www.rfc-editor.org/rfc/rfc2018.html),
without timestamps, duplicate-data SACK or on-wire sequence wraparound. SACK
options consume modeled wire bytes: an offered SYN is 44 bytes, an ACK with one
block is 52 bytes, and one with four blocks is 76 bytes. ACKs are still immediate;
delayed ACK remains a separate planned mechanism.

The **TCP congestion control** form section configures `tcp.congestionControl`:

- `mode`: `reno` by default, `newReno` for partial-ACK recovery without SACK,
  `tahoe` for the historical loss-response comparison,
  `cubic` for the selected RFC 9438 profile, or `none` for the rwnd-only baseline;
- `fastRetransmit`: enabled by default for Reno, NewReno, Tahoe and CUBIC; controls early retry
  after three duplicate ACKs. Reno uses fast recovery, while Tahoe restarts slow
  start. CUBIC uses the existing fast-recovery mechanism with its own reduction
  factor. With `none`, it defaults to disabled;
- `cubicFastConvergence`: enabled by default; lets CUBIC lower its remembered
  window peak when a later loss suggests less capacity is available. Disable it
  for an isolated single-flow experiment. The field is retained but has no
  effect with another congestion-control mode;
- `initialWindowSegments`: 4 by default, each with at most 1024 payload bytes;
- `initialSlowStartThresholdBytes`: 65535 by default, with a minimum of 2048.

Legacy CLI flags are `-tcp-congestion-control`, `-tcp-fast-retransmit`,
`-tcp-cubic-fast-convergence`,
`-tcp-initial-window` and `-tcp-slow-start-threshold`. Omitted settings inherit
these defaults. `-tcp-fast-retransmit=false` keeps timeout-only recovery while
retaining the selected algorithm's window rules. An explicit enabled flag
requires Reno, NewReno, Tahoe or CUBIC; selecting `none` alone disables it. Larger
custom startup windows are experimental; four segments is the selected RFC 5681
initial limit for this model's MSS. To reproduce older rwnd-only experiments,
select `none` explicitly; existing JSON files now inherit Reno.

Reno, NewReno and Tahoe use the same selected startup and ACK-growth rules.
During slow start, each ACK confirming new data grows `cwnd` by
`min(newly acknowledged bytes, MSS)`. At `cwnd >= ssthresh`, congestion avoidance
counts newly confirmed bytes; confirming one window adds one MSS, at most once
per ACK. A cumulative ACK is processed in one phase, so crossing the threshold
cannot count that ACK again. Outside fast recovery, duplicate ACKs do not grow
the window; stale and invalid ACKs are ignored. This is roughly exponential growth during slow start and linear growth
during congestion avoidance when the sender fills its window.

On the first data timeout for a segment, `ssthresh=max(FlightSize/2, 2*MSS)`;
every data timeout sets `cwnd=MSS`. An original segment's retry marker survives
partial ACKs, preventing repeated threshold reductions for the same trimmed suffix.
Already-sent data remains in flight and can exceed the reduced window, blocking
new data until enough is acknowledged. The oldest-fragment retry remains allowed.
After a retried SYN, data starts with one MSS; a SYN-ACK itself never grows `cwnd`.
Before sending after a data-send gap longer than the current RTO, the sender
reduces `cwnd` to `min(initialWindow, cwnd)` and clears accumulated avoidance credit.
The initial window here includes the one-MSS reduction after a SYN retry.
This restart is independent of client/LB idle cleanup.

Fast retransmit counts valid ACKs repeating the oldest unconfirmed byte position,
with no payload, SYN or FIN and an unchanged raw advertised window. A window
update contributes no duplicate. The third qualifying duplicate immediately
retries the oldest fragment, before RTO. That retry restarts the current RTO
without doubling it; Karn's rule excludes ambiguous RTT samples.

Reno fast recovery sets `ssthresh=max(FlightSize/2, 2*MSS)` and temporarily sets
`cwnd=ssthresh+3*MSS`. Later duplicate ACKs can add one MSS each, capped using
the number of originally outstanding segments. New data still respects `rwnd`
and the configured send gap. The first ACK making progress, including a partial
ACK, exits recovery with `cwnd=ssthresh`, without another growth on that ACK.
A protected byte boundary prevents delayed duplicates from cutting the same
flight repeatedly; RTO clears active recovery and restores timeout behavior.
All retry state is prepared before a synchronous sender callback can reply.

NewReno is a separate [RFC 6582, sections 3.2 and 4](https://www.rfc-editor.org/rfc/rfc6582.html)
profile. It records the exclusive end of the old flight when fast recovery starts.
An advancing ACK below that fixed boundary retries the oldest remaining fragment,
reduces cwnd by the newly acknowledged bytes and adds back one MSS if at least
one MSS was acknowledged. The window stays at least one MSS. New data sent
during recovery cannot move the recorded boundary. This permits multiple holes
in one flight to be repaired without waiting for another three duplicate ACKs.

Only the first partial ACK restarts RTO; later partial ACKs and their retries
preserve that deadline. A full ACK reaches the fixed boundary and leaves recovery
with `cwnd=min(ssthresh, max(remaining flight, MSS)+MSS)`, without growing again
on the same ACK. RTO exits recovery and uses the existing one-MSS timeout response.
The policy requests retries; common TCP still owns packets, timers, Karn's rule,
retry limits and unique-payload accounting.

The selected base NewReno profile retains the RFC's conservative recovery guard:
a new fast loss is allowed only after ACK progresses beyond the previous boundary,
also after RTO. Initially that boundary follows SYN at sequence zero. Therefore
loss of the very first data byte before any payload progress waits for RTO.
Optional ACK/timestamp heuristics and Limited Transmit are excluded.
SACK is a separate opt-in setting and replaces this classic recovery when enabled.
Selecting `newReno` does not change the classic recovery selected by Reno or CUBIC.

Tahoe instead sets `ssthresh=max(FlightSize/2, 2*MSS)` and `cwnd=1*MSS` on the
third duplicate ACK. It immediately retransmits the missing fragment without
entering Reno fast recovery. Later duplicates do not inflate its window; the
next ACK confirming new bytes grows slow start normally. For example, eight
outstanding segments produce a four-MSS threshold: Reno enters recovery at seven
MSS and deflates to four on an advancing ACK, while Tahoe starts at one and grows
to two on that ACK. The old-flight guard also protects Tahoe from repeated cuts.
This is a documented Tahoe loss-response profile under the simulator's shared
TCP assumptions, not a reconstruction of every historical TCP detail.
See [RFC 3390](https://www.rfc-editor.org/rfc/rfc3390.html).

CUBIC uses a versioned, bounded [RFC 9438](https://www.rfc-editor.org/rfc/rfc9438.html)
profile: coefficient `C=0.4`, decrease factor `beta=0.7`, epoch-based cubic growth,
the Reno-friendly estimate and configurable fast convergence. Its calculations
use fractional segments internally; exported windows still count payload bytes.
The target one SRTT ahead is bounded between the current window and 1.5 times
that window. Only ACKs confirming new data increase the window; virtual time
passing alone cannot send packets or grow cwnd.

CUBIC uses the sender's smoothed RTT, not its RTO or a known link bandwidth.
Workload pauses and receiver-window limitations stop growth and are excluded
from the epoch clock, including pauses shorter than one RTO. Waiting for ACKs
while the congestion window itself is full continues the epoch clock.

On fast loss, the byte threshold is `max(floor(0.7*FlightSize), 2*MSS)`.
The selected classic recovery mechanism temporarily adds three MSS and leaves
recovery on the first advancing ACK. For example, 8192 bytes in flight produce
a 5734-byte threshold and an 8806-byte recovery window. On timeout, cwnd falls
to one MSS; repeated timeouts of the same original segment keep its threshold.
The first congestion-avoidance epoch after RTO restarts at its current window
with `K=0`, instead of reusing the earlier peak.

This CUBIC profile keeps Reno-style startup and the simulator's existing classic
recovery. HyStart++, SACK/NewReno, ECN and spurious-loss undo are not implemented
by selecting CUBIC. The profile is not a claim of reproducing Linux CUBIC's
complete TCP stack or offload behavior. Its receiver window is still unscaled.

These are the selected [RFC 5681, sections 2, 3.1, 3.2 and 4.1](https://www.rfc-editor.org/rfc/rfc5681.html)
rules for Reno. This is classic Reno: multiple gaps in one flight may still require RTO.
Limited Transmit on the first two duplicates and NewReno partial-ACK recovery
remain outside this profile, as does full window validation for senders limited
by workload pacing or `rwnd`.

Client topology details expose `cwnd`, `ssthresh`, current duplicate ACKs and
cumulative fast retransmissions, plus a readable congestion phase including
`fastRecovery`. CLI `ClientCongestion[].State` retains the same phase/counters
after cleanup. `DuplicateACKs` is the current run since progress/RTO and is
cleared on close; `FastRetransmissions` is cumulative. Fast retries are also
included in total data retransmissions; the two counters must not be added.
Disabled congestion control omits window limits rather than showing a false zero.

Each connection selects its congestion policy before handshake. Common TCP owns
SEQ/ACK, rwnd, duplicate-ACK detection, the protected flight boundary, retry
accounting, RTT/RTO and actual packet emission. The selected policy owns window
growth, loss response and its observable phase; it cannot inspect remote links
or schedule engine callbacks directly.

The algorithms live in `simulator/protocols/tcp/congestion_control/`, a separate Go
package named `congestioncontrol`. Its `Policy` interface accepts scalar ACK,
sender and loss observations and returns a detached `State`. `New` validates
configuration and selects the algorithm; concrete implementations stay private.
ACK responses can request a retry of the oldest fragment and preservation of
the RTO deadline. Loss responses can suppress a stale fast-retransmit indication.
The TCP package keeps `congestion_adapter.go` for translating transport events and
`loss_detection.go` for shared duplicate-ACK/retry tracking. Congestion algorithms
do not import the client, virtual-time engine, network or presentation layers.

The policy may return an optional absolute virtual deadline for the next new
data send. The sender combines that deadline with its workload gap and rechecks
it before emission. Current Reno, NewReno, Tahoe, CUBIC and none do not add pacing deadlines,
so they retain the existing per-segment scheduling. This seam covers new data;
retransmissions still use the immediate transport path. Delivery-rate sampling
and algorithm-specific retransmission pacing remain part of the future BBR scope.

`packetGapNanoseconds` is the initial pause once both the connection and stream
are ready, and the minimum interval between new data sends. An ACK does not
restart that pause. After a
window stall, an expired send deadline allows the next send immediately. Retries
bypass this pause. Each new segment uses its own engine event, including when
the gap is zero, so one connection does not generate its whole window in one
callback.

Data has one virtual retransmission timer for the oldest pending fragment.
Additional tail segments, repeated ACKs and window-only updates do not postpone
it. New cumulative progress restarts it if data remains, and an empty queue
cancels it. A timeout retries only the oldest pending fragment. Normal stream
completion waits for all payload to be confirmed before initiating FIN; explicit
`Close()` keeps its existing early-close semantics.

The default adaptive RTO starts at 1 s and stays within 200 ms..60 s. One safe RTT
probe measures virtual time from handover to the client's sender until an ACK
confirms its full range, including local queueing. The first sample sets
`SRTT=RTT` and `RTTVAR=RTT/2`; later samples use weights 1/8 and 1/4 respectively.
RTO is `SRTT + max(1 ns, 4*RTTVAR)`, clamped to the configured limits.
After measurements, the default minimum is 200 ms, including on a fast LAN.
This is an experimental lower bound; RFC 6298 recommends a 1 s minimum.

Karn's rule excludes ambiguous samples after a retry. The model conservatively
skips every ACK through the recovery boundary, including a cumulative ACK that
also confirms untouched later data. Only a new original send after recovery
can start another probe. Each timeout doubles the effective RTO up to its maximum.
ACK progress alone keeps that backoff; a fresh safe RTT sample recalculates RTO
and removes it. A clean SYN/SYN-ACK can supply the first sample. If SYN was
retried, adaptive data starts with at least 3 s, capped by the configured maximum.
These rules follow the selected [RFC 6298](https://www.rfc-editor.org/rfc/rfc6298.html)
profile; fixed mode deliberately retains its configured base instead.

The **TCP timers** form section configures `tcp.rto.mode` (`adaptive` or `fixed`),
`exponentialBackoff`, and `initialTimeoutNanoseconds`, `minimumTimeoutNanoseconds`,
`maximumTimeoutNanoseconds`. Limits must be positive and ordered
`minimum <= initial <= maximum`. Mode and backoff are independent: fixed mode
with backoff disabled reproduces the earlier constant-timeout policy while
still collecting safe RTT measurements. Legacy CLI equivalents are
`-tcp-rto-mode`, `-tcp-rto-backoff`, `-tcp-rto-initial`, `-tcp-rto-minimum`
and `-tcp-rto-maximum`.

Client idle cleanup is separate from retransmission: `tcp.clientIdleTimeoutNanoseconds`
defaults to 20 s; `-tcp-idle-timeout` is its CLI equivalent. Zero disables it.
Backend cleanup and LB mapping expiry have their own policies below. When comparing
timer modes, keep these policies equal: an endpoint's idle timeout can terminate it,
while an LB timeout removes only the route used by later retransmissions.

### Independent endpoint lifecycle

Each endpoint and the LB release only their own connection state. Stopping a
client leaves the backend connection and LB route until their own deadlines.
Stopping a backend clears its local state without ending remote clients or
migrating their active TCP streams. Packets emitted before the sender stops can
still arrive; FIN/RST take effect at the receiver only when delivered.

Backend cleanup is configured under `tcp`:

- `backendHalfOpenIdleTimeoutNanoseconds`: defaults to 5 s, measured from
  the first SYN. Repeated SYNs do not extend this handshake deadline.
- `backendEstablishedIdleTimeoutNanoseconds`: defaults to zero (disabled).
  When enabled, valid incoming traffic refreshes the backend's idle deadline.

The CLI flags are `-backend-half-open-idle-timeout` and
`-backend-established-idle-timeout`; both settings also appear in the form and
effective TCP report. Zero disables either limit. Closing uses its own timers.

The LB keeps only a phase and last activity time for each flow mapping. Its four
idle limits are independent of endpoint RTO, MSL and FIN-wait settings:

| Observed phase | Native field under `resources.loadBalancer` | Default |
|---|---|---:|
| `halfOpen`: new client SYN | `halfOpenIdleTimeoutNanoseconds` | 5 s |
| `established`: client ACK or DATA+ACK | `establishedIdleTimeoutNanoseconds` | 50 s |
| `closing`: FIN from either side | `closingIdleTimeoutNanoseconds` | 120 s |
| `reset`: RST from either side | `resetIdleTimeoutNanoseconds` | 10 s |

Omission inherits the default; zero disables expiry for that phase only. CLI flags
are `-lb-half-open-idle-timeout`, `-lb-established-idle-timeout`,
`-lb-closing-idle-timeout` and `-lb-reset-idle-timeout`. The form exposes all four,
and reports record them in `LBIdleTimeouts`.

Supported matching packets from either direction refresh activity when seen by the
LB, before its chaos or output drops. RST selects reset; otherwise FIN selects
closing; only a client ACK without SYN/FIN/RST can promote half-open to established.
Reset takes precedence over closing, and delayed SYN/SYN+ACK/ACK/data cannot reopen
either terminal phase. Repeated traffic can extend those idle deadlines.

The observer supports client SYN, backend SYN+ACK, ACK with optional payload/PSH,
FIN+ACK, and RST with optional ACK. Controls must have no payload; unsupported
combinations and negative sizes do not update its state. Invalid new SYN shapes
cannot create a route. Existing-route forwarding leaves TCP acceptance to receivers.
The LB does not validate SEQ/ACK positions: a supported RST with an unacceptable
sequence number selects reset retention even if the TCP receiver rejects it.

Cleanup runs every five virtual seconds and expires idle mappings at a sweep.
A packet arriving after the nominal deadline but before the sweep can refresh it;
equal-time events follow the engine's scheduling order. Short configured limits
may therefore expire up to a sweep later. A closing timeout shorter than RTO can
expire before a FIN retry, without terminating either endpoint or extending the
configured limit. Completed closing mappings now default to 120 s retention,
independently of the endpoint's TIME_WAIT.

Endpoint registrations remain available for delivery and historical observations
until the run is reset; they are separate from live LB mappings.
Missing mappings drop non-SYN traffic. A later client SYN can allocate a route;
before expiry, a same-key SYN remains pinned without reopening a terminal phase.
This does not add endpoint tuple-reuse or TCP-incarnation handling.

This is a simplified flag/direction policy inspired by
[IPVS state-based timeouts](https://github.com/torvalds/linux/blob/master/net/netfilter/ipvs/ip_vs_proto_tcp.c),
with fewer phases and sticky closing/reset, rather than full IPVS conformance.

Backend availability for new routes is the LB's own last probe result. A silent
crash is discovered on probe timeout, while an existing flow keeps its backend.
These probes still use the earlier idealized path without consuming link capacity;
network health checks are a later scope. Selection uses registered backend settings
and the LB's mapping counts, without reading remote live connection counters.
Stopped backends remain visible in topology and reports.

Whole-run Stop/reset is the explicit exception: it stops all endpoints, cancels
pending events and discards queued/in-flight work. Interactive reset then removes
the world and publishes the empty state.

### TCP connection closing

`Close()` drains the already accepted stream, including retries of unconfirmed
payload. This bounded profile sends FIN after all those bytes have been ACKed.
FIN reserves one sequence position once; every retransmission reuses that position.
The backend keeps its state until its FIN is acknowledged. The client sends the
final ACK and retains TIME_WAIT so a repeated peer FIN can receive another ACK.
ACK and FIN can be combined, and a peer FIN ends only that peer's sending half.
Accepted client payload can still finish after an early backend FIN.

The **TCP closing** form section configures `tcp.close`:

- `mslNanoseconds`: default 30 seconds, so TIME_WAIT lasts 60 virtual seconds;
- `finWaitTimeoutNanoseconds`: default 60 seconds waiting for the peer FIN
  after the local FIN has been acknowledged;
- `maxFINRetries`: default 5; zero permits the initial FIN but disables retries,
  with supported values 0..30.

MSL is a chosen profile parameter, not a measured packet-lifetime guarantee.
It must be positive and at most half the maximum Go duration; the FIN-wait
timeout must be positive. All timers use virtual events. CLI equivalents are
`-tcp-msl`, `-tcp-fin-wait-timeout`, and `-tcp-fin-retries`.

FIN retries have separate counters and backoff. The client starts from its current
RTO; the backend starts from configured initial RTO because it has no payload RTT
estimator. Both follow the configured backoff switch and maximum timeout. Duplicate
FINs repeat the necessary ACK without repeatedly consuming sequence positions.
RST requires a valid state and exact expected peer sequence; a reset answering SYN
must also acknowledge that SYN. This profile rejects FIN with payload or PSH,
and does not implement challenge ACKs or TIME_WAIT recycling.

Client observations retain TCP state, close outcome, FIN retries and closing duration.
CLI reports include `effectiveClose` and per-client `clientConnection`; topology
details show state and outcome separately from payload success. A payload transfer
can succeed even if later teardown reaches its retry limit. Completed streams can
remain in TIME_WAIT when a short experiment ends.

The LB retains closing mappings using its own observations and timers, independently
of endpoint termination. Neither graceful closure nor forced disappearance directly
deletes another endpoint's TCP state.

Closing follows the selected [RFC 9293, section 3.6](https://www.rfc-editor.org/rfc/rfc9293.html#section-3.6)
profile, with the documented bounds above. Server SYN-ACK retry timers,
remaining TCP mechanisms, and UDP are deferred.
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
wire-byte counter increase over the virtual time elapsed between observations.
Snapshots target one update every 100 real ms by default, independently of playback
speed. Rates remain bytes per virtual second; the virtual sampling window varies
with progress. Equal-time event batches are sampled after their timestamp completes.
Headless runs sample at completion. TX counts completed link transmission,
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

Public-API Go unit tests live under each component's `tests/behavior/unit/`.
Tests that need private state, or share fixtures with those checks, stay beside
their implementation. This includes the main application's private wiring.
Tests of cooperation through public boundaries live under
`simulator/tests/behavior/integration/`; architecture checks live under
`simulator/tests/behavior/unit/`. Test helpers move with their suites.

The frontend uses `visualizer/tests/behavior/unit/` and
`visualizer/tests/behavior/integration/`, with shared fixtures in
`visualizer/tests/`. The npm test command discovers both directories.

For Go coverage across the imported production packages, run
`go test -coverpkg=./... ./...` from `simulator/`. A package-local command such
as `go test ./client/...` includes the client's nested tests.

Tests of performance reporting are behavior tests, not execution benchmarks.
Each test root includes `behavior/integration/` and `performance/`; empty
directories contain `.gitkeep` placeholders for future suites.
After the functional work, component benchmarks and repeatable full runs will
track throughput, execution time and memory changes after each feature or
optimization, while also checking that the simulated outcomes remain comparable.

Dependencies, build output, runtime traces, and local repository backups are
excluded from Git by the root `.gitignore`.
