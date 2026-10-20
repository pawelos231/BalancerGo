import dagre from '@dagrejs/dagre';
import type { TopologyGraph, TopologySelection } from './types';

export const NODE_WIDTH = 240;
export const NODE_HEIGHT = 116;
export const MAX_VISIBLE_NODES = 200;
export const MAX_VISIBLE_LINKS = 500;

interface StructuralLink {
  id: string;
  source: string;
  target: string;
}

interface Structure {
  nodes: string[];
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
  const links = candidates.slice(0, MAX_VISIBLE_LINKS);

  if (selection?.kind === 'link') {
    const selectedIsVisible = links.some((link) => link.id === selection.id);

    // Preserve edge order so an ordinary selection does not rearrange the graph.
    if (!selectedIsVisible) {
      const selectedLink = candidates.find((link) => link.id === selection.id);

      if (selectedLink) {
        const lastIndex = Math.max(0, links.length - 1);
        links.splice(lastIndex, 1, selectedLink);
      }
    }
  }

  return {
    nodes: graph.nodes.filter((node) => visibleIds.has(node.id)),
    links,
  };
}

// Exclude live values so statistics updates cannot trigger another layout.
export function structureKey(graph: TopologyGraph): string {
  const structure: Structure = {
    nodes: graph.nodes.map((node) => node.id),
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

  for (const id of structure.nodes) {
    graph.setNode(id, { width: NODE_WIDTH, height: NODE_HEIGHT });
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

function createLinkRoutes(links: StructuralLink[], positions: Map<string, NodePosition>) {
  const routes = new Map<string, LinkRoute>();
  const parallel = new Map<string, number>();

  for (const link of links) {
    const source = positions.get(link.source)!;
    const target = positions.get(link.target)!;
    const sameColumn = target.x === source.x;
    const rightward = target.x > source.x || (sameColumn && target.y >= source.y);
    const direction = JSON.stringify([link.source, link.target]);
    const lane = parallel.get(direction) ?? 0;

    parallel.set(direction, lane + 1);

    let targetHandle = 'in-right';
    if (rightward && link.source !== link.target) {
      targetHandle = 'in-left';
    }

    routes.set(link.id, {
      sourceHandle: rightward ? 'out-right' : 'out-left',
      targetHandle,
      lane,
    });
  }

  return routes;
}

export function layoutTopology(key: string): TopologyLayout {
  const structure = JSON.parse(key) as Structure;
  const graph = createPlacementGraph(structure);

  dagre.layout(graph);

  const positions = new Map(structure.nodes.map((id) => {
    const node = graph.node(id);

    return [id, {
      x: node.x - NODE_WIDTH / 2,
      y: node.y - NODE_HEIGHT / 2,
    }] as const;
  }));
  const routes = createLinkRoutes(structure.links, positions);

  return { positions, routes };
}
