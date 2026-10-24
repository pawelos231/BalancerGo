import dagre from '@dagrejs/dagre';
import type { TopologyGraph, TopologySelection } from './types';

export const NODE_WIDTH = 240;
export const NODE_HEIGHT = 200;
export const LOAD_BALANCER_HEIGHT = 336;
export const MAX_VISIBLE_NODES = 200;
export const MAX_VISIBLE_LINKS = 500;

interface StructuralLink {
  id: string;
  source: string;
  target: string;
}

interface StructuralNode {
  id: string;
  kind: string;
}

interface Structure {
  nodes: StructuralNode[];
  links: StructuralLink[];
}

interface NodePosition {
  x: number;
  y: number;
}

interface LinkRoute {
  sourceHandle: string;
  targetHandle: string;
  lane: number;
}

export interface TopologyLayout {
  positions: Map<string, NodePosition>;
  routes: Map<string, LinkRoute>;
  domainLayout: boolean;
}

// All nodes reserve room for measured traffic; the LB also shows limits and queues.
export function nodeHeight(kind: string): number {
  return kind === 'loadBalancer' ? LOAD_BALANCER_HEIGHT : NODE_HEIGHT;
}

// Bound layout and rendering while keeping selected elements reachable.
export function visibleGraph(graph: TopologyGraph, selection: TopologySelection): TopologyGraph {
  const nodesById = new Map(graph.nodes.map((node) => [node.id, node]));
  const visibleIds = new Set<string>();

  function addVisibleNode(id: string) {
    if (nodesById.has(id) && visibleIds.size < MAX_VISIBLE_NODES) {
      visibleIds.add(id);
    }
  }

  if (selection?.kind === 'node') {
    addVisibleNode(selection.id);
  }

  if (selection?.kind === 'link') {
    const link = graph.links.find((candidate) => candidate.id === selection.id);

    if (link) {
      addVisibleNode(link.source);
      addVisibleNode(link.target);
    }
  }

  const focusedIds = new Set(visibleIds);

  // Keep shared limits in view even when an expanded group exceeds the canvas cap.
  for (const node of graph.nodes) {
    const sharedResource = node.kind === 'loadBalancer'
      || node.kind === 'networkAggregator';
    if (sharedResource) {
      addVisibleNode(node.id);
    }
  }

  for (const link of graph.links) {
    if (focusedIds.has(link.source)) {
      addVisibleNode(link.target);
    }
    if (focusedIds.has(link.target)) {
      addVisibleNode(link.source);
    }
  }

  for (const node of graph.nodes) {
    addVisibleNode(node.id);
  }

  const candidates = graph.links.filter((link) => (
    visibleIds.has(link.source) && visibleIds.has(link.target)
  ));
  const retainedLinks = new Set<string>();
  if (selection?.kind === 'link' && candidates.some((link) => link.id === selection.id)) {
    retainedLinks.add(selection.id);
  }

  // The shared input and selected arrow stay visible when the link cap is reached.
  for (const link of candidates) {
    const source = nodesById.get(link.source);
    const target = nodesById.get(link.target);
    const sharedInput = source?.kind === 'networkAggregator'
      && target?.kind === 'loadBalancer';
    if (sharedInput && retainedLinks.size < MAX_VISIBLE_LINKS) {
      retainedLinks.add(link.id);
    }
  }
  for (const link of candidates) {
    if (retainedLinks.size >= MAX_VISIBLE_LINKS) break;
    retainedLinks.add(link.id);
  }

  // Preserve source order so prioritizing an arrow does not shuffle the layout.
  const links = candidates.filter((link) => retainedLinks.has(link.id));

  return {
    ...graph,
    nodes: graph.nodes.filter((node) => visibleIds.has(node.id)),
    links,
  };
}

// Exclude live values so statistics updates cannot trigger another layout.
export function structureKey(graph: TopologyGraph): string {
  const structure: Structure = {
    nodes: graph.nodes.map(({ id, kind }) => ({ id, kind })),
    links: graph.links.map(({ id, source, target }) => ({ id, source, target })),
  };

  return JSON.stringify(structure);
}

function createPlacementGraph(structure: Structure) {
  const graph = new dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));

  graph.setGraph({
    rankdir: 'LR',
    nodesep: 42,
    ranksep: 180,
    marginx: 28,
    marginy: 28,
  });

  for (const node of structure.nodes) {
    graph.setNode(node.id, { width: NODE_WIDTH, height: nodeHeight(node.kind) });
  }

  // Reciprocal links share placement but retain separate rendered arrows.
  const relationships = new Set<string>();

  for (const { source, target } of structure.links) {
    const pair = JSON.stringify([source, target].sort());

    if (!relationships.has(pair)) {
      graph.setEdge(source, target);
      relationships.add(pair);
    }
  }

  return graph;
}

function createLinkRoutes(
  structure: Structure,
  positions: Map<string, NodePosition>,
  domainLayout: boolean,
) {
  const routes = new Map<string, LinkRoute>();
  const parallel = new Map<string, number>();
  const kinds = new Map(structure.nodes.map((node) => [node.id, node.kind]));

  for (const link of structure.links) {
    const source = positions.get(link.source)!;
    const target = positions.get(link.target)!;
    const sameColumn = target.x === source.x;
    const rightward = target.x > source.x || (sameColumn && target.y >= source.y);
    const direction = JSON.stringify([link.source, link.target]);
    const lane = parallel.get(direction) ?? 0;

    parallel.set(direction, lane + 1);

    let sourceHandle = rightward ? 'out-right' : 'out-left';
    let targetHandle = rightward && link.source !== link.target ? 'in-left' : 'in-right';
    let routedLane = lane;

    if (domainLayout) {
      const sourceKind = kinds.get(link.source);
      const targetKind = kinds.get(link.target);
      const frontInput = sourceKind === 'client' || sourceKind === 'clientGroup';
      const backOutput = sourceKind === 'loadBalancer' && targetKind === 'backend';
      const upperPath = frontInput || backOutput;
      sourceHandle = upperPath ? 'out-right' : 'out-left';
      targetHandle = upperPath ? 'in-left' : 'in-right';
      // Opposite directions fan away from one another around their own ports.
      routedLane = upperPath ? -(lane + 1) : lane + 1;
    }

    routes.set(link.id, {
      sourceHandle,
      targetHandle,
      lane: routedLane,
    });
  }

  return routes;
}

// Recognized endpoint stars use stable front/LB/back columns; other graphs use Dagre.
function simpleLoadBalancer(structure: Structure): StructuralNode | undefined {
  const balancers = structure.nodes.filter((node) => node.kind === 'loadBalancer');
  if (balancers.length !== 1) {
    return undefined;
  }
  const balancer = balancers[0];
  const knownKinds = new Set(['client', 'clientGroup', 'loadBalancer', 'backend']);
  const nodeIds = new Set(structure.nodes.map((node) => node.id));
  const knownNodes = structure.nodes.every((node) => knownKinds.has(node.kind));
  const starLinks = structure.links.every((link) => (
    nodeIds.has(link.source)
    && nodeIds.has(link.target)
    && link.source !== link.target
    && (link.source === balancer.id || link.target === balancer.id)
  ));
  return knownNodes && starLinks ? balancer : undefined;
}

// Center each endpoint column on the taller LB while preserving source order.
function placeEndpointStar(structure: Structure, balancer: StructuralNode): Map<string, NodePosition> {
  const clients = structure.nodes.filter((node) => (
    node.kind === 'client' || node.kind === 'clientGroup'
  ));
  const backends = structure.nodes.filter((node) => node.kind === 'backend');
  const rowStep = NODE_HEIGHT + 42;
  const columnHeight = (count: number) => count > 0 ? count * rowStep - 42 : 0;
  const totalHeight = Math.max(
    columnHeight(clients.length),
    columnHeight(backends.length),
    LOAD_BALANCER_HEIGHT,
  );
  const centerY = 28 + totalHeight / 2;
  const positions = new Map<string, NodePosition>();
  const columnStep = NODE_WIDTH + 220;

  for (const [column, nodes] of [[0, clients], [2, backends]] as const) {
    const top = centerY - columnHeight(nodes.length) / 2;
    nodes.forEach((node, index) => {
      positions.set(node.id, { x: 28 + column * columnStep, y: top + index * rowStep });
    });
  }
  positions.set(balancer.id, {
    x: 28 + columnStep,
    y: centerY - LOAD_BALANCER_HEIGHT / 2,
  });
  return positions;
}

// Placement depends on roles and wiring, never current queue activity.
export function layoutTopology(key: string): TopologyLayout {
  const structure = JSON.parse(key) as Structure;
  const balancer = simpleLoadBalancer(structure);
  if (balancer) {
    const positions = placeEndpointStar(structure, balancer);
    return {
      positions,
      routes: createLinkRoutes(structure, positions, true),
      domainLayout: true,
    };
  }
  const graph = createPlacementGraph(structure);

  dagre.layout(graph);

  const positions = new Map(structure.nodes.map(({ id, kind }) => {
    const node = graph.node(id);

    return [id, {
      x: node.x - NODE_WIDTH / 2,
      y: node.y - nodeHeight(kind) / 2,
    }] as const;
  }));
  const routes = createLinkRoutes(structure, positions, false);

  return { positions, routes, domainLayout: false };
}
