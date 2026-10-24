import type { LoadBalancerState } from '../types';
import type { TopologyMetric } from '../topology/types';
import type { MetricDefinition, SimulationSnapshot, TopologyDefinition } from './types.ts';

function indexById<T extends { id: string }>(items: T[]): Map<string, T> {
  const index = new Map(items.map((item) => [item.id, item]));
  if (index.size !== items.length) throw new Error('Topology contains duplicate resource IDs.');
  return index;
}

function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error('Topology definitions do not cover the live resources.');
  return value;
}

function metricValue(definition: MetricDefinition, values: Record<string, number>): TopologyMetric {
  if (definition.value !== undefined) return definition as TopologyMetric;
  return { ...definition, value: required(values[definition.key]) };
}

// Hydration reuses HTTP settings and resolves routed clients by their physical IDs.
export function hydrateSnapshot(
  frame: SimulationSnapshot,
  definition: TopologyDefinition,
): LoadBalancerState {
  if (frame.simulation.runId !== definition.runId || frame.topology.revision !== definition.revision) {
    throw new Error('Cannot combine different runs or topology revisions.');
  }

  const nodeStates = indexById(frame.topology.nodes);
  const linkStates = indexById(frame.topology.links);
  const nodeDefinitions = indexById(definition.nodes);
  const serverDefinitions = indexById(definition.servers);
  if (nodeStates.size !== definition.nodes.length || linkStates.size !== definition.links.length) {
    throw new Error('Topology definitions do not match the live resource set.');
  }

  const nodes = definition.nodes.map((node) => {
    const state = required(nodeStates.get(node.id));
    return {
      ...node,
      metrics: node.metrics.map((metric) => metricValue(metric, state.metrics)),
      active: state.active,
      traffic: state.traffic,
    };
  });
  const links = definition.links.map((link) => {
    required(nodeDefinitions.get(link.source));
    required(nodeDefinitions.get(link.target));
    return { ...link, stats: required(linkStates.get(link.id)).stats };
  });

  let ingress;
  if (definition.ingress) {
    const state = required(frame.topology.ingress);
    const queues = indexById(state.queues);
    ingress = {
      config: definition.ingress.config,
      stats: state.stats,
      queues: definition.ingress.queues.map((queue) => ({
        ...queue,
        stats: required(queues.get(queue.id)).stats,
      })),
    };
  }

  const servers = frame.servers.map((server) => {
    const identity = required(serverDefinitions.get(server.id));
    return {
      server: {
        ID: identity.id,
        Name: identity.name,
        Address: identity.address,
        Port: identity.port,
        Active: server.active,
        ActiveConnections: server.activeConnections,
        HalfOpenConnections: server.halfOpenConnections,
        LatencyP95Milli: server.latencyP95Milli,
        NumberOfHandledPackets: server.numberOfHandledPackets,
      },
      clients: server.clients.map((client) => ({
        Key: required(required(nodeDefinitions.get(client.id)).clientKey),
        RetrySyn: client.retrySyn,
        RetryData: client.retryData,
      })),
    };
  });

  return {
    mode: frame.mode,
    algorithm: frame.algorithm,
    simulation: frame.simulation,
    servers,
    topology: { nodes, links, ingress },
  };
}

// Idle is self-contained and clears the graph without waiting for HTTP.
export function idleSnapshot(frame: SimulationSnapshot): LoadBalancerState {
  return {
    mode: frame.mode,
    algorithm: frame.algorithm,
    simulation: frame.simulation,
    servers: [],
    topology: { nodes: [], links: [] },
  };
}
