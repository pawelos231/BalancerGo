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
  backends, **Stop and clear**, or **Stop and save to Results**. The **Scenario library** offers
  ready experiments with different client/backend rates and shared LB bottlenecks.
  Click **Load preset** on a card to validate and apply its complete JSON to the editor
  and planned topology, then use the single **Start simulation** button. Loading a
  preset does not start a run.
- **Topology** (`/topology`): explore the live graph, pan and zoom, and select a
  node or directed link to inspect its settings and counters. Client groups start
  collapsed. Expand a group, select one of its members, or search for any client
  or physical link. Search covers the full graph, including hidden members.
- **Planned topology** (`/planned-topology`): inspect the applied scenario's
  topology before starting it. The timestamp slider selects only declared states;
  node and directed-link inspectors show configured resources and effective links.
  Exact nanosecond keys remain intact, including values above 2^53. A single-state
  scenario has a disabled slider at its only timestamp. This preview does not run
  the model or predict traffic and counters.
- **Results** (`/results`): browse saved observations without the backend, inspect
  summaries and coverage, retry unsaved candidates, recover records and delete
  history entries. Lists and resource tables are paginated.

`src/hooks/useSimulation.ts` owns the WebSocket, snapshots and lifecycle actions.
It starts scenarios and retrieves the current run's configuration through HTTP.
`src/App.tsx` shares that hook across pages. The scenario editor stays
mounted when switching pages, preserving
its draft. Static hosting must serve `index.html` for all page routes; the container's
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
Reaching the horizon preserves in-progress attempts. Stop resets the live model;
completed Results archives remain separate from that reset.

`src/scenarios/types.ts` mirrors the backend document. Integer values may be safe
JSON numbers or exact decimal strings; keep strings unchanged when sending or
displaying the scenario. Nanosecond fields retain their explicit units. Scenario
documents use `scenarioVersion: 2`; live and terminal snapshots use
`snapshotVersion: 1`. Reports use `reportVersion: 1`, completed Results use
`resultVersion: 1`, and local storage envelopes use `storageVersion: 1`. Each field versions its own
contract, and the generic `version` field is rejected. See the
[scenario contract](../examples/scenarios/CONTRACT.md) for the document format.

`presets.ts` exposes 37 scenarios across links, queues, loss, workloads, routing,
edge cases, performance and reference experiments. Search by title/description or filter by category;
the library shows six cards per page. Each card has a **Load preset** button;
loading or filtering never starts a run.
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

`ExperimentSetup.tsx` owns one applied native scenario. The configuration panel
has **JSON** and **Language** formats, each with a retained text buffer.
The **Scenario JSON document** editor shows the complete applied draft, including
`fixed` settings and all `states`. Paste or edit JSON, then choose **Apply JSON**
to check it with the same strict local parser and authoritative server validation.
Invalid text stays editable and leaves the applied draft intact. **Discard JSON
edits** restores that draft. Pending edits disable preset loading,
file actions and Start, and the planned topology asks you to apply or discard them.
The editor locks during validation, so a response cannot apply superseded text.
JSON input and imported files are limited to 2 MiB of UTF-8 text; exact decimal
integer strings and resource identities are preserved. Applying JSON replaces the
whole document and does not start a simulation.

In **Language**, edit the shorter declarative `.lb` source, then choose
**Compile and apply**. The frontend compiles it to native JSON and validates it
through the same server API. Presets and imported JSON select JSON without
overwriting retained source. Returning to source that no longer describes the
applied draft blocks Start until compiled and applied again or JSON is selected.
Apply or discard pending edits before switching formats. No reverse translation
from JSON or extra configuration accordions are needed.

`ScenarioLibrary.tsx` validates and applies a preset directly from its card's
**Load preset** button. Successful validation replaces the complete applied document
and planned topology; failure preserves the previous document. Pending edits
block loading, and validation blocks editing and Start until it completes. `api.ts` wraps
the defaults, validate, Start and current-run endpoints; `runtimeApi.ts` selects
the backend address. `ScenarioRunDetails.tsx` displays the exact initial run
document. Client-generator and routing changes are declared in the scenario
states before Start.

The single Start action sends the native draft through HTTP for server validation
and admission. Pending edits or unapplied active source block Start. Drafts survive Stop, navigation
and reconnect.

## Scenario files

The implemented frontend language is documented in
[Scenario language](docs/scenario-language.md), with a quick start, field/default
reference, timeline rules and five checked experiment recipes. It compiles to the
same JSON v2; the Go CLI continues to accept only native JSON. **Import source**
and **Export source** load/save `.lb` text, preserving comments and unfinished
edits. Import does not apply or start it; choose **Compile and apply** explicitly.

The **Scenario configuration** panel has **Import JSON**, **Export JSON** and
**Validate draft** actions. Import sends the original UTF-8 file text to the
backend before browser JSON parsing, with a 2 MiB byte limit. Only successful
validation replaces the draft; malformed or invalid files leave it unchanged.
Import and validation never start a run. Apply or discard pending JSON edits before
using file actions.

Export downloads the current native draft with its groups, profiles and overrides,
including applied JSON edits. The JSON editor displays that applied draft or your
pending text; export and Start use the applied document after edits are applied
or discarded.
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
Configured link rates are maxima: TCP windows, ACK feedback and other bottlenecks
can prevent their sum from saturating the LB. Drop totals describe physical hops,
not unique payloads.

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
  scenario defaults, profiles and endpoint overrides. The complete JSON editor
  preserves shared limits and detailed endpoint settings in one document.
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
drop packets while another has space. TCP uses sliding receive/congestion windows,
fixed or adaptive RTO and selectable loss recovery. SACK, ECN and delayed ACK
are optional. CPU is a configurable processing-cost model; kernel RX queues,
RSS/NAPI and a complete Linux stack are outside the current model. Queue losses
and throughput describe this simulator, not a validated kernel implementation.

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
The collector itself does not write to the backend or localStorage.
ResultsHistory stores completed artifacts separately from live collection.

Run metadata is attached by its returned run ID. The HTTP run document takes
priority; the admitted submitted document is a separately labelled fallback.
Editing the scenario JSON during a run never changes that run's retained input. A missing
document or topology revision stays visible as missing metadata.

Result assembly uses the terminal publication when available, or explicitly the
latest retained observation. Cumulative counters are not summed across samples.
Removed resources keep their last observation and its time, rather than being
presented as current resources. Assembly creates an independent, versioned result;
later metadata does not mutate an already assembled artifact.

Results lifecycle keeps its own Stop intent, addressed by run and command ID.
A successful end prepares one result; a plain `finished` snapshot alone does not.
`Stop and save to Results` preserves observed data before reset, while ordinary
`Stop and clear` suppresses saving. A rejected or unsent Stop restores normal
end handling. A disconnect leaves its unresolved intent uncertain until matching
evidence arrives; it does not retry or silently save the run.

Eligible results wait for a bounded metadata request and an in-flight local Start
receipt before assembly. Missing data stays explicit after that attempt. Candidate
metrics are independent of the reset live view and survive navigation.

## Local result history

Completed results are written to localStorage above the page routes. Storage has
its own version, independent of `resultVersion`. Full immutable records are stored
separately from the compact history index. Lists read the index and owned record
keys; opening a result validates its complete document without changing exact
counter strings or dropping unknown fields.
Reopening a finished run can replay its cached end. A terminal-only replay is
not stored again when a readable result for that run already exists; separate
observations containing earlier publications remain independent.

The record is written before the index. An index failure leaves the record
recoverable and keeps the candidate in memory. Unindexed records remain visible
by ID; recovery validates one record and restores its summary. Concurrent tabs
can race when updating the index, so it is treated as a cache rather than a
transactional source of truth. Storage events refresh the other tab's list.
Each browser session generates fresh UUID result IDs. Collision checks protect
already stored IDs; localStorage cannot atomically arbitrate two callers that
concurrently reuse the same ID for different records.

Deleting a known run stores a small dismissal marker before removing its record.
This prevents a cached backend end from recreating it after reload. Cleanup keeps
the latest 256 known markers; unknown versions are preserved, and failed cleanup
keeps extra markers rather than undoing a deletion. A failed marker write leaves
the saved record intact. A damaged orphan without a trustworthy run ID can still
be removed, but cannot provide run-level replay suppression.

Unavailable storage, full storage, corrupted data and unsupported versions are
reported without overwriting existing results. Failed saves remain in RAM until
an explicit retry succeeds or the user discards them. Refreshing or closing the
tab can lose those unsaved candidates. Defaults limit a selected stored record
to 64 MiB and the index to 1 MiB before parsing; these are validation limits,
not a guarantee that the browser's localStorage quota can hold that much data.

Result charts use virtual seconds on the horizontal axis. Native wire rates
describe the backend's reported sampling window; acknowledged payload goodput
uses exact byte-counter differences between adjacent retained publications.
Neither is the engine's packets-per-real-second performance measurement.
Queue counters, modeled CPU host-lifetime means and the selected backend's
reported connection-lifetime p95 keep their separate meanings and units.

Missing publications, connection interruptions and unavailable resource values
break plotted lines. Topology revisions remain attached to inspected samples.
Large series use a bounded display projection; the archived samples are unchanged.
Global transfer latency p95/p99 remains unavailable because the source contract
does not provide it.

`Export JSON` downloads the complete `resultVersion: 1` artifact, including its
executed scenario, definitions, every retained source sample, summary, coverage
and provenance. Exact decimal strings and unknown fields are unchanged. This is
separate from exporting `scenario.json` in the configuration editor. Export also
works directly from an unsaved RAM candidate and does not require the backend or
an available localStorage quota.

`Export XLSX` creates nine logical tables: `Run`, `Summary`, `Scenario`, `Samples`,
`Nodes`, `Backends`, `LinksQueues`, `Clients` and `Definitions`. Each field has its
source JSON Pointer, source type, resource and sample context, value, unit and
availability. Unknown fields and empty containers are retained. Backend metrics
come from `servers[]` as well as topology nodes, so backend-only measurements
are not lost. Native units and clock scopes are preserved.

Source strings stay text, including exact decimal counters and formula-looking
labels. Numeric values that would lose precision in Excel also become text,
with their original source type recorded. Unmeasured duration placeholders keep
their raw value and an explicit unavailable status; they are not measured zeros.
Subnormal numbers also remain text because Excel does not support that range
([Microsoft's numeric limits](https://learn.microsoft.com/en-us/troubleshoot/microsoft-365-apps/excel/floating-point-arithmetic-inaccurate-result)).

Large tables continue in numbered sheets. Long text cells continue in numbered
segments with a shared record ID, including long paths and IDs. Concatenating a
text column's segments reconstructs its source value without truncation.
XML-incompatible characters and lone surrogates use JSON-string encoding;
`encodedColumns` names the affected columns. Concatenate their segments first,
then JSON-decode those cells. Ordinary strings keep their original text.
Clearly marked `export-note` rows in `Run` describe layout version 1 and these
reconstruction conventions; they are separate from source fields.

The default limits are 25,000 rows per sheet, 100,000 rows across all sheets
(both include headers), 750,000 populated cells, 16 MiB of UTF-8 text and 64
sheets. Cells hold at most 32,767 UTF-16 code units; continuation preserves
surrogate pairs. These limits count encoded and continued text as exported.

Export preflight counts the expanded rows, populated cells and text before loading the
spreadsheet library. Limits bound the export workload rather than guaranteeing
a particular browser heap size. Oversized results produce an actionable error;
the complete JSON export remains available. Spreadsheet support uses pinned
ExcelJS 4.4.0, loaded on demand from the frontend build without a CDN.
