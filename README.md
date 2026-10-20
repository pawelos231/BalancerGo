# Intelligent load balancer

This repository contains both parts of the project:

- `simulator/` - Go backend, including `main.go`, `go.mod`, and all Go packages.
- `visualizer/` - React, TypeScript, and Vite frontend.

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

Requirements: Go 1.23.2 or newer, Node.js, and npm.

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
state and per-link queue/loss counters. `VirtualTime` describes the model;
`WallTime` describes the cost of running it. Clients still active at the end time
remain in the final state; reaching that time does not imply successful completion.

CLI flags provide the form defaults, and configure immediate headless runs.
The browser sends its selected configuration when Start is clicked. Useful
parameters (see `go run . -help` for all options):

| Option | Default | Meaning |
| --- | --- | --- |
| `-clients`, `-servers`, `-packets` | `200`, `10`, `10000` | Initial clients, backend count (at most 10000) and full segments per stream. |
| `-bytes` | `-1` | Override stream payload size, including a partial final segment; `-1` uses `packets * 1024`. |
| `-duration`, `-seed` | `2m`, `1` | Virtual end time (inclusive) and experiment seed. |
| `-client-gap`, `-packet-gap` | `0s`, `0s` | Virtual inter-arrival interval and sender pause before each data segment. |
| `-bandwidth`, `-packet-travel-time` | `1000000000`, `1ms` | Bits/s and virtual packet travel time after transmission per directed link. |
| `-link-queue`, `-loss` | `1024`, `0` | Waiting packets per link and independent loss probability per transmission. |
| `-algorithm` | `roundRobin` | `roundRobin` or seeded `random`. |
| `-snapshot-interval`, `-speed` | `100ms`, `1` | Virtual snapshot interval and virtual seconds per real second; speed `0` is unpaced. |
| `-max-events` | `50000000` | Execution limit, including equal-time events, to stop runaway scenarios. |

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

## Virtual-time architecture

- `simulator/engine/engine.go` owns one global event queue ordered by virtual
  nanoseconds and increasing event ID. Cancellation removes timers from the heap.
  No model event is scheduled after the experiment's end time.
- `simulator/network/link.go` models directed FIFO links: one transmission at a
  time, a finite waiting queue, packet travel time and seeded loss. A packet consumes
  bandwidth even when random loss suppresses delivery. Queue capacity excludes
  the active transmission and packets already travelling.
- `client/`, `server/` and `balancer/` use short packet handlers and scheduled
  callbacks. Connections coexist as state objects. There is no waiting goroutine
  or channel per client, connection or backend.
- `simulator/simulation.go` wires endpoints and links, schedules client arrivals
  and snapshots, and applies UI commands as events at virtual step boundaries.
  Clients share one access link in each direction; each backend has its own pair.
  The optional real clock pacing belongs to this application runner.
- `http/` handles real HTTP/WebSocket I/O. It receives detached JSON snapshots,
  keeps the latest frame for new subscribers and replaces queued UI frames when
  a browser is slow. Model execution does not wait for a WebSocket writer.

`startSimulation` carries the initial configuration through a bounded command
queue to the simulation owner. Configuration is validated before models are
created. During a run, `setAlgorithm` and `spawnClient` are applied at virtual
step boundaries and logged with their virtual timestamp. After a run, these
controls are disabled until another Start. Invalid requests are reported in the
UI; starting an already running experiment is rejected.

Command names are defined once in `simulator/http/commands.go` as `CommandType`
constants. After adding or renaming an action, run `go generate ./http` from
`simulator/` to update the frontend enum in `visualizer/src/commands.ts`.
The generated file is committed, so frontend and Docker builds need no generator.

This migration preserves a **simplified TCP-like stop-and-wait model**. Packet
indices are segment counters, not TCP byte sequence numbers. SYN retries and
data retries use virtual timers; server SYN-ACK retries, FIN retries, a complete
TCP state machine, windows, congestion control and UDP are deferred. The old
same-flow SYN generator is retained as a limited model, not a validated SYN flood.
Backend application workers and service queues are also deferred.

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

| Page | Path | Purpose |
| --- | --- | --- |
| Simulation | `/` | Configure, start or stop an experiment and monitor backend state. |
| Topology | `/topology` | Explore the current graph and inspect nodes and directed links. |

Both pages share the WebSocket connection and current experiment in `App`.
Switching pages preserves the configuration draft and keeps the run advancing.
The topology groups clients into one node; each arrow represents a directed link
reported by the backend, including the return direction. Select a node or link
to inspect its configuration and counters, or use search to find any element.
Finished runs retain their final graph and state.

The view displays each link's own bandwidth and other settings from the graph
DTO. The current model initializes all links from one global configuration.
Additional topologies or load-balancer resources require corresponding backend
models and snapshot projections. See [the visualizer guide](visualizer/README.md)
for the frontend extension points.

## Verify the project

Run `go test ./...` from `simulator/`. From `visualizer/`, run `npm run build`
and `npm run lint`.

Dependencies, build output, runtime traces, and local repository backups are
excluded from Git by the root `.gitignore`.
