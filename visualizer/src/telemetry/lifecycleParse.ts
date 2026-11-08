import { hydrateSnapshot } from './hydrate.ts';
import { parseSnapshot } from './parse.ts';
import type { SimulationEnded, SimulationSnapshot, TopologyDefinition } from './types.ts';
import type { LoadBalancerState } from '../types.ts';

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function text(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0;
}

function finite(value: unknown): boolean {
  return typeof value === 'number' && Number.isFinite(value);
}

function optionalText(value: unknown): boolean {
  return value === undefined || typeof value === 'string';
}

function linkConfig(value: unknown): boolean {
  return record(value)
    && ['bandwidthBitsPerSecond', 'packetTravelTimeMs', 'queueCapacity', 'lossRate']
      .every((key) => finite(value[key]));
}

function clientKey(value: unknown): boolean {
  return record(value) && ['SrcIP', 'DstIP', 'Protocol'].every((key) => text(value[key]))
    && finite(value.SrcPort) && finite(value.DstPort);
}

const linkCounters = [
  'queuedPackets', 'travellingPackets', 'offeredPackets', 'offeredBytes',
  'acceptedPackets', 'acceptedBytes', 'queueDroppedPackets', 'queueDroppedBytes',
  'lostPackets', 'lostBytes', 'deliveredPackets', 'deliveredBytes',
];
const optionalLinkCounters = [
  'ecnMarkedPackets', 'ecnMarkedBytes', 'congestionDroppedPackets', 'congestionDroppedBytes',
];

function linkStats(value: unknown): boolean {
  return record(value) && typeof value.transmitting === 'boolean'
    && linkCounters.every((key) => finite(value[key]))
    && optionalLinkCounters.every((key) => value[key] === undefined || finite(value[key]));
}

// Hydration forwards these resource objects into views, so check their shape first.
function validFinalResources(frame: SimulationSnapshot): boolean {
  const { topology } = frame;
  const validNodes = topology.nodes.every((node: unknown) => (
    record(node) && text(node.id) && record(node.metrics) && Object.values(node.metrics).every(finite)
    && (node.active === undefined || typeof node.active === 'boolean')
    && ['tcpPhase', 'tcpState', 'closeOutcome'].every((key) => optionalText(node[key]))
  ));
  const validLinks = topology.links.every((link: unknown) => (
    record(link) && text(link.id) && linkStats(link.stats)
  ));
  const ingress: unknown = topology.ingress;
  const validIngress = ingress === undefined || (record(ingress) && linkStats(ingress.stats)
    && Array.isArray(ingress.queues) && ingress.queues.every((queue: unknown) => (
      record(queue) && text(queue.id) && linkStats(queue.stats)
    )));
  const validServers = frame.servers.every((server: unknown) => (
    record(server) && text(server.id) && typeof server.active === 'boolean'
    && ['activeConnections', 'halfOpenConnections', 'latencyP95Milli', 'numberOfHandledPackets']
      .every((key) => finite(server[key]))
    && Array.isArray(server.clients) && server.clients.every((client: unknown) => (
      record(client) && text(client.id) && finite(client.retrySyn) && finite(client.retryData)
    ))
  ));
  return validNodes && validLinks && validIngress && validServers;
}

function numericFields(value: unknown, fields: string[]): boolean {
  return record(value) && fields.every((key) => finite(value[key]));
}

function validFinalMetadata(value: unknown): boolean {
  if (!record(value) || !optionalText(value.error)) return false;
  const { performance, payload, transfers } = value;
  return (performance === undefined || numericFields(performance, [
    'wallTimeSeconds', 'eventsExecuted', 'eventsPerWallSecond',
    'forwardedPackets', 'forwardedPacketsPerWallSecond',
  ]))
    && (payload === undefined || numericFields(payload, ['receivedBytes', 'acknowledgedBytes']))
    && (transfers === undefined || (record(transfers)
      && numericFields(transfers, [
        'started', 'notStarted', 'succeeded', 'failed', 'inProgress',
        'synRetransmissions', 'dataRetransmissions',
      ])
      && record(transfers.failures) && Object.values(transfers.failures).every(finite)
      && ['handshake', 'payload', 'completion'].every((key) => numericFields(transfers[key], [
        'samples', 'meanSeconds', 'minSeconds', 'maxSeconds',
      ]))));
}

export function parseTopologyDefinition(value: unknown): TopologyDefinition {
  const valid = record(value) && text(value.runId) && text(value.revision)
    && /^[1-9]\d*$/.test(value.revision) && !/\D/.test(value.revision)
    && Array.isArray(value.nodes) && value.nodes.every((node: unknown) => (
      record(node) && text(node.id) && text(node.kind) && typeof node.label === 'string'
      && ['resourceId', 'groupId', 'description', 'address'].every((key) => optionalText(node[key]))
      && Array.isArray(node.metrics) && node.metrics.every((metric: unknown) => (
        record(metric) && text(metric.key) && typeof metric.label === 'string'
        && optionalText(metric.unit) && (metric.value === undefined || finite(metric.value))
      ))
      && (node.clientKey === undefined || clientKey(node.clientKey))
    ))
    && Array.isArray(value.links) && value.links.every((link: unknown) => (
      record(link) && text(link.id) && typeof link.label === 'string'
      && text(link.source) && text(link.target) && linkConfig(link.config)
    ))
    && Array.isArray(value.servers) && value.servers.every((server: unknown) => (
      record(server) && text(server.id) && typeof server.name === 'string'
      && typeof server.address === 'string' && finite(server.port)
    ))
    && (value.ingress === undefined || (record(value.ingress) && linkConfig(value.ingress.config)
      && Array.isArray(value.ingress.queues) && value.ingress.queues.every((queue: unknown) => (
        record(queue) && text(queue.id) && typeof queue.label === 'string' && finite(queue.capacity)
        && (queue.side === 'clients' || queue.side === 'backends')
      ))));
  if (!valid) throw new Error('The final topology definition is incomplete.');
  return value as unknown as TopologyDefinition;
}

export interface FinalSample {
  snapshot: SimulationSnapshot;
  definition: TopologyDefinition;
  data: LoadBalancerState;
}

export interface ParsedSimulationEnded {
  event: SimulationEnded;
  sample: FinalSample | null;
}

// Validate and hydrate before changing command state or notifying lifecycle consumers.
export function parseFinalSample(runId: string, snapshot: unknown, definition: unknown): FinalSample | null {
  if (snapshot === null && definition === null) return null;
  const frame = parseSnapshot(snapshot);
  const topology = parseTopologyDefinition(definition);
  if (frame.simulation.status === 'idle' || frame.simulation.runId !== runId || topology.runId !== runId
    || frame.topology.revision !== topology.revision) {
    throw new Error('The final sample does not match its run and topology revision.');
  }
  if (!validFinalResources(frame)) throw new Error('The final sample contains malformed resource measurements.');
  if (!validFinalMetadata(frame.simulation)) throw new Error('The final sample contains malformed simulation metadata.');
  return { snapshot: frame, definition: topology, data: hydrateSnapshot(frame, topology) };
}

export function parseSimulationEnded(value: unknown): ParsedSimulationEnded {
  if (!record(value) || value.type !== 'simulationEnded' || !text(value.runId)
    || typeof value.reason !== 'string' || !['finished', 'failed', 'stopped'].includes(value.reason)
    || (value.commandId !== undefined && !text(value.commandId))) {
    throw new Error('The server sent an invalid simulation end event.');
  }
  const sample = parseFinalSample(value.runId, value.finalSnapshot, value.topologyDefinition);
  const expectedStatus = value.reason === 'stopped' ? 'running' : value.reason;
  if ((!sample && value.reason !== 'stopped') || (sample && sample.snapshot.simulation.status !== expectedStatus)) {
    throw new Error('The final sample does not match the simulation end reason.');
  }
  return { event: value as unknown as SimulationEnded, sample };
}
