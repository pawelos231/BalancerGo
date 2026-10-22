# Load balancer visualizer

React, TypeScript and Vite frontend for the Go simulator. Use Node.js 24 and npm.
Start the backend with `go run .` from `simulator/`, then run from this directory:

```sh
npm ci
npm run dev
```

Open `http://localhost:5173`. The default WebSocket URL is
`ws://localhost:8080/ws`; set `VITE_WS_URL` before starting Vite to override it.
For Docker Compose, ports and build instructions, see the [root README](../README.md).

## Pages

- **Simulation** (`/`): edit configuration, click **Start simulation**, monitor
  backends, or **Stop and clear** the experiment.
- **Topology** (`/topology`): explore the live graph, pan and zoom, and select a
  node or directed link to inspect its settings and counters. Client groups start
  collapsed. Expand a group, select one of its members, or search for any client
  or physical link. Search covers the full graph, including hidden members.

`src/hooks/useSimulation.ts` owns the WebSocket, snapshots and lifecycle actions.
`src/App.tsx` shares that hook across both pages. The configuration form stays
mounted when switching pages, preserving
its draft. Static hosting must serve `index.html` for `/topology`; the container's
`local-web-server --spa index.html` already does this.

## Topology data and extension points

The backend snapshot supplies `topology.nodes` and `topology.links` from the
actual simulation wiring. Each created client and backend has its own node and
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
  scenario defaults, profiles and endpoint overrides; the current form supplies
  uniform endpoint defaults and separate shared ingress/forwarding limits.
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
grouping, physical counter scopes and bounded graph selection without adding a
test framework dependency.

Build output is written to `dist/`. `VITE_WS_URL` is embedded at build time, so
changing the backend URL for a static deployment requires rebuilding.
