import dagre from '@dagrejs/dagre';
import type { TopologyGraph, TopologySelection } from './types';

export const NODE_WIDTH = 240;
export const NODE_HEIGHT = 116;
export const MAX_VISIBLE_NODES = 200;
export const MAX_VISIBLE_LINKS = 500;

interface Structure {
  nodes: string[];
  links: { id: string; source: string; target: string }[];
}

export interface TopologyLayout {
  positions: Map<string, { x: number; y: number }>;
  routes: Map<string, { sourceHandle: string; targetHandle: string; lane: number }>;
}

// Bound both layout work and the DOM. A selected element and its immediate
// neighbors take priority; all elements remain reachable through search.
export function visibleGraph(graph: TopologyGraph, selection: TopologySelection): TopologyGraph {
  const known = new Map(graph.nodes.map((node) => [node.id, node]));
  const ids = new Set<string>();
  const add = (id: string) => { if (known.has(id) && ids.size < MAX_VISIBLE_NODES) ids.add(id); };
  if (selection?.kind === 'node') add(selection.id);
  if (selection?.kind === 'link') {
    const link = graph.links.find((candidate) => candidate.id === selection.id);
    if (link) { add(link.source); add(link.target); }
  }
  const focus = new Set(ids);
  for (const link of graph.links) {
    if (focus.has(link.source)) add(link.target);
    if (focus.has(link.target)) add(link.source);
  }
  for (const node of graph.nodes) add(node.id);
  const candidates = graph.links.filter((link) => ids.has(link.source) && ids.has(link.target));
  const links = candidates.slice(0, MAX_VISIBLE_LINKS);
  // Keep selected parallel links without changing ordinary edge order/layout.
  if (selection?.kind === 'link' && !links.some((link) => link.id === selection.id)) {
    const selected = candidates.find((link) => link.id === selection.id);
    if (selected) links.splice(Math.max(0, links.length - 1), 1, selected);
  }
  return { nodes: graph.nodes.filter((node) => ids.has(node.id)), links };
}

// Metrics, status, labels and link capacities do not affect placement. Parse this
// key only when it changes, so frequent snapshots cannot trigger Dagre again.
export function structureKey(graph: TopologyGraph): string {
  return JSON.stringify({ nodes: graph.nodes.map((node) => node.id), links: graph.links.map(({ id, source, target }) => ({ id, source, target })) });
}

export function layoutTopology(key: string): TopologyLayout {
  const structure = JSON.parse(key) as Structure;
  const graph = new dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));
  graph.setGraph({ rankdir: 'LR', nodesep: 42, ranksep: 180, marginx: 28, marginy: 28 });
  for (const id of structure.nodes) graph.setNode(id, { width: NODE_WIDTH, height: NODE_HEIGHT });
  // Reciprocal directions share a relationship for placement only. Every real
  // directed link still gets its own route and arrow in the renderer.
  const relationships = new Set<string>();
  for (const { source, target } of structure.links) {
    const pair = JSON.stringify([source, target].sort());
    if (!relationships.has(pair)) { graph.setEdge(source, target); relationships.add(pair); }
  }
  dagre.layout(graph);
  const positions = new Map(structure.nodes.map((id) => {
    const node = graph.node(id);
    return [id, { x: node.x - NODE_WIDTH / 2, y: node.y - NODE_HEIGHT / 2 }] as const;
  }));
  const routes = new Map<string, { sourceHandle: string; targetHandle: string; lane: number }>();
  const parallel = new Map<string, number>();
  for (const link of structure.links) {
    const source = positions.get(link.source)!;
    const target = positions.get(link.target)!;
    const rightward = target.x > source.x || (target.x === source.x && target.y >= source.y);
    const direction = JSON.stringify([link.source, link.target]);
    const lane = parallel.get(direction) ?? 0;
    parallel.set(direction, lane + 1);
    routes.set(link.id, {
      sourceHandle: rightward ? 'out-right' : 'out-left',
      targetHandle: link.source === link.target ? 'in-right' : rightward ? 'in-left' : 'in-right',
      lane,
    });
  }
  return { positions, routes };
}
