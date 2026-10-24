# Load balancer visualizer

React, TypeScript and Vite frontend for the Go simulator. Use Node.js 24 and npm.
Start the backend with `go run .` from `simulator/`, then run from this directory:

```sh
npm ci
npm run dev
```

Open `http://localhost:5173`. The default WebSocket URL is
`ws://localhost:8080/ws`; set `VITE_WS_URL` before starting Vite to override it.
HTTP uses the same backend address, replacing WS/WSS with HTTP/HTTPS and removing
`/ws`. Set `VITE_API_URL` when the HTTP API has a separate base URL.
For Docker Compose, ports and build instructions, see the [root README](../README.md).

## Pages

- **Simulation** (`/`): edit configuration, click **Start simulation**, monitor
  backends, or **Stop and clear** the experiment. The **Scenario library** offers
  ready experiments with different client/backend rates and shared LB bottlenecks.
  Choose and validate a preset to load its settings into the form, then use the
  single **Start simulation** button. Selection alone changes no run.
- **Topology** (`/topology`): explore the live graph, pan and zoom, and select a
  node or directed link to inspect its settings and counters. Client groups start
  collapsed. Expand a group, select one of its members, or search for any client
  or physical link. Search covers the full graph, including hidden members.

`src/hooks/useSimulation.ts` owns the WebSocket, snapshots and lifecycle actions.
It starts scenarios and retrieves the current run's configuration through HTTP.
`src/App.tsx` shares that hook across both pages. The configuration form stays
mounted when switching pages, preserving
its draft. Static hosting must serve `index.html` for `/topology`; the container's
`local-web-server --spa index.html` already does this.

## Scenario library

`src/scenarios/types.ts` mirrors the backend document. Integer values may be safe
JSON numbers or exact decimal strings; keep strings unchanged when sending or
displaying the scenario. Nanosecond fields retain their explicit units.

`presets.ts` exposes 18 scenarios across links, queues, loss, workloads, routing,
edge cases and performance. Search by title/description or filter by category;
the library shows six cards per page. Choosing or filtering never starts a run.
The original four topology presets still inherit backend defaults. The twelve
additional factories in `scenarios/presets/` use a fixed baseline so CLI overrides
do not silently change the experiment being tested. Ordinary experiments run
for 180 virtual seconds at speed 1, with 1 GiB per client unless their workload
deliberately includes short or empty streams.

The additions cover asymmetric client links, mixed latency, a slow backend ACK
return, lossy access, one blackholed backend link, tiny or absent ingress queues,
staggered groups, mixed stream sizes, seeded random routing, no backends and no
clients. The staggered scenario also schedules a group beyond the horizon, so
not every configured client must appear in the graph. A blackholed link loses
packets; it does not simulate backend health detection or failover.

**Absolute simulator limits** is pinned first, including on the first page of
the unfiltered library. It schedules 50,000 clients and 100 backends: client
links are 10 TB/s, backend links 100 TB/s, and both shared LB limits are 10 PB/s.
The latter are exact decimal strings (`"80000000000000000"` bit/s). It uses
speed 0, zero travel/loss/gaps, a 1-ms virtual horizon and snapshot interval,
1 GiB per client, a 20-million-event cap, 4096-slot endpoint queues and
131072-slot LB input queues. Sparse snapshots limit telemetry work at this scale.
Created clients may remain in handshake because backend half-open capacity is
still 100 each. The 1 ns transmission floor and current frontend rendering costs
remain part of the experiment.

The separate **Simulator throughput** preset uses 50 clients and 5 backends with
10 TB/s client links, 100 TB/s backend links and shared LB limits of 1 PB/s. It
removes travel delays, sender pauses and random loss, and runs without pacing
for 5 virtual milliseconds. Snapshots are spaced by 50 virtual microseconds;
the event limit is 50 million and queues have 4096 slots. Each client's 1 GiB
stream remains unfinished when the benchmark reaches its time horizon.

`SimulationPerformance.tsx` appears on both pages when the backend supplies
`simulation.performance`. It shows cumulative LB packets and DES events per real
second, wall time and work totals. The backend includes setup, pacing and snapshot
work in the measurement and freezes it after completion or failure. Stop clears
it. These rates measure simulator execution, independently of virtual-time TX/RX.
With the throughput preset, transmission durations reach the model's 1 ns floor;
the large configured bitrates do not represent literal unlimited bandwidth.

`ExperimentSetup.tsx` owns one native scenario and its controlled form inputs.
`ScenarioLibrary.tsx` selects a candidate and applies it only after successful
server validation. Loading a preset updates every input together; validation
blocks editing and Start until it completes. `api.ts` wraps
the defaults, validate, Start and current-run endpoints; `runtimeApi.ts` selects
the backend address. `ScenarioRunDetails.tsx` displays the exact initial run
document. When a run has multiple client groups, the runtime Add clients control
selects the group whose resolved network and stream settings should be used.

`scenarios/draft.ts` projects native settings into form strings and applies
individual field changes without flattening groups, profiles or overrides.
Integer strings and nanosecond durations retain their precision. Changing the
total client count proportionally resizes existing groups; group counts remain
visible, and edits that would invalidate explicit client overrides are rejected.

The single Start action sends the native draft through HTTP for server validation
and admission. Invalid visible input blocks Start. Drafts survive Stop, navigation
and reconnect.

## Scenario files

The **Scenario files** panel has **Import JSON**, **Export JSON** and
**Validate draft** actions. Import sends the original UTF-8 file text to the
backend before browser JSON parsing, with a 2 MiB byte limit. Only successful
validation replaces the draft; malformed or invalid files leave it unchanged.
Import and validation never start a run. Correct invalid form values before
validating or exporting.

Export downloads the current native draft with its groups, profiles and overrides,
including manual edits. **Native scenario** previews that export/Start document.
**Effective configuration** shows the optional `resolved` result from backend
validation: inherited link settings and streams for backends, groups and explicit
client overrides. It is read-only and is not an import format. The active run's
original scenario remains a separate document.

See [example scenarios](../examples/scenarios/README.md) for all 18 catalog
experiments and a smaller directional/client-override example. JSON duration
fields use exact virtual nanoseconds, bandwidth uses decimal bit/s and capacities
count waiting packets. Keep large integer decimal strings intact. The same file,
seed, code/model version and runtime command timing reproduce virtual results;
elapsed real time and simulator throughput still vary with host load and pacing.
Configured link rates are maxima: stop-and-wait ACK feedback can prevent their
sum from saturating the LB. Drop totals describe physical hops, not unique payloads.

## Topology data and extension points

HTTP `/api/simulations/current/topology` supplies graph definitions from the
actual simulation wiring. Compact WebSocket frames carry complete current metrics
and identify their definition with `simulation.runId` and `topology.revision`.
The hook assembles these into the view's `topology.nodes` and `topology.links`,
sharing static descriptors and settings between updates. It never combines data
from different runs or revisions. Graph requests are coalesced; reconnect invalidates
the old definition cache, and loading failures offer a retry without replacing the draft.

Each created client and backend has its own node and
two directed links. Clients appear left of the LB and backends right. Incoming
arrows end at the corresponding LB input FIFO; both FIFOs use one shared receiver.
`topology.ingress` carries that common resource's configuration, aggregate stats
and per-side queue stats, rather than another node or arrow.
Idle and cleared experiments have an empty graph;
finished or failed runs retain their final snapshot.

- `src/topology/types.ts` defines the graph DTO. Node `kind` is an open string:
  unknown kinds use a generic renderer. Node metrics use `key`, `label`, numeric
  `value` and optional `unit`. Preserve stable node and link IDs; link `source`
  and `target` reference node IDs independently of labels. Optional `resourceId`
  and `groupId` identify the scenario resource and client group separately from
  graph IDs.
- `src/topology/grouping.ts` builds a separate drawing from that physical graph.
  A group bundles only links with matching displayed endpoints and all four link settings.
  Labels such as `100 × 100 Mbit/s` describe independent links, each limited to
  100 Mbit/s. The shared receiver is preserved once, outside the bundled arrows. Selecting a
  member reveals its own links while the remaining clients stay grouped.
  Presentation IDs and layout structure stay unchanged when only counters change.
- Each link carries its own `config` and `stats`, including bandwidth, packet
  travel time, queue capacity, loss and current packet counters. The view already
  displays different bandwidths per link. The backend resolves settings from
  scenario defaults, profiles and endpoint overrides. The form edits shared
  defaults and ingress/forwarding limits while preserving detailed endpoint settings.
- `src/topology/layout.ts` bounds the canvas to 200 nodes and 500 links while
  keeping every element searchable; selection brings its context into view.
  Search displays up to 50 matches. `structureKey` uses node IDs/kinds and link
  IDs/endpoints; metric and capacity updates reuse the existing layout. A single
  LB with known endpoint kinds uses three columns and separate RX/TX ports.
  Unknown or more complex graphs fall back to Dagre.
- `TopologyLimits.tsx` separates incoming access links, the common receiver and
  outgoing LB links with their shared forwarding budget. Counters refer to
  physical hops; delivery totals across these scopes must not be interpreted as
  unique application packets. Configured bitrates are limits, not measured rates.
  `telemetry.ts` classifies links using endpoint kinds and counts each link once.
- `TopologyIngressDetails.tsx` shows the common receiver once and each input
  FIFO's occupancy and drops. Its details are shared by the summaries and LB inspector.
- `TopologyNodeView.tsx` and `TopologyLinkView.tsx` render nodes and arrows;
  `TopologyCanvas.tsx` handles graph interaction. The inspector shows physical
  links or bounded member lists for groups and bundles, with separate queue and
  random-loss counters. The page and `useTopologyView.ts` coordinate grouping,
  search and selection. Search UI lives in `TopologySearch.tsx`.
- `TopologyTrafficRates.tsx` displays each node's optional `traffic` measurement
  in B/s with an explanation of the virtual-time window. TX is completed wire
  transmission and RX is delivered bytes, including headers, ACKs and retries.
  `grouping.ts` sums only represented members with matching windows; missing
  measurements stay unknown. Rates arrive from Go and do not depend on WS timing.

The ingress queue setting applies to each side: 1024 means 1024 client slots
plus 1024 backend slots. The receiver always serves the oldest waiting FIFO
head, without reserving bandwidth shares. Output FIFOs remain per endpoint and
share one forwarding budget.

Queue slots exclude active transmissions and packets already travelling. A
bundle's queue slots are spread across independent queues, so one member may
drop packets while another has space. The transport model currently uses
stop-and-wait and fixed retry timers; it does not model congestion control or
kernel RX processing. Queue losses describe this model rather than real TCP.

For additional topologies or load-balancer resources, implement the backend
model and extend its topology projection first. Then update DTOs and renderers
to display those resources and their actual metrics.

## Build and check

```sh
npm run build
npm run lint
npm test
```

Tests use Node.js 24's TypeScript support and built-in test runner. They check
scenario edits and exact integers, presets and API requests, grouping, physical
counter scopes, bounded graph selection and topology loading races without adding a
test framework dependency.

Build output is written to `dist/`. `VITE_WS_URL` is embedded at build time, so
changing the backend URL for a static deployment requires rebuilding.
