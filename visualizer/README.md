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
  node or directed link to inspect its settings and counters. Search covers the
  full graph, including elements outside the visible subset.

`src/hooks/useSimulation.ts` owns the WebSocket, snapshots and lifecycle actions.
`src/App.tsx` shares that hook across both pages. The configuration form stays
mounted when switching pages, preserving
its draft. Static hosting must serve `index.html` for `/topology`; the container's
`local-web-server --spa index.html` already does this.

## Topology data and extension points

The backend snapshot supplies `topology.nodes` and `topology.links` from the
actual simulation wiring. Clients are aggregated into one node; forward and
return links are separate arrows. Idle and cleared experiments have an empty
graph; finished or failed runs retain their final snapshot.

- `src/topology/types.ts` defines the graph DTO. Node `kind` is an open string:
  unknown kinds use a generic renderer. Node metrics use `key`, `label`, numeric
  `value` and optional `unit`. Preserve stable node and link IDs; link `source`
  and `target` reference node IDs independently of labels.
- Each link carries its own `config` and `stats`, including bandwidth, packet
  travel time, queue capacity, loss and current packet counters. The view already
  displays different bandwidths per link. The current backend still initializes
  all links from one global configuration.
- `src/topology/layout.ts` bounds the canvas to 200 nodes and 500 links while
  keeping every element searchable; selection brings its context into view.
  Search displays up to 50 matches. `structureKey` uses node IDs and link
  IDs/endpoints, so Dagre reruns when the structure changes; metric and capacity
  updates reuse the existing layout.
- `TopologyNodeView.tsx` and `TopologyLinkView.tsx` render nodes and arrows;
  `TopologyCanvas.tsx` handles graph interaction. `TopologyInspector.tsx` presents
  selection details separately, using generic node metrics and per-link data.
  `src/pages/TopologyPage.tsx` coordinates search and selection.

For additional topologies or load-balancer resources, implement the backend
model and extend its topology projection first. Then update DTOs and renderers
to display those resources and their actual metrics.

## Build and check

```sh
npm run build
npm run lint
```

Build output is written to `dist/`. `VITE_WS_URL` is embedded at build time, so
changing the backend URL for a static deployment requires rebuilding.
