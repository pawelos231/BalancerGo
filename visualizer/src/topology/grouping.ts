import { formatBandwidth } from './format.ts';
import type {
  TopologyGraph,
  TopologyLink,
  TopologyLinkStats,
  TopologyNode,
  TopologySelection,
  TopologyTraffic,
} from './types';

// All members belong to this group; collapsedClients are still drawn as one node.
export interface TopologyGroup {
  id: string;
  nodeId: string;
  label: string;
  clients: TopologyNode[];
  collapsedClients: TopologyNode[];
  expanded: boolean;
}

// One view arrow stands for these physical links with matching link settings.
export interface TopologyBundle {
  id: string;
  links: TopologyLink[];
  groupId: string;
}

// The drawing keeps member lists so the inspector can return to physical links.
export interface GroupedTopology {
  graph: TopologyGraph;
  groups: TopologyGroup[];
  bundles: Map<string, TopologyBundle>;
}

interface BundleProjection {
  edge: TopologyLink;
  bundle: TopologyBundle;
}

// Actual DTO IDs remain untouched even if a future resource uses our view prefix.
function uniqueViewId(base: string, usedIds: Set<string>): string {
  let id = base;
  let suffix = 1;

  while (usedIds.has(id)) {
    id = `${base}/${suffix}`;
    suffix++;
  }

  usedIds.add(id);
  return id;
}

function collapsedNode(group: TopologyGroup): TopologyNode {
  const clients = group.collapsedClients;
  const activeClients = clients.filter((client) => client.active === true).length;
  const allActivityKnown = clients.every((client) => client.active !== undefined);

  let active: boolean | undefined;
  if (activeClients > 0) {
    active = true;
  } else if (allActivityKnown) {
    active = false;
  }

  return {
    id: group.nodeId,
    kind: 'clientGroup',
    label: group.label,
    groupId: group.id,
    description: 'These clients have independent directed links. Matching link settings are bundled in this view.',
    active,
    traffic: collapsedTraffic(clients),
    metrics: [
      {
        key: 'clientCount',
        label: 'Clients',
        value: clients.length,
        unit: 'clients',
      },
      {
        key: 'activeClients',
        label: allActivityKnown ? 'Active clients' : 'Known active clients',
        value: activeClients,
        unit: 'clients',
      },
    ],
  };
}

// Sum only represented clients from the same complete window; missing data stays unknown.
function collapsedTraffic(clients: TopologyNode[]): TopologyTraffic | undefined {
  const sampleWindowSeconds = clients[0]?.traffic?.sampleWindowSeconds;
  if (sampleWindowSeconds === undefined || sampleWindowSeconds <= 0) return undefined;

  let transmitBytesPerSecond = 0;
  let receiveBytesPerSecond = 0;
  for (const client of clients) {
    const traffic = client.traffic;
    if (!traffic || traffic.sampleWindowSeconds !== sampleWindowSeconds) return undefined;
    transmitBytesPerSecond += traffic.transmitBytesPerSecond;
    receiveBytesPerSecond += traffic.receiveBytesPerSecond;
  }

  return { transmitBytesPerSecond, receiveBytesPerSecond, sampleWindowSeconds };
}

// Only the projected counters are changed; each physical link keeps its own DTO.
function addStats(total: TopologyLinkStats, stats: TopologyLinkStats) {
  total.queuedPackets += stats.queuedPackets;
  total.transmitting = total.transmitting || stats.transmitting;
  total.travellingPackets += stats.travellingPackets;
  total.offeredPackets += stats.offeredPackets;
  total.offeredBytes += stats.offeredBytes;
  total.acceptedPackets += stats.acceptedPackets;
  total.acceptedBytes += stats.acceptedBytes;
  total.queueDroppedPackets += stats.queueDroppedPackets;
  total.queueDroppedBytes += stats.queueDroppedBytes;
  total.lostPackets += stats.lostPackets;
  total.lostBytes += stats.lostBytes;
  total.deliveredPackets += stats.deliveredPackets;
  total.deliveredBytes += stats.deliveredBytes;
}

// Collapse client groups without changing the physical graph or shared resources.
export function groupTopology(
  topology: TopologyGraph,
  expandedGroups: ReadonlySet<string>,
  selection: TopologySelection,
): GroupedTopology {
  const clientsByGroup = new Map<string, TopologyNode[]>();

  for (const node of topology.nodes) {
    if (node.kind !== 'client' || !node.groupId) {
      continue;
    }

    const clients = clientsByGroup.get(node.groupId);
    if (clients) {
      clients.push(node);
    } else {
      clientsByGroup.set(node.groupId, [node]);
    }
  }

  const usedNodeIds = new Set(topology.nodes.map((node) => node.id));
  const groups: TopologyGroup[] = [];
  const groupByClient = new Map<string, TopologyGroup>();

  for (const [id, clients] of clientsByGroup) {
    if (clients.length < 2) {
      continue;
    }

    const nodeId = uniqueViewId(`view/client-group/${encodeURIComponent(id)}`, usedNodeIds);
    const group: TopologyGroup = {
      id,
      nodeId,
      label: `Group ${id}`,
      clients,
      collapsedClients: [],
      expanded: expandedGroups.has(id),
    };
    groups.push(group);

    for (const client of clients) {
      groupByClient.set(client.id, group);
    }
  }

  // A physical selection reveals its clients, rather than expanding the whole group.
  const exposedClients = new Set<string>();
  if (selection?.kind === 'node' && groupByClient.has(selection.id)) {
    exposedClients.add(selection.id);
  }
  if (selection?.kind === 'link') {
    const selectedLink = topology.links.find((link) => link.id === selection.id);
    if (selectedLink) {
      if (groupByClient.has(selectedLink.source)) {
        exposedClients.add(selectedLink.source);
      }
      if (groupByClient.has(selectedLink.target)) {
        exposedClients.add(selectedLink.target);
      }
    }
  }

  const hiddenClients = new Map<string, TopologyGroup>();
  for (const group of groups) {
    group.collapsedClients = group.expanded
      ? []
      : group.clients.filter((client) => !exposedClients.has(client.id));

    for (const client of group.collapsedClients) {
      hiddenClients.set(client.id, group);
    }
  }

  const bundles = new Map<string, TopologyBundle>();
  if (hiddenClients.size === 0) {
    return { graph: topology, groups, bundles };
  }

  const nodes: TopologyNode[] = [];
  const insertedGroups = new Set<string>();
  for (const node of topology.nodes) {
    const group = hiddenClients.get(node.id);
    if (!group) {
      nodes.push(node);
    } else if (!insertedGroups.has(group.id)) {
      nodes.push(collapsedNode(group));
      insertedGroups.add(group.id);
    }
  }

  const links: TopologyLink[] = [];
  const projections = new Map<string, BundleProjection>();
  const usedLinkIds = new Set(topology.links.map((link) => link.id));

  for (const link of topology.links) {
    const sourceGroup = hiddenClients.get(link.source);
    const targetGroup = hiddenClients.get(link.target);
    const group = sourceGroup ?? targetGroup;
    if (!group) {
      links.push(link);
      continue;
    }

    const source = sourceGroup?.nodeId ?? link.source;
    const target = targetGroup?.nodeId ?? link.target;
    const config = link.config;
    const key = JSON.stringify([
      group.id,
      source,
      target,
      config.bandwidthBitsPerSecond,
      config.packetTravelTimeMs,
      config.queueCapacity,
      config.lossRate,
    ]);
    const existing = projections.get(key);

    if (existing) {
      existing.bundle.links.push(link);
      addStats(existing.edge.stats, link.stats);
      continue;
    }

    const id = uniqueViewId(`view/client-bundle/${encodeURIComponent(key)}`, usedLinkIds);
    const bundle: TopologyBundle = { id, links: [link], groupId: group.id };
    const edge: TopologyLink = {
      id,
      label: '',
      source,
      target,
      config: { ...config },
      stats: { ...link.stats },
    };
    projections.set(key, { edge, bundle });
    bundles.set(id, bundle);
    links.push(edge);
  }

  for (const { edge, bundle } of projections.values()) {
    edge.memberCount = bundle.links.length;
    edge.label = `${bundle.links.length} × ${formatBandwidth(edge.config.bandwidthBitsPerSecond)}`;
  }

  return { graph: { ...topology, nodes, links }, groups, bundles };
}
