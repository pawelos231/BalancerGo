import type { ExactTelemetry } from '../telemetry/exact.ts';
import type { TopologyDefinition } from '../telemetry/types.ts';
import type { RunResult } from './types.ts';

export const XLSX_TABLES = [
  'Run', 'Summary', 'Scenario', 'Samples', 'Nodes', 'Backends', 'LinksQueues', 'Clients', 'Definitions',
] as const;

export type XlsxTable = typeof XLSX_TABLES[number];
type SourceObject = Record<string, unknown>;
type Definition = ExactTelemetry<TopologyDefinition>;

interface DefinitionIndex {
  nodes: Map<string, Definition['nodes'][number]>;
  links: Map<string, Definition['links'][number]>;
  queues: Map<string, NonNullable<Definition['ingress']>['queues'][number]>;
}

export interface FieldContext {
  sampleIndex?: number;
  publicationSequence?: string;
  virtualTimeSeconds?: number;
  receivedAtUnixMs?: number;
  topologyRevision?: string;
  resourceKind?: string;
  resourceId?: string;
  backendId?: string;
  source?: string;
  target?: string;
  queueSide?: string;
}

export function sourceObject(value: unknown): SourceObject | undefined {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? value as SourceObject : undefined;
}

export function tableForPath(path: readonly string[]): XlsxTable {
  if (path[0] === 'summary') return 'Summary';
  if (path[0] === 'context' && path[1] === 'scenario') return 'Scenario';
  if (path[0] !== 'collection') return 'Run';
  if (path[1] === 'metadata' && path[2] === 'scenario') return 'Scenario';
  if (path[1] === 'definitions') return 'Definitions';
  if (path[1] !== 'samples') return 'Run';
  if (path[3] === 'snapshot' && path[4] === 'servers') {
    return path[6] === 'clients' ? 'Clients' : 'Backends';
  }
  if (path[3] === 'snapshot' && path[4] === 'topology') {
    if (path[5] === 'nodes') return 'Nodes';
    if (path[5] === 'links' || path[5] === 'ingress') return 'LinksQueues';
  }
  return 'Samples';
}

function reference(value: unknown): FieldContext {
  const entry = sourceObject(value);
  if (!entry) return {};
  return {
    sampleIndex: entry.index as number,
    publicationSequence: entry.publicationSequence as string,
    virtualTimeSeconds: entry.virtualTimeSeconds as number,
    receivedAtUnixMs: entry.receivedAt as number,
    topologyRevision: entry.topologyRevision as string,
  };
}

// Definition joins are revision-specific; missing revisions never borrow another revision's descriptions.
export class XlsxFieldMetadata {
  private readonly definitions = new Map<string, DefinitionIndex>();

  constructor(result: RunResult) {
    for (const definition of result.collection.definitions) {
      this.definitions.set(definition.revision, {
        nodes: new Map(definition.nodes.map(node => [node.id, node])),
        links: new Map(definition.links.map(link => [link.id, link])),
        queues: new Map(definition.ingress?.queues.map(queue => [queue.id, queue])),
      });
    }
  }

  context(path: readonly string[], value: unknown, inherited: FieldContext): FieldContext {
    const entry = sourceObject(value);
    if (!entry) return inherited;
    let context = inherited;

    if (path[0] === 'summary' && path.length === 1) context = reference(entry.observation);
    if (path[0] === 'summary' && path[1] === 'resources' && path.length === 4 && /^\d+$/.test(path[3])
      && ['nodes', 'servers', 'clients', 'links', 'queues'].includes(path[2])) {
      context = { ...reference(entry.observation), resourceKind: path[2], resourceId: entry.id as string };
      if (path[2] === 'servers') context.backendId = entry.id as string;
      if (path[2] === 'clients') context.backendId = sourceObject(entry.value)?.serverId as string;
    }
    if (path[0] === 'summary' && path[1] === 'resources' && path[2] === 'ingress' && path.length === 3) {
      context = { ...reference(entry.observation), resourceKind: 'ingress', resourceId: entry.id as string };
    }
    if (path[0] === 'summary' && path[1] === 'resources' && path[2] === 'servers'
      && path[4] === 'value' && path[5] === 'clients' && path.length === 7 && /^\d+$/.test(path[6])) {
      context = { ...context, resourceKind: 'client', resourceId: entry.id as string };
    }
    if (path[0] !== 'collection') return this.direction(context);

    if (path[1] === 'definitions') {
      if (path.length === 3) context = { topologyRevision: entry.revision as string };
      if (path.length === 5 && /^\d+$/.test(path[4]) && ['nodes', 'links', 'servers'].includes(path[3])) {
        context = { ...context, resourceKind: path[3], resourceId: entry.id as string };
      }
      if (path[3] === 'ingress' && path.length === 4) {
        context = { ...context, resourceKind: 'ingress', resourceId: 'ingress' };
      }
      if (path[3] === 'ingress' && path[4] === 'queues' && path.length === 6 && /^\d+$/.test(path[5])) {
        context = { ...context, resourceKind: 'queue', resourceId: entry.id as string };
      }
      return this.direction(context);
    }
    if (path[1] !== 'samples') return context;
    if (path.length === 3) {
      const snapshot = sourceObject(entry.snapshot);
      context = { ...reference(entry), topologyRevision: sourceObject(snapshot?.topology)?.revision as string };
    }
    if (path[3] !== 'snapshot') return context;

    if (path[4] === 'servers' && path.length === 6) {
      context = { ...context, resourceKind: 'backend', resourceId: entry.id as string, backendId: entry.id as string };
    }
    if (path[4] === 'servers' && path[6] === 'clients' && path.length === 8) {
      context = { ...context, resourceKind: 'client', resourceId: entry.id as string };
    }
    if (path[4] !== 'topology') return context;
    if (path[5] === 'nodes' && path.length === 7) {
      context = { ...context, resourceKind: 'node', resourceId: entry.id as string };
    }
    if (path[5] === 'links' && path.length === 7) {
      context = { ...context, resourceKind: 'link', resourceId: entry.id as string };
    }
    if (path[5] === 'ingress' && path.length === 6) {
      context = { ...context, resourceKind: 'ingress', resourceId: 'ingress' };
    }
    if (path[5] === 'ingress' && path[6] === 'queues' && path.length === 8) {
      context = { ...context, resourceKind: 'queue', resourceId: entry.id as string };
    }
    return this.direction(context);
  }

  private direction(context: FieldContext): FieldContext {
    const definition = this.definitions.get(context.topologyRevision ?? '');
    if (context.resourceKind === 'link' || context.resourceKind === 'links') {
      const link = definition?.links.get(context.resourceId ?? '');
      return { ...context, source: link?.source, target: link?.target };
    }
    if (context.resourceKind === 'queue' || context.resourceKind === 'queues') {
      return { ...context, queueSide: definition?.queues.get(context.resourceId ?? '')?.side };
    }
    return context;
  }

  unit(path: readonly string[], parent: SourceObject | undefined, context: FieldContext): string | undefined {
    const key = path[path.length - 1] ?? '';
    if (path[path.length - 2] === 'metrics' && knownNodeMetrics(path)) {
      const node = this.definitions.get(context.topologyRevision ?? '')?.nodes.get(context.resourceId ?? '');
      return node?.metrics.find(metric => metric.key === key)?.unit;
    }
    if (path[0] === 'collection' && path[1] === 'definitions' && path[3] === 'nodes'
      && path[5] === 'metrics' && path.length === 8 && key === 'value' && typeof parent?.unit === 'string') {
      return parent.unit;
    }
    if (path[0] === 'collection' && path[1] === 'definitions' && path[3] === 'ingress'
      && path[4] === 'queues' && path.length === 7 && key === 'capacity') return 'packets';
    return knownUnitContainer(path) ? FIELD_UNITS.get(key) : undefined;
  }
}

function knownNodeMetrics(path: readonly string[]): boolean {
  return path[0] === 'collection' && path[1] === 'samples' && path[3] === 'snapshot'
    && path[4] === 'topology' && path[5] === 'nodes' && path.length === 9
    || path[0] === 'summary' && path[1] === 'resources' && path[2] === 'nodes'
      && path[4] === 'value' && path.length === 7;
}

const UNIT_CONTAINERS = [
  /^(completion|context|context\/publicationInterval|collection|collection\/metadata)$/,
  /^collection\/markers\/\d+$/,
  /^collection\/samples\/\d+(\/snapshot\/simulation)?$/,
  /^collection\/samples\/\d+\/snapshot\/simulation\/(payload|performance|transfers)(\/(handshake|payload|completion))?$/,
  /^collection\/samples\/\d+\/snapshot\/servers\/\d+(\/clients\/\d+(\/(transfer|connection))?)?$/,
  /^collection\/samples\/\d+\/snapshot\/topology\/nodes\/\d+\/traffic$/,
  /^collection\/samples\/\d+\/snapshot\/topology\/links\/\d+\/stats$/,
  /^collection\/samples\/\d+\/snapshot\/topology\/ingress(\/queues\/\d+)?\/stats$/,
  /^summary\/(payload|performance|payloadRate|transfers)(\/(handshake|payload|completion))?$/,
  /^summary(\/observation)?$/,
  /^summary\/resources\/(nodes|servers|clients|links|queues)\/\d+\/observation$/,
  /^summary\/resources\/ingress\/observation$/,
  /^summary\/resources\/(nodes|servers)\/\d+\/cpu$/,
  /^summary\/resources\/nodes\/\d+\/value\/traffic$/,
  /^summary\/resources\/servers\/\d+\/value(\/clients\/\d+(\/(transfer|connection))?)?$/,
  /^summary\/resources\/clients\/\d+\/value(\/(transfer|connection))?$/,
  /^summary\/resources\/(links|queues)\/\d+\/value\/stats$/,
  /^summary\/resources\/ingress\/value$/,
  /^collection\/definitions\/\d+\/links\/\d+\/config$/,
  /^collection\/definitions\/\d+\/ingress(\/config|\/queues\/\d+)$/,
  /^(context\/scenario|collection\/metadata\/scenario)\/fixed\/(execution|tcp(\/(delayedAck|rto|close|congestionControl))?)$/,
  /^(context\/scenario|collection\/metadata\/scenario)\/fixed\/(network\/(defaults|profiles\/\d+\/config)|workload\/defaults)$/,
  /^(context\/scenario|collection\/metadata\/scenario)\/states\/[^/]+\/resources\/loadBalancer(\/(host|ingress|mappingTimeouts))?$/,
  /^(context\/scenario|collection\/metadata\/scenario)\/states\/[^/]+\/resources\/backends\/\d+(\/host)?$/,
] as const;

function knownUnitContainer(path: readonly string[]): boolean {
  const parts = path.slice(0, -1).map(part => part.replace(/~/g, '~0').replace(/\//g, '~1'));
  const container = parts.join('/');
  if (UNIT_CONTAINERS.some(pattern => pattern.test(container))) return true;
  // Endpoint network patches and client-group overrides use the same compact scenario settings.
  if (!/^(context\/scenario|collection\/metadata\/scenario)\/states\/[^/]+\//.test(container)) return false;
  const statePath = parts.slice(parts.indexOf('states') + 2).join('/');
  const endpoint = /^resources\/backends\/\d+\/network\/(toLoadBalancer|fromLoadBalancer)$/;
  const group = /^workload\/clientGroups\/\d+(\/overrides\/\d+)?$/;
  const groupSettings = /\/(stream|network\/(toLoadBalancer|fromLoadBalancer))$/;
  return endpoint.test(statePath) || group.test(statePath) || group.test(statePath.replace(groupSettings, ''));
}

const FIELD_UNITS = new Map<string, string>();
for (const [unit, names] of [
  ['B', 'bytes receivedBytes acknowledgedBytes receivedPayloadBytes acknowledgedPayloadBytes payloadBytes '
    + 'offeredBytes acceptedBytes queueDroppedBytes ecnMarkedBytes congestionDroppedBytes lostBytes deliveredBytes '
    + 'forwardedBytes forwardingQueueDroppedBytes bytesInFlight congestionWindowBytes slowStartThresholdBytes '
    + 'mssBytes receiveBufferBytes initialSlowStartThresholdBytes'],
  ['packets', 'queuedPackets travellingPackets offeredPackets acceptedPackets queueDroppedPackets ecnMarkedPackets '
    + 'congestionDroppedPackets lostPackets deliveredPackets numberOfHandledPackets forwardedPackets '
    + 'ingressDeliveredPackets forwardingQueuedPackets forwardingQueueDroppedPackets receivedPackets '
    + 'hostQueuedPackets hostMaxQueuedPackets hostQueueDroppedPackets hostProcessedPackets queueCapacity '
    + 'hostQueueCapacity'],
  ['1024 B payload units', 'packets'],
  ['MSS units', 'initialWindowSegments maximumWindowSegments'],
  ['segments / ACK', 'segmentsPerACK'],
  ['ns', 'durationNanoseconds snapshotIntervalNanoseconds packetTravelTimeNanoseconds packetProcessingTimeNanoseconds '
    + 'packetGapNanoseconds configuredNanoseconds startAtNanoseconds clientGapNanoseconds initialTimeoutNanoseconds '
    + 'minimumTimeoutNanoseconds maximumTimeoutNanoseconds mslNanoseconds finWaitTimeoutNanoseconds maxDelayNanoseconds '
    + 'clientIdleTimeoutNanoseconds backendHalfOpenIdleTimeoutNanoseconds backendEstablishedIdleTimeoutNanoseconds '
    + 'halfOpenIdleTimeoutNanoseconds establishedIdleTimeoutNanoseconds closingIdleTimeoutNanoseconds '
    + 'resetIdleTimeoutNanoseconds elapsedNanoseconds closeElapsedNanoseconds'],
  ['s', 'virtualTimeSeconds wallTimeSeconds elapsedSeconds handshakeDurationSeconds payloadDurationSeconds '
    + 'meanSeconds minSeconds maxSeconds sampleWindowSeconds transferLatencyP95Seconds transferLatencyP99Seconds'],
  ['core s', 'hostBusyTimeSeconds hostAvailableCoreTimeSeconds busyTimeSeconds availableCoreTimeSeconds'],
  ['ms', 'latencyP95Milli packetTravelTimeMs'],
  ['Unix ms', 'receivedAt createdAt firstObservedAt scenarioReceivedAt experimentReceivedAt'],
  ['ratio', 'cpuUsage hostUtilization utilizationRatio lossRate congestionSignalProbability'],
  ['%', 'congestionThresholdPercent'],
  ['bit/s', 'bandwidthBitsPerSecond forwardingBitsPerSecond ingressBitsPerSecond'],
  ['B / virtual s', 'transmitBytesPerSecond receiveBytesPerSecond approximateReceivedBytesPerSecond '
    + 'approximateAcknowledgedBytesPerSecond'],
  ['events / real s', 'eventsPerWallSecond'],
  ['packets / real s', 'forwardedPacketsPerWallSecond'],
  ['events', 'eventsExecuted maxEvents'],
  ['cores', 'cores hostCores hostBusyCores'],
  ['connections', 'activeConnections halfOpenConnections routedConnections'],
  ['clients', 'scheduledClients createdClients activeClients count'],
  ['retries', 'retrySyn retryData synRetries dataRetries synRetransmissions dataRetransmissions finRetransmissions maxFINRetries'],
] as const) {
  for (const name of names.split(' ')) FIELD_UNITS.set(name, unit);
}

export function fieldAvailability(value: unknown, path: readonly string[], parent: SourceObject | undefined) {
  const key = path[path.length - 1];
  const owner = path[path.length - 2];
  const durationSummary = path[0] === 'summary' && path[1] === 'transfers' && path.length === 4
    || path[0] === 'collection' && path[1] === 'samples' && path[3] === 'snapshot'
      && path[4] === 'simulation' && path[5] === 'transfers' && path.length === 8;
  const clientTransfer = path[0] === 'collection' && path[1] === 'samples' && path[3] === 'snapshot'
    && path[4] === 'servers' && path[6] === 'clients' && path.length === 10
    || path[0] === 'summary' && path[1] === 'resources' && path[2] === 'clients'
      && path[4] === 'value' && path.length === 7
    || path[0] === 'summary' && path[1] === 'resources' && path[2] === 'servers'
      && path[4] === 'value' && path[5] === 'clients' && path.length === 9;
  if (value === null) return { availability: 'unavailable', note: 'No value was recorded.' };
  if (value !== null && typeof value === 'object') return { availability: 'structure', note: undefined };
  if (durationSummary && ['meanSeconds', 'minSeconds', 'maxSeconds'].includes(key)
    && ['handshake', 'payload', 'completion'].includes(owner) && /^(0|-0)$/.test(String(parent?.samples))) {
    return { availability: 'unavailable', note: 'No duration samples; the original field is retained.' };
  }
  if (clientTransfer && owner === 'transfer' && key === 'handshakeDurationSeconds' && parent?.handshakeCompleted !== true) {
    return { availability: 'unavailable', note: 'Handshake incomplete; the original field is retained.' };
  }
  if (clientTransfer && owner === 'transfer' && key === 'payloadDurationSeconds' && parent?.status !== 'succeeded') {
    return { availability: 'unavailable', note: 'Payload transfer did not succeed; the original field is retained.' };
  }
  return { availability: 'recorded', note: undefined };
}
