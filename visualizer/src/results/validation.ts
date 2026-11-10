import { freezeTree } from './immutable.ts';
import { COLLECTION_VERSION, RUN_RESULT_VERSION, type RunResult } from './types.ts';

export const RESULT_RECORD_BYTE_LIMIT = 64 * 1024 * 1024;
export type ResultValidationCode = 'invalid' | 'unsupported-version' | 'too-large';

export class ResultValidationError extends Error {
  readonly code: ResultValidationCode;

  constructor(code: ResultValidationCode, message: string) {
    super(message);
    this.name = 'ResultValidationError';
    this.code = code;
  }
}

type ObjectValue = Record<string, unknown>;
type Check = (value: unknown, path: string) => void;

function invalid(path: string): never {
  throw new ResultValidationError('invalid', `The archived result has an invalid ${path}.`);
}

function object(value: unknown, path: string): ObjectValue {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) invalid(path);
  return value as ObjectValue;
}

function list(value: unknown, path: string, check: Check): void {
  if (!Array.isArray(value)) invalid(path);
  value.forEach((child, index) => check(child, `${path}[${index}]`));
}

function fields(value: ObjectValue, names: string[], check: Check, path: string): void {
  for (const name of names) check(value[name], `${path}.${name}`);
}

const text: Check = (value, path) => { if (typeof value !== 'string') invalid(path); };
const id: Check = (value, path) => { if (typeof value !== 'string' || !value.trim()) invalid(path); };
const boolean: Check = (value, path) => { if (typeof value !== 'boolean') invalid(path); };
const timestamp: Check = (value, path) => {
  if (typeof value !== 'number' || !Number.isFinite(value)) invalid(path);
};
const nonnegativeNumber: Check = (value, path) => {
  timestamp(value, path);
  if ((value as number) < 0) invalid(path);
};
const count: Check = (value, path) => {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) invalid(path);
};

// ExactTelemetry can contain decimal strings for ANY unsafe integer token,
// including a float setting such as speed. Validate without replacing that text.
const scalar: Check = (value, path) => {
  if (typeof value === 'number' && Number.isFinite(value)) return;
  if (typeof value === 'string' && /^-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?$/.test(value)
    && Number.isFinite(Number(value))) return;
  invalid(path);
};
const nonnegative: Check = (value, path) => {
  scalar(value, path);
  if (Number(value) < 0) invalid(path);
};
const integer: Check = (value, path) => {
  if (typeof value === 'number' && Number.isSafeInteger(value)) return;
  if (typeof value === 'string' && /^-?(0|[1-9]\d*)$/.test(value)) return;
  invalid(path);
};
const counter: Check = (value, path) => {
  integer(value, path);
  if (typeof value === 'number' ? value < 0 : (value as string).startsWith('-')) invalid(path);
};
const publication: Check = (value, path) => {
  if (typeof value !== 'string' || !/^[1-9]\d{0,19}$/.test(value)
    || BigInt(value) > 18446744073709551615n) invalid(path);
};
const revision: Check = (value, path) => {
  if (typeof value !== 'string' || !/^(0|[1-9]\d*)$/.test(value)) invalid(path);
};

function optional(value: unknown, path: string, check: Check): void {
  if (value !== undefined) check(value, path);
}

function nullable(value: unknown, path: string, check: Check): void {
  if (value !== null) check(value, path);
}

function oneOf(value: unknown, path: string, choices: readonly unknown[]): void {
  if (!choices.includes(value)) invalid(path);
}

function optionalIntegers(entry: ObjectValue, names: string[], path: string): void {
  for (const key of names) {
    if (entry[key] !== undefined && entry[key] !== null) integer(entry[key], `${path}.${key}`);
  }
}

// These metrics carry exact integer quantities in the presentation contract.
// Rates, ratios and converted durations (including integer-valued doubles) use
// scalar validation, so large measured floats retain their original JSON type.
const INTEGER_METRICS = new Set([
  'ingressBitsPerSecond', 'ingressDeliveredPackets', 'ingressDeliveredBytes', 'backendCount',
  'routedConnections', 'forwardingBitsPerSecond', 'forwardedPackets', 'forwardedBytes',
  'forwardingQueuedPackets', 'forwardingQueueDroppedPackets', 'forwardingQueueDroppedBytes',
  'scheduledClients', 'createdClients', 'activeClients', 'completedStreams', 'receivedPayloadBytes',
  'acknowledgedPayloadBytes', 'successfulTransfers', 'failedTransfers', 'inProgressTransfers',
  'notStartedTransfers', 'synRetransmissions', 'dataRetransmissions', 'activeConnections',
  'halfOpenConnections', 'receivedPackets', 'sentACKCount', 'timerTriggeredACKCount', 'payloadBytes',
  'synRetries', 'dataRetries', 'finRetransmissions', 'closeElapsedNanoseconds', 'bytesInFlight',
  'duplicateACKs', 'fastRetransmissions', 'congestionWindowBytes', 'slowStartThresholdBytes',
  'ecnReceivedCEPacketCount', 'ecnSentECECount', 'ecnReceivedECECount', 'ecnSentCWRCount',
  'ecnResponseCount', 'hostCores', 'hostQueueCapacity', 'hostBusyCores', 'hostQueuedPackets',
  'hostMaxQueuedPackets', 'hostQueueDroppedPackets', 'hostProcessedPackets',
]);
const INTEGER_METRIC_UNITS = new Set(['bytes', 'packets', 'streams', 'backends', 'connections', 'clients', 'transfers', 'retries', 'ACKs', 'responses', 'cores', 'ns']);

function metricCheck(key: unknown, unit?: unknown): Check {
  return typeof key === 'string' && INTEGER_METRICS.has(key)
    || typeof unit === 'string' && INTEGER_METRIC_UNITS.has(unit) ? integer : scalar;
}

// Archives accept JSON data only. Unknown JSON fields survive unchanged, while
// cycles, non-finite values and implicit toJSON transformations cannot lose data.
function jsonTree(value: unknown, path = 'JSON', ancestors = new Set<object>(), depth = 0): void {
  if (value === null || typeof value === 'string' || typeof value === 'boolean') return;
  if (typeof value === 'number' && Number.isFinite(value)) return;
  if (typeof value !== 'object' || depth > 128 || ancestors.has(value)) invalid(path);
  if (!Array.isArray(value) && Object.getPrototypeOf(value) !== Object.prototype
    && Object.getPrototypeOf(value) !== null) invalid(path);
  if (Array.isArray(value)) {
    if (Object.keys(value).length !== value.length) invalid(path);
    for (let index = 0; index < value.length; index++) {
      if (!Object.prototype.hasOwnProperty.call(value, index)) invalid(path);
    }
  }
  ancestors.add(value);
  for (const child of Object.values(value)) jsonTree(child, path, ancestors, depth + 1);
  ancestors.delete(value);
}

function experiment(value: unknown, path: string): void {
  const entry = object(value, path);
  id(entry.model, `${path}.model`);
  if (typeof entry.scenarioSha256 !== 'string' || !/^[0-9a-f]{64}$/.test(entry.scenarioSha256)) invalid(`${path}.scenarioSha256`);
  const execution = object(entry.execution, `${path}.execution`);
  fields(execution, ['headless', 'traceEnabled'], boolean, `${path}.execution`);
  nonnegative(execution.speed, `${path}.execution.speed`);
  const build = object(entry.build, `${path}.build`);
  fields(build, ['goVersion', 'goos', 'goarch'], id, `${path}.build`);
  text(build.moduleVersion, `${path}.build.moduleVersion`);
  optional(build.vcsRevision, `${path}.build.vcsRevision`, text);
  optional(build.vcsModified, `${path}.build.vcsModified`, boolean);
}

function linkConfig(value: unknown, path: string, native = false, patch = false): void {
  const config = object(value, path);
  for (const key of ['bandwidthBitsPerSecond', native ? 'packetTravelTimeNanoseconds' : 'packetTravelTimeMs', 'queueCapacity', 'lossRate']) {
    if (!patch || config[key] !== undefined && config[key] !== null) {
      const check = key === 'bandwidthBitsPerSecond' || key === 'queueCapacity' || key === 'packetTravelTimeNanoseconds' ? counter : nonnegative;
      check(config[key], `${path}.${key}`);
    }
  }
  if (config.queuePolicy !== undefined && config.queuePolicy !== null) oneOf(config.queuePolicy, `${path}.queuePolicy`, ['', 'tailDrop', 'threshold']);
  for (const key of ['congestionThresholdPercent', 'congestionSignalProbability']) {
    if (config[key] !== undefined && config[key] !== null) nonnegative(config[key], `${path}.${key}`);
  }
}

function stream(value: unknown, path: string, patch = false): void {
  const entry = object(value, path);
  for (const key of ['packets', 'bytes', 'packetGapNanoseconds']) {
    if (!patch || entry[key] !== undefined && entry[key] !== null) integer(entry[key], `${path}.${key}`);
  }
}

function host(value: unknown, path: string): void {
  const entry = object(value, path);
  for (const key of ['cores', 'packetProcessingTimeNanoseconds', 'queueCapacity']) {
    if (entry[key] !== undefined && entry[key] !== null) counter(entry[key], `${path}.${key}`);
  }
}

function endpoint(value: unknown, path: string): void {
  const entry = object(value, path);
  optional(entry.profile, `${path}.profile`, text);
  linkConfig(entry.toLoadBalancer, `${path}.toLoadBalancer`, true, true);
  linkConfig(entry.fromLoadBalancer, `${path}.fromLoadBalancer`, true, true);
}

function scenario(value: unknown, path: string): void {
  const entry = object(value, path);
  if (entry.scenarioVersion !== 2 && entry.scenarioVersion !== '2') invalid(`${path}.scenarioVersion`);
  const fixed = object(entry.fixed, `${path}.fixed`);
  const execution = object(fixed.execution, `${path}.fixed.execution`);
  fields(execution, ['durationNanoseconds', 'snapshotIntervalNanoseconds', 'maxEvents'], counter, `${path}.fixed.execution`);
  integer(execution.seed, `${path}.fixed.execution.seed`);
  nonnegative(execution.speed, `${path}.fixed.execution.speed`);
  id(object(fixed.loadBalancer, `${path}.fixed.loadBalancer`).id, `${path}.fixed.loadBalancer.id`);
  // Admission already validated scenario policy. The archive needs its containers,
  // not a second TCP model validator (which would reject exact float strings).
  const tcp = object(fixed.tcp, `${path}.fixed.tcp`);
  optionalIntegers(tcp, ['mssBytes', 'receiveBufferBytes', 'clientIdleTimeoutNanoseconds', 'backendHalfOpenIdleTimeoutNanoseconds', 'backendEstablishedIdleTimeoutNanoseconds'], `${path}.fixed.tcp`);
  const tcpIntegers = {
    delayedAck: ['segmentsPerACK', 'maxDelayNanoseconds'],
    rto: ['initialTimeoutNanoseconds', 'minimumTimeoutNanoseconds', 'maximumTimeoutNanoseconds'],
    close: ['mslNanoseconds', 'finWaitTimeoutNanoseconds', 'maxFINRetries'],
    congestionControl: ['initialWindowSegments', 'maximumWindowSegments', 'initialSlowStartThresholdBytes'],
  };
  for (const [key, names] of Object.entries(tcpIntegers)) optional(tcp[key], `${path}.fixed.tcp.${key}`, (value, path) => { optionalIntegers(object(value, path), names, path); });
  const network = object(fixed.network, `${path}.fixed.network`);
  linkConfig(network.defaults, `${path}.fixed.network.defaults`, true);
  if (network.profiles !== undefined) list(network.profiles, `${path}.fixed.network.profiles`, (item, where) => {
    const profile = object(item, where);
    id(profile.id, `${where}.id`);
    linkConfig(profile.config, `${where}.config`, true, true);
  });
  stream(object(fixed.workload, `${path}.fixed.workload`).defaults, `${path}.fixed.workload.defaults`);
  const states = object(entry.states, `${path}.states`);
  for (const [at, value] of Object.entries(states)) {
    revision(at, `${path}.states time`);
    const state = object(value, `${path}.states.${at}`);
    const resources = object(state.resources, `${path}.states.${at}.resources`);
    const lb = object(resources.loadBalancer, `${path}.states.${at}.resources.loadBalancer`);
    text(lb.algorithm, `${path}.states.${at}.resources.loadBalancer.algorithm`);
    counter(lb.forwardingBitsPerSecond, `${path}.states.${at}.resources.loadBalancer.forwardingBitsPerSecond`);
    linkConfig(lb.ingress, `${path}.states.${at}.resources.loadBalancer.ingress`, true);
    optional(lb.host, `${path}.states.${at}.resources.loadBalancer.host`, host);
    const timeouts = object(lb.mappingTimeouts, `${path}.states.${at}.resources.loadBalancer.mappingTimeouts`);
    for (const key of ['halfOpenIdleTimeoutNanoseconds', 'establishedIdleTimeoutNanoseconds', 'closingIdleTimeoutNanoseconds', 'resetIdleTimeoutNanoseconds']) {
      if (timeouts[key] !== undefined && timeouts[key] !== null) counter(timeouts[key], `${path}.states.${at}.mappingTimeouts.${key}`);
    }
    list(resources.backends, `${path}.states.${at}.resources.backends`, (value, where) => {
      const backend = object(value, where);
      fields(backend, ['id', 'address'], id, where);
      counter(backend.port, `${where}.port`);
      optional(backend.name, `${where}.name`, text);
      optional(backend.host, `${where}.host`, host);
      if (backend.physicalAvailable !== undefined) nullable(backend.physicalAvailable, `${where}.physicalAvailable`, boolean);
      if (backend.administrativeState !== undefined && backend.administrativeState !== null) oneOf(backend.administrativeState, `${where}.administrativeState`, ['active', 'drain']);
      endpoint(backend.network, `${where}.network`);
    });
    list(object(state.workload, `${path}.states.${at}.workload`).clientGroups, `${path}.states.${at}.workload.clientGroups`, (value, where) => {
      const group = object(value, where);
      id(group.id, `${where}.id`);
      fields(group, ['count', 'startAtNanoseconds', 'clientGapNanoseconds'], counter, where);
      endpoint(group.network, `${where}.network`);
      stream(group.stream, `${where}.stream`, true);
      if (group.overrides !== undefined) list(group.overrides, `${where}.overrides`, (value, where) => {
        const override = object(value, where);
        counter(override.index, `${where}.index`);
        endpoint(override.network, `${where}.network`);
        stream(override.stream, `${where}.stream`, true);
      });
    });
  }
}

function stats(value: unknown, path: string): void {
  const entry = object(value, path);
  boolean(entry.transmitting, `${path}.transmitting`);
  fields(entry, ['queuedPackets', 'travellingPackets', 'offeredPackets', 'offeredBytes', 'acceptedPackets', 'acceptedBytes', 'queueDroppedPackets', 'queueDroppedBytes', 'lostPackets', 'lostBytes', 'deliveredPackets', 'deliveredBytes'], counter, path);
  for (const key of ['ecnMarkedPackets', 'ecnMarkedBytes', 'congestionDroppedPackets', 'congestionDroppedBytes']) optional(entry[key], `${path}.${key}`, counter);
}

function client(value: unknown, path: string): void {
  const entry = object(value, path);
  id(entry.id, `${path}.id`);
  fields(entry, ['retrySyn', 'retryData'], counter, path);
  optional(entry.acknowledgedPayloadBytes, `${path}.acknowledgedPayloadBytes`, counter);
  if (entry.transfer !== undefined) {
    const transfer = object(entry.transfer, `${path}.transfer`);
    oneOf(transfer.status, `${path}.transfer.status`, ['inProgress', 'succeeded', 'failed']);
    optional(transfer.failureReason, `${path}.transfer.failureReason`, text);
    fields(transfer, ['elapsedSeconds', 'handshakeDurationSeconds', 'payloadDurationSeconds'], nonnegative, `${path}.transfer`);
    fields(transfer, ['synRetransmissions', 'dataRetransmissions'], counter, `${path}.transfer`);
    boolean(transfer.handshakeCompleted, `${path}.transfer.handshakeCompleted`);
  }
  if (entry.connection !== undefined) {
    const connection = object(entry.connection, `${path}.connection`);
    oneOf(connection.state, `${path}.connection.state`, ['closed', 'synSent', 'synReceived', 'established', 'finWait1', 'finWait2', 'closeWait', 'closing', 'lastAck', 'timeWait']);
    optional(connection.outcome, `${path}.connection.outcome`, text);
    fields(connection, ['finRetransmissions', 'elapsedNanoseconds'], counter, `${path}.connection`);
  }
}

function server(value: unknown, path: string): void {
  const entry = object(value, path);
  id(entry.id, `${path}.id`);
  boolean(entry.active, `${path}.active`);
  fields(entry, ['activeConnections', 'halfOpenConnections', 'numberOfHandledPackets'], counter, path);
  nonnegative(entry.latencyP95Milli, `${path}.latencyP95Milli`);
  optional(entry.receivedPayloadBytes, `${path}.receivedPayloadBytes`, counter);
  optional(entry.cpuUsage, `${path}.cpuUsage`, nonnegative);
  list(entry.clients, `${path}.clients`, client);
}

function node(value: unknown, path: string): void {
  const entry = object(value, path);
  id(entry.id, `${path}.id`);
  optional(entry.active, `${path}.active`, boolean);
  for (const key of ['tcpPhase', 'tcpState', 'closeOutcome']) optional(entry[key], `${path}.${key}`, text);
  const metrics = object(entry.metrics, `${path}.metrics`);
  for (const [key, value] of Object.entries(metrics)) metricCheck(key)(value, `${path}.metrics.${key}`);
  if (entry.traffic !== undefined) fields(object(entry.traffic, `${path}.traffic`), ['transmitBytesPerSecond', 'receiveBytesPerSecond', 'sampleWindowSeconds'], nonnegative, `${path}.traffic`);
}

function measuredLink(value: unknown, path: string): void {
  const entry = object(value, path);
  id(entry.id, `${path}.id`);
  stats(entry.stats, `${path}.stats`);
}

function payload(value: unknown, path: string): void {
  fields(object(value, path), ['receivedBytes', 'acknowledgedBytes'], counter, path);
}

function performance(value: unknown, path: string): void {
  const entry = object(value, path);
  fields(entry, ['eventsExecuted', 'forwardedPackets'], counter, path);
  fields(entry, ['wallTimeSeconds', 'eventsPerWallSecond', 'forwardedPacketsPerWallSecond'], nonnegative, path);
}

function transfers(value: unknown, path: string): void {
  const entry = object(value, path);
  fields(entry, ['started', 'notStarted', 'succeeded', 'failed', 'inProgress', 'synRetransmissions', 'dataRetransmissions'], counter, path);
  for (const [key, value] of Object.entries(object(entry.failures, `${path}.failures`))) counter(value, `${path}.failures.${key}`);
  for (const key of ['handshake', 'payload', 'completion']) {
    const duration = object(entry[key], `${path}.${key}`);
    counter(duration.samples, `${path}.${key}.samples`);
    fields(duration, ['meanSeconds', 'minSeconds', 'maxSeconds'], nonnegative, `${path}.${key}`);
  }
}

function snapshot(value: unknown, path: string, runId: unknown): ObjectValue {
  const entry = object(value, path);
  if (entry.type !== 'simulationSnapshot' || entry.snapshotVersion !== 1) invalid(path);
  fields(entry, ['mode', 'algorithm'], text, path);
  const simulation = object(entry.simulation, `${path}.simulation`);
  if (simulation.runId !== runId) invalid(`${path}.simulation.runId`);
  oneOf(simulation.status, `${path}.simulation.status`, ['running', 'finished', 'failed']);
  oneOf(simulation.algorithm, `${path}.simulation.algorithm`, ['roundRobin', 'random']);
  publication(simulation.publicationSequence, `${path}.simulation.publicationSequence`);
  nonnegative(simulation.virtualTimeSeconds, `${path}.simulation.virtualTimeSeconds`);
  experiment(simulation.experiment, `${path}.simulation.experiment`);
  optional(simulation.error, `${path}.simulation.error`, text);
  optional(simulation.payload, `${path}.simulation.payload`, payload);
  optional(simulation.performance, `${path}.simulation.performance`, performance);
  optional(simulation.transfers, `${path}.simulation.transfers`, transfers);
  list(entry.servers, `${path}.servers`, server);
  const topology = object(entry.topology, `${path}.topology`);
  revision(topology.revision, `${path}.topology.revision`);
  list(topology.nodes, `${path}.topology.nodes`, node);
  list(topology.links, `${path}.topology.links`, measuredLink);
  if (topology.ingress !== undefined) {
    const ingress = object(topology.ingress, `${path}.topology.ingress`);
    stats(ingress.stats, `${path}.topology.ingress.stats`);
    list(ingress.queues, `${path}.topology.ingress.queues`, measuredLink);
  }
  return simulation;
}

function definition(value: unknown, path: string, runId: unknown): void {
  const entry = object(value, path);
  if (entry.runId !== runId) invalid(`${path}.runId`);
  revision(entry.revision, `${path}.revision`);
  list(entry.nodes, `${path}.nodes`, (value, where) => {
    const node = object(value, where);
    fields(node, ['id', 'kind', 'label'], text, where);
    for (const key of ['resourceId', 'groupId', 'description', 'address']) optional(node[key], `${where}.${key}`, text);
    list(node.metrics, `${where}.metrics`, (value, where) => {
      const metric = object(value, where);
      fields(metric, ['key', 'label'], text, where);
      optional(metric.unit, `${where}.unit`, text);
      optional(metric.value, `${where}.value`, metricCheck(metric.key, metric.unit));
    });
    if (node.clientKey !== undefined) {
      const key = object(node.clientKey, `${where}.clientKey`);
      fields(key, ['SrcIP', 'DstIP', 'Protocol'], text, `${where}.clientKey`);
      fields(key, ['SrcPort', 'DstPort'], counter, `${where}.clientKey`);
    }
  });
  list(entry.links, `${path}.links`, (value, where) => {
    const link = object(value, where);
    fields(link, ['id', 'label', 'source', 'target'], text, where);
    linkConfig(link.config, `${where}.config`);
  });
  list(entry.servers, `${path}.servers`, (value, where) => {
    const server = object(value, where);
    fields(server, ['id', 'name', 'address'], text, where);
    counter(server.port, `${where}.port`);
  });
  if (entry.ingress !== undefined) {
    const ingress = object(entry.ingress, `${path}.ingress`);
    linkConfig(ingress.config, `${path}.ingress.config`);
    list(ingress.queues, `${path}.ingress.queues`, (value, where) => {
      const queue = object(value, where);
      fields(queue, ['id', 'label'], text, where);
      oneOf(queue.side, `${where}.side`, ['clients', 'backends']);
      counter(queue.capacity, `${where}.capacity`);
    });
  }
}

function limits(value: unknown, path: string): void {
  const entry = object(value, path);
  fields(entry, ['memoryLimited', 'markerLimitReached', 'terminalSnapshotDropped'], boolean, path);
  fields(entry, ['droppedSamples', 'droppedDefinitions', 'droppedMetadata', 'droppedMarkers'], count, path);
}

function range(value: unknown, path: string): void {
  const entry = object(value, path);
  fields(entry, ['first', 'last'], publication, path);
  if (BigInt(entry.first as string) > BigInt(entry.last as string)) invalid(path);
}

export function validateRunResult(value: unknown, expectedId?: string): asserts value is RunResult {
  jsonTree(value);
  const result = object(value, 'result');
  if (result.resultVersion !== RUN_RESULT_VERSION) throw new ResultValidationError('unsupported-version', 'This result version is not supported.');
  fields(result, ['id', 'runId'], id, 'result');
  if (expectedId !== undefined && result.id !== expectedId) invalid('record identity');
  const completion = object(result.completion, 'completion');
  oneOf(completion.reason, 'completion.reason', ['finished', 'stopped']);
  timestamp(completion.receivedAt, 'completion.receivedAt');
  const context = object(result.context, 'context');
  nullable(context.scenario, 'context.scenario', scenario);
  oneOf(context.scenarioProvenance, 'context.scenarioProvenance', ['backend', 'submitted', 'missing']);
  nullable(context.scenarioReceivedAt, 'context.scenarioReceivedAt', timestamp);
  nullable(context.experiment, 'context.experiment', experiment);
  oneOf(context.experimentProvenance, 'context.experimentProvenance', ['backend', 'snapshot', 'missing']);
  nullable(context.experimentReceivedAt, 'context.experimentReceivedAt', timestamp);
  nullable(context.seed, 'context.seed', integer);
  const interval = object(context.publicationInterval, 'context.publicationInterval');
  if (interval.clock !== 'real') invalid('context.publicationInterval.clock');
  nullable(interval.configuredNanoseconds, 'context.publicationInterval.configuredNanoseconds', counter);
  const collection = object(result.collection, 'collection');
  if (collection.collectionVersion !== COLLECTION_VERSION) throw new ResultValidationError('unsupported-version', 'This collection version is not supported.');
  if (collection.runId !== result.runId) invalid('collection.runId');
  timestamp(collection.createdAt, 'collection.createdAt');
  limits(collection.limits, 'collection.limits');
  for (const key of ['firstObservedSequence', 'highestObservedSequence', 'terminalPublicationSequence']) nullable(collection[key], `collection.${key}`, publication);
  nullable(collection.firstObservedAt, 'collection.firstObservedAt', timestamp);
  const samples = new Map<unknown, ObjectValue>();
  const sequences = new Set<unknown>();
  list(collection.samples, 'collection.samples', (value, path) => {
    const sample = object(value, path);
    count(sample.index, `${path}.index`);
    if (sample.index === 0 || samples.has(sample.index)) invalid(`${path}.index`);
    publication(sample.publicationSequence, `${path}.publicationSequence`);
    if (sequences.has(sample.publicationSequence)) invalid(`${path}.publicationSequence`);
    sequences.add(sample.publicationSequence);
    nonnegativeNumber(sample.virtualTimeSeconds, `${path}.virtualTimeSeconds`);
    timestamp(sample.receivedAt, `${path}.receivedAt`);
    const simulation = snapshot(sample.snapshot, `${path}.snapshot`, result.runId);
    if (simulation.publicationSequence !== sample.publicationSequence) invalid(`${path}.snapshot publication identity`);
    if (Number(simulation.virtualTimeSeconds) !== sample.virtualTimeSeconds) invalid(`${path}.snapshot virtual time`);
    samples.set(sample.index, sample);
  });
  list(collection.definitions, 'collection.definitions', (value, path) => definition(value, path, result.runId));
  list(collection.markers, 'collection.markers', (value, path) => {
    const marker = object(value, path);
    oneOf(marker.kind, `${path}.kind`, ['lateJoin', 'gap', 'disconnected', 'reconnected']);
    timestamp(marker.receivedAt, `${path}.receivedAt`);
    if (marker.kind === 'lateJoin') publication(marker.publicationSequence, `${path}.publicationSequence`);
    if (marker.kind === 'gap') fields(marker, ['afterSequence', 'beforeSequence'], publication, path);
  });
  nullable(collection.metadata, 'collection.metadata', (value, path) => {
    const metadata = object(value, path);
    oneOf(metadata.provenance, `${path}.provenance`, ['backend', 'submitted']);
    timestamp(metadata.receivedAt, `${path}.receivedAt`);
    scenario(metadata.scenario, `${path}.scenario`);
    nullable(metadata.experiment, `${path}.experiment`, experiment);
  });
  function reference(value: unknown, path: string): void {
    const reference = object(value, path);
    count(reference.index, `${path}.index`);
    publication(reference.publicationSequence, `${path}.publicationSequence`);
    nonnegativeNumber(reference.virtualTimeSeconds, `${path}.virtualTimeSeconds`);
    timestamp(reference.receivedAt, `${path}.receivedAt`);
    revision(reference.topologyRevision, `${path}.topologyRevision`);
    const sample = samples.get(reference.index);
    if (!sample || sample.publicationSequence !== reference.publicationSequence
      || sample.virtualTimeSeconds !== reference.virtualTimeSeconds || sample.receivedAt !== reference.receivedAt
      || object(object(sample.snapshot, path).topology, path).revision !== reference.topologyRevision) invalid(`${path} publication identity`);
  }
  function cpu(value: unknown, path: string): void {
    const entry = object(value, path);
    if (entry.basis !== 'hostLifetimeMean') invalid(`${path}.basis`);
    for (const key of ['utilizationRatio', 'busyTimeSeconds', 'availableCoreTimeSeconds']) nullable(entry[key], `${path}.${key}`, nonnegative);
  }
  function observation(value: unknown, path: string, check: Check): ObjectValue {
    const entry = object(value, path);
    id(entry.id, `${path}.id`);
    reference(entry.observation, `${path}.observation`);
    boolean(entry.presentInSummarySample, `${path}.presentInSummarySample`);
    check(entry.value, `${path}.value`);
    const measured = object(entry.value, `${path}.value`);
    if (measured.id !== undefined && measured.id !== entry.id) invalid(`${path}.resource identity`);
    return entry;
  }
  const summary = object(result.summary, 'summary');
  oneOf(summary.basis, 'summary.basis', ['terminal', 'lastObservation', 'unavailable']);
  nullable(summary.observation, 'summary.observation', reference);
  nullable(summary.payload, 'summary.payload', payload);
  nullable(summary.transfers, 'summary.transfers', transfers);
  nullable(summary.performance, 'summary.performance', performance);
  if (summary.transferLatencyP95Seconds !== null || summary.transferLatencyP99Seconds !== null) invalid('summary unavailable transfer percentiles');
  nullable(summary.payloadRate, 'summary.payloadRate', (value, path) => {
    const rate = object(value, path);
    if (rate.basis !== 'runVirtualTime') invalid(`${path}.basis`);
    nonnegative(rate.virtualTimeSeconds, `${path}.virtualTimeSeconds`);
    for (const key of ['approximateReceivedBytesPerSecond', 'approximateAcknowledgedBytesPerSecond']) nullable(rate[key], `${path}.${key}`, nonnegativeNumber);
  });
  const resources = object(summary.resources, 'summary.resources');
  list(resources.nodes, 'summary.resources.nodes', (value, path) => {
    const entry = observation(value, path, node);
    nullable(entry.kind, `${path}.kind`, text);
    nullable(entry.groupId, `${path}.groupId`, text);
    cpu(entry.cpu, `${path}.cpu`);
  });
  list(resources.servers, 'summary.resources.servers', (value, path) => cpu(observation(value, path, server).cpu, `${path}.cpu`));
  list(resources.clients, 'summary.resources.clients', (value, path) => {
    const entry = observation(value, path, client);
    id(object(entry.value, `${path}.value`).serverId, `${path}.value.serverId`);
  });
  for (const key of ['links', 'queues']) list(resources[key], `summary.resources.${key}`, (value, path) => { observation(value, path, measuredLink); });
  nullable(resources.ingress, 'summary.resources.ingress', (value, path) => { observation(value, path, stats); });
  const coverage = object(result.coverage, 'coverage');
  fields(coverage, ['complete', 'joinedAfterStart', 'missingScenario', 'missingAuthoritativeScenario', 'missingExperiment', 'metadataAfterFirstSample', 'terminalSampleMissing', 'memoryLimited'], boolean, 'coverage');
  nullable(coverage.publicationsBeforeFirstReceipt, 'coverage.publicationsBeforeFirstReceipt', range);
  list(coverage.unrecordedPublications, 'coverage.unrecordedPublications', range);
  if (typeof coverage.unrecordedPublicationCount !== 'string' || !/^(0|[1-9]\d*)$/.test(coverage.unrecordedPublicationCount)) invalid('coverage.unrecordedPublicationCount');
  count(coverage.disconnections, 'coverage.disconnections');
  list(coverage.missingDefinitionRevisions, 'coverage.missingDefinitionRevisions', revision);
  limits(coverage.limits, 'coverage.limits');
}

export function requireJSONSize(json: string, byteLimit = RESULT_RECORD_BYTE_LIMIT): void {
  if (json.length > byteLimit || new TextEncoder().encode(json).byteLength > byteLimit) {
    throw new ResultValidationError('too-large', 'The archived result exceeds the local record size limit.');
  }
}

export function parseRunResultJSON(json: string, expectedId?: string, byteLimit = RESULT_RECORD_BYTE_LIMIT): RunResult {
  requireJSONSize(json, byteLimit);
  let value: unknown;
  try { value = JSON.parse(json); } catch { throw new ResultValidationError('invalid', 'The archived result is not valid JSON.'); }
  validateRunResult(value, expectedId);
  return freezeTree(value);
}
