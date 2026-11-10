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

The Simulation page also shows **Payload progress**: cumulative unique payload
received by backends and confirmed to clients by ACK, including unfinished
transfers. The counters exclude headers and retransmissions and survive closed
connections until reset. Backend cards show received payload; topology details
show per-backend receipts, per-client confirmations and aggregate totals.

**Transfer results** separates succeeded, failed and in-progress attempts from
future arrivals not yet started. Success is defined at the final payload ACK,
independently of later connection closure. The panel shows failure reasons and
cumulative SYN/data retransmissions, plus virtual duration sample counts and
mean/min/max values. Payload/completion summaries include successes only;
handshake summaries include every completed handshake. Zero samples means
unavailable, while a measured zero duration is valid. Tooltips define each phase.
Reaching the horizon preserves in-progress attempts; Stop clears the run's results.

`src/scenarios/types.ts` mirrors the backend document. Integer values may be safe
JSON numbers or exact decimal strings; keep strings unchanged when sending or
displaying the scenario. Nanosecond fields retain their explicit units. Scenario
documents use `scenarioVersion: 2`; live and terminal snapshots use
`snapshotVersion: 1`. Reports use `reportVersion: 1`. Each field versions its own
contract, and the generic `version` field is rejected. See the
[scenario contract](../examples/scenarios/CONTRACT.md) for the document format.

`presets.ts` exposes 37 scenarios across links, queues, loss, workloads, routing,
edge cases, performance and reference experiments. Search by title/description or filter by category;
the library shows six cards per page. Choosing or filtering never starts a run.
The original four topology presets still inherit backend defaults. The other
network and reference factories in `scenarios/presets/` use a fixed baseline so CLI overrides
do not silently change the experiment being tested. Ordinary experiments run
for 180 virtual seconds at speed 1, with 1 GiB per client unless their workload
deliberately includes short or empty streams.

**Reference experiments** contains eight 60-second cases crossing backend 2's
4/40 ms base travel RTT, 1 Gbit/s/64 kbit/s return link and 16 KiB/1 MiB payload
per client. Backend 1 and the remaining settings stay fixed. See the
[reference guide](../examples/scenarios/REFERENCE_EXPERIMENTS.md) for the matrix
and the distinction between base travel RTT and measured RTT.

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
speed 0, zero travel/loss/gaps, a 1-ms virtual horizon and snapshots every 100 ms of real time,
1 GiB per client, a 20-million-event cap, 4096-slot endpoint queues and
131072-slot LB input queues. Sparse snapshots limit telemetry work at this scale.
Created clients may remain in handshake because backend half-open capacity is
still 100 each. The 1 ns transmission floor and current frontend rendering costs
remain part of the experiment.

The separate **Simulator throughput** preset uses 50 clients and 5 backends with
10 TB/s client links, 100 TB/s backend links and shared LB limits of 1 PB/s. It
removes travel delays, sender pauses and random loss, and runs without pacing
for 5 virtual milliseconds. Snapshots target an interval of 100 real milliseconds;
the event limit is 50 million and queues have 4096 slots. Each client's 1 GiB
stream remains unfinished when the benchmark reaches its time horizon.

The snapshot interval controls real time between UI updates at any playback
speed. The final state is published immediately when a run ends.

`SimulationPerformance.tsx` appears on both pages when the backend supplies
`simulation.performance`. It shows cumulative LB packets and DES events per real
second, wall time and work totals. The backend includes setup, pacing and snapshot
work in the measurement and freezes it after completion or failure. Stop clears
it. These rates measure simulator execution, independently of virtual-time TX/RX.
With the throughput preset, transmission durations reach the model's 1 ns floor;
the large configured bitrates do not represent literal unlimited bandwidth.

`ExperimentSetup.tsx` owns one native scenario and its controlled form inputs.
The **Scenario JSON document** editor shows the complete applied draft, including
`fixed` settings and all `states`. Paste or edit JSON, then choose **Apply JSON**
to check it with the same strict local parser and authoritative server validation.
Invalid text stays editable and leaves the applied draft intact. **Discard JSON
edits** restores that draft. Pending JSON edits disable the form, preset loading,
file actions and Start, and the planned topology asks you to apply or discard them.
The editor locks during validation, so a response cannot apply superseded text.
JSON input and imported files are limited to 2 MiB of UTF-8 text; exact decimal
integer strings and resource identities are preserved. Applying JSON replaces the
whole document and does not start a simulation.

Form and host edits also check the complete timeline before updating the applied
draft. Rejected text stays visible, and related pending values are retried when a
correcting edit makes them valid. Common backend CPU inputs take effect only with
**Apply to all backends**; a rejected Apply retains those inputs. Until pending
inputs are corrected, applied or discarded, Start, file actions, JSON editing,
preset loading and state changes stay disabled. **Discard unapplied form inputs**
restores the form from the applied draft, preserving every accepted edit and
resource identity. It does not reset the document or affect the active run.

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

The **Configuration timeline** selects complete states and supports **Add state**,
**Copy state**, **Move state** and **Delete state**. Enter the destination in virtual
seconds with up to nine decimal places; the document retains canonical, exact
nanosecond keys. Every state must be strictly before the run duration, destination
times must be unused, and the initial state at zero cannot be moved or deleted.
Add copies the configuration active at the destination time; Copy duplicates the
selected state. Delete selects its predecessor. Each operation validates the full
candidate before replacing the draft, so rejected edits preserve the document and
selection. Fixed settings apply to the whole run; scope chips identify sections
that edit the selected state, including mixed traffic controls.

Copies preserve resource identities, profiles, overrides and group lifetime birth
budgets. They do not restart arrivals. Moving or deleting an early state can
invalidate a group's fixed start time or a later client override; the editor
explains these conflicts without silently changing identities or start times.
Correct invalid form inputs and apply staged common CPU inputs before selecting
or changing states. Unapplied JSON edits also lock the timeline controls.

The single Start action sends the native draft through HTTP for server validation
and admission. Invalid visible input blocks Start. Drafts survive Stop, navigation
and reconnect.

## Scenario files

The **Scenario files** panel has **Import JSON**, **Export JSON** and
**Validate draft** actions. Import sends the original UTF-8 file text to the
backend before browser JSON parsing, with a 2 MiB byte limit. Only successful
validation replaces the draft; malformed or invalid files leave it unchanged.
Import and validation never start a run. Correct or discard invalid form values
and apply or discard common backend CPU inputs before using file actions.

Export downloads the current native draft with its groups, profiles and overrides,
including manual edits. **Native scenario** previews that export/Start document.
**Effective configuration** shows the optional `resolved` result from backend
validation: inherited link settings and streams for backends, groups and explicit
client overrides. It is read-only and is not an import format. The active run's
original scenario remains a separate document.

See [example scenarios](../examples/scenarios/README.md) for all 37 catalog
experiments, a smaller directional/client-override example and two timeline
contract examples. JSON duration
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

Non-idle frames also identify a server publication with `simulation.publicationSequence`,
a canonical positive uint64 decimal string. The hook validates this without converting
it to a JavaScript number, then tracks delivery before topology coalescing or lifecycle
updates. Its `publicationObservation` exposes the latest delivery kind, first observed
and highest sequence, exact decimal missing counts, and whether this is a new publication.
An attachment at sequence N establishes a baseline; it makes no claim about 1 through N-1.
Gaps count missing publications after that baseline, including gaps across reconnects.
Stale frames cannot regress the view. A reconnect retains the current run's watermark
and allows one cached duplicate to rebuild the view without counting a new measurement.
Idle and a different run reset this bounded scope; there is no retained run history,
replay, or guarantee of complete historical measurements.

Run completion arrives as a nonreplaceable `simulationEnded` message with its run ID,
reason (`finished`, `failed`, or `stopped`), final snapshot and matching topology
definition. Definitions stay usable even if HTTP already serves the following run.
A Stop before model creation has no final sample. Stop captures metrics before cleanup;
its private acknowledgement includes that sample, while clearing a finished run reuses
the original final measurement without producing another end.

The hook's synchronous `onRunEnded` and `onFinalSample` callbacks run before React
coalescing. The live view accepts fast completions without a preceding running frame,
and older runs cannot replace a currently active run. Critical queue overflow closes
the connection; an observed active run then has an explicitly uncertain termination.
Reconnect alone cannot resolve that uncertainty or restore missed history.

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

## Exact telemetry for Results

WebSocket messages and HTTP responses use `readTelemetryJSON`. It decodes JSON once,
retaining a separate exact source for each decoded object. Large integer tokens become
decimal strings in that source; fractional measurements remain numbers. Live views keep
their existing numeric projections and may approximate large values for display.

Use `telemetrySource(snapshot)` or `telemetrySource(definition)` before view hydration or
coalescing when collecting Results. Embedded final snapshots and topology definitions
also retain their own sources. Archive/export this source, not the numeric live view.
The accessor rejects unregistered unsafe numbers because their original digits cannot
be recovered. Go node metrics also retain original integer tokens before conversion to
their numeric display values; serialization uses those tokens without adding wire fields.

## Results collection

The application collects exact samples synchronously before live-view hydration and
React updates. Each sample keeps its publication number, receipt time and virtual
time. Different publications at the same virtual time remain separate. Topology
definitions are archived once per run and revision, including definitions embedded
in end and Stop acknowledgements.

The collection retains up to 32 MiB of serialized UTF-8 data across eight runs,
including a 2 MiB reserve for terminal measurements. This bounds the stored data
representation, not the browser's JavaScript heap. Samples and definitions that
cannot fit are counted explicitly; earlier samples are never silently downsampled.
Each run also retains at most 256 observation markers for gaps and reconnects.
Saving completed results, history and export are implemented in the following
Epic 3 stages; this collector does not write to the backend or localStorage.
