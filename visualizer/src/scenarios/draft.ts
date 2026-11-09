import type { SimulationConfig } from '../types';
import { congestionModeOptions } from './types.ts';
import type { ExactInteger, Scenario } from './types';
import type { ScenarioStateV2 } from './v2Types.ts';

export type ScenarioField = keyof SimulationConfig;
export type ScenarioDraft = Record<ScenarioField, string>;

const maxInt64 = (1n << 63n) - 1n;
const maxUint64 = (1n << 64n) - 1n;
const maxSafeInteger = BigInt(Number.MAX_SAFE_INTEGER);

type DirectField = Exclude<ScenarioField, 'clients' | 'servers' | 'clientGapMs'>;

// These paths change shared defaults without flattening endpoint overrides.
const fieldPaths: Record<DirectField, readonly string[]> = {
  algorithm: ['resources', 'loadBalancer', 'algorithm'],
  durationSeconds: ['fixed', 'execution', 'durationNanoseconds'],
  snapshotIntervalMs: ['fixed', 'execution', 'snapshotIntervalNanoseconds'],
  seed: ['fixed', 'execution', 'seed'],
  speed: ['fixed', 'execution', 'speed'],
  maxEvents: ['fixed', 'execution', 'maxEvents'],
  packets: ['fixed', 'workload', 'defaults', 'packets'],
  bytes: ['fixed', 'workload', 'defaults', 'bytes'],
  mssBytes: ['fixed', 'tcp', 'mssBytes'],
  receiveBufferBytes: ['fixed', 'tcp', 'receiveBufferBytes'],
  sackEnabled: ['fixed', 'tcp', 'sackEnabled'],
  ECNenabled: ['fixed', 'tcp', 'ECNenabled'],
  delayedAckEnabled: ['fixed', 'tcp', 'delayedAck', 'enabled'],
  delayedAckSegments: ['fixed', 'tcp', 'delayedAck', 'segmentsPerACK'],
  delayedAckMaxDelayMs: ['fixed', 'tcp', 'delayedAck', 'maxDelayNanoseconds'],
  congestionControl: ['fixed', 'tcp', 'congestionControl', 'mode'],
  fastRetransmit: ['fixed', 'tcp', 'congestionControl', 'fastRetransmit'],
  cubicFastConvergence: ['fixed', 'tcp', 'congestionControl', 'cubicFastConvergence'],
  initialCongestionWindowSegments: ['fixed', 'tcp', 'congestionControl', 'initialWindowSegments'],
  maximumCongestionWindowSegments: ['fixed', 'tcp', 'congestionControl', 'maximumWindowSegments'],
  initialSlowStartThresholdBytes: ['fixed', 'tcp', 'congestionControl', 'initialSlowStartThresholdBytes'],
  rtoMode: ['fixed', 'tcp', 'rto', 'mode'],
  rtoBackoff: ['fixed', 'tcp', 'rto', 'exponentialBackoff'],
  rtoInitialMs: ['fixed', 'tcp', 'rto', 'initialTimeoutNanoseconds'],
  rtoMinimumMs: ['fixed', 'tcp', 'rto', 'minimumTimeoutNanoseconds'],
  rtoMaximumMs: ['fixed', 'tcp', 'rto', 'maximumTimeoutNanoseconds'],
  tcpMSLMs: ['fixed', 'tcp', 'close', 'mslNanoseconds'],
  tcpFinWaitTimeoutMs: ['fixed', 'tcp', 'close', 'finWaitTimeoutNanoseconds'],
  tcpMaxFINRetries: ['fixed', 'tcp', 'close', 'maxFINRetries'],
  clientIdleTimeoutMs: ['fixed', 'tcp', 'clientIdleTimeoutNanoseconds'],
  backendHalfOpenIdleTimeoutMs: ['fixed', 'tcp', 'backendHalfOpenIdleTimeoutNanoseconds'],
  backendEstablishedIdleTimeoutMs: ['fixed', 'tcp', 'backendEstablishedIdleTimeoutNanoseconds'],
  lbHalfOpenIdleTimeoutMs: ['resources', 'loadBalancer', 'mappingTimeouts', 'halfOpenIdleTimeoutNanoseconds'],
  lbEstablishedIdleTimeoutMs: ['resources', 'loadBalancer', 'mappingTimeouts', 'establishedIdleTimeoutNanoseconds'],
  lbClosingIdleTimeoutMs: ['resources', 'loadBalancer', 'mappingTimeouts', 'closingIdleTimeoutNanoseconds'],
  lbResetIdleTimeoutMs: ['resources', 'loadBalancer', 'mappingTimeouts', 'resetIdleTimeoutNanoseconds'],
  packetGapMs: ['fixed', 'workload', 'defaults', 'packetGapNanoseconds'],
  bandwidthBitsPerSecond: ['fixed', 'network', 'defaults', 'bandwidthBitsPerSecond'],
  packetTravelTimeMs: ['fixed', 'network', 'defaults', 'packetTravelTimeNanoseconds'],
  linkQueue: ['fixed', 'network', 'defaults', 'queueCapacity'],
  loss: ['fixed', 'network', 'defaults', 'lossRate'],
  queuePolicy: ['fixed', 'network', 'defaults', 'queuePolicy'],
  congestionThresholdPercent: ['fixed', 'network', 'defaults', 'congestionThresholdPercent'],
  congestionSignalProbability: ['fixed', 'network', 'defaults', 'congestionSignalProbability'],
  forwardingBitsPerSecond: ['resources', 'loadBalancer', 'forwardingBitsPerSecond'],
  ingressBandwidthBitsPerSecond: ['resources', 'loadBalancer', 'ingress', 'bandwidthBitsPerSecond'],
  ingressPacketTravelTimeMs: ['resources', 'loadBalancer', 'ingress', 'packetTravelTimeNanoseconds'],
  ingressQueueCapacity: ['resources', 'loadBalancer', 'ingress', 'queueCapacity'],
  ingressLoss: ['resources', 'loadBalancer', 'ingress', 'lossRate'],
  ingressQueuePolicy: ['resources', 'loadBalancer', 'ingress', 'queuePolicy'],
  ingressCongestionThresholdPercent: ['resources', 'loadBalancer', 'ingress', 'congestionThresholdPercent'],
  ingressCongestionSignalProbability: ['resources', 'loadBalancer', 'ingress', 'congestionSignalProbability'],
};

const timeDecimals: Partial<Record<ScenarioField, number>> = {
  durationSeconds: 9,
  clientGapMs: 6,
  packetGapMs: 6,
  snapshotIntervalMs: 6,
  packetTravelTimeMs: 6,
  ingressPacketTravelTimeMs: 6,
  rtoInitialMs: 6,
  rtoMinimumMs: 6,
  rtoMaximumMs: 6,
  delayedAckMaxDelayMs: 6,
  tcpMSLMs: 6,
  tcpFinWaitTimeoutMs: 6,
  clientIdleTimeoutMs: 6,
  backendHalfOpenIdleTimeoutMs: 6,
  backendEstablishedIdleTimeoutMs: 6,
  lbHalfOpenIdleTimeoutMs: 6,
  lbEstablishedIdleTimeoutMs: 6,
  lbClosingIdleTimeoutMs: 6,
  lbResetIdleTimeoutMs: 6,
};

const thresholdFields = new Set<ScenarioField>([
  'congestionThresholdPercent', 'ingressCongestionThresholdPercent',
]);
const floatFields = new Set<ScenarioField>([
  'speed', 'loss', 'ingressLoss', ...thresholdFields,
  'congestionSignalProbability', 'ingressCongestionSignalProbability',
]);
const positiveFields = new Set<ScenarioField>([
  'snapshotIntervalMs', 'maxEvents', 'bandwidthBitsPerSecond',
  'forwardingBitsPerSecond', 'ingressBandwidthBitsPerSecond',
  'receiveBufferBytes', 'mssBytes',
  'delayedAckSegments', 'delayedAckMaxDelayMs',
  'initialCongestionWindowSegments',
  'rtoInitialMs', 'rtoMinimumMs', 'rtoMaximumMs',
  'tcpMSLMs', 'tcpFinWaitTimeoutMs',
]);

// Omitted settings retain their native shape while the form shows defaults.
const optionalDefaults: Partial<Record<ScenarioField, string | number | boolean>> = {
  mssBytes: 1024,
  receiveBufferBytes: 65535,
  sackEnabled: false,
  ECNenabled: false,
  delayedAckEnabled: false,
  delayedAckSegments: 2,
  delayedAckMaxDelayMs: 40_000_000,
  queuePolicy: 'tailDrop',
  congestionThresholdPercent: 0,
  congestionSignalProbability: 0,
  ingressQueuePolicy: 'tailDrop',
  ingressCongestionThresholdPercent: 0,
  ingressCongestionSignalProbability: 0,
  congestionControl: 'reno',
  cubicFastConvergence: true,
  initialCongestionWindowSegments: 4,
  maximumCongestionWindowSegments: 0,
  initialSlowStartThresholdBytes: 65535,
  rtoMode: 'adaptive',
  rtoBackoff: true,
  rtoInitialMs: 1_000_000_000,
  rtoMinimumMs: 200_000_000,
  rtoMaximumMs: 60_000_000_000,
  tcpMSLMs: 30_000_000_000,
  tcpFinWaitTimeoutMs: 60_000_000_000,
  tcpMaxFINRetries: 5,
  clientIdleTimeoutMs: 20_000_000_000,
  backendHalfOpenIdleTimeoutMs: 5_000_000_000,
  backendEstablishedIdleTimeoutMs: 0,
  lbHalfOpenIdleTimeoutMs: 5_000_000_000,
  lbEstablishedIdleTimeoutMs: 50_000_000_000,
  lbClosingIdleTimeoutMs: 120_000_000_000,
  lbResetIdleTimeoutMs: 10_000_000_000,
};

// Reject already-rounded numbers before converting native document integers.
function integer(value: ExactInteger): bigint {
  if (typeof value === 'number' && !Number.isSafeInteger(value)) {
    throw new Error('The scenario contains an unsafe integer number. Use a decimal string.');
  }
  return BigInt(value);
}

// Browser-safe values use numbers; larger values retain every digit as strings.
function documentInteger(value: bigint): ExactInteger {
  return value >= -maxSafeInteger && value <= maxSafeInteger ? Number(value) : String(value);
}

// Unit conversion uses decimal digits rather than floating-point multiplication.
function duration(raw: string, decimals: number): bigint {
  const match = /^(0|[1-9]\d*)(?:\.(\d+))?$/.exec(raw.trim());
  if (!match || (match[2]?.length ?? 0) > decimals) {
    throw new Error(`Enter a nonnegative decimal with at most ${decimals} fractional digits.`);
  }
  const fraction = (match[2] ?? '').padEnd(decimals, '0');
  return BigInt(match[1]) * (10n ** BigInt(decimals)) + BigInt(fraction || '0');
}

// Display exact seconds or milliseconds without rounding away a nanosecond.
function durationText(value: ExactInteger, decimals: number): string {
  const nanoseconds = integer(value);
  const scale = 10n ** BigInt(decimals);
  const whole = nanoseconds / scale;
  const fraction = String(nanoseconds % scale).padStart(decimals, '0').replace(/0+$/, '');
  return fraction ? `${whole}.${fraction}` : String(whole);
}

// Local validation checks field syntax and Go ranges; HTTP checks the whole scenario.
export function validateScenarioField(
  field: ScenarioField,
  raw: string,
  mssBytes: ExactInteger = 1024,
): string | undefined {
  const text = raw.trim();
  if (field === 'algorithm') {
    return text === 'roundRobin' || text === 'random' ? undefined : 'Choose a supported algorithm.';
  }
  if (field === 'rtoMode') {
    return text === 'fixed' || text === 'adaptive' ? undefined : 'Choose fixed or adaptive timing.';
  }
  if (field === 'congestionControl') {
    const valid = congestionModeOptions.some((option) => option.value === text);
    return valid ? undefined : 'Choose none, Reno, NewReno, Tahoe or CUBIC congestion control.';
  }
  if (field === 'rtoBackoff') {
    return text === 'true' || text === 'false' ? undefined : 'Choose whether timeout backoff is enabled.';
  }
  if (field === 'fastRetransmit') {
    return text === 'true' || text === 'false' ? undefined : 'Choose whether fast retransmit is enabled.';
  }
  if (field === 'sackEnabled') {
    return text === 'true' || text === 'false' ? undefined : 'Choose whether selective ACKs are enabled.';
  }
  if (field === 'ECNenabled') {
    return text === 'true' || text === 'false' ? undefined : 'Choose whether ECN is enabled.';
  }
  if (field === 'delayedAckEnabled') {
    return text === 'true' || text === 'false' ? undefined : 'Choose whether delayed ACK is enabled.';
  }
  if (field === 'queuePolicy' || field === 'ingressQueuePolicy') {
    return text === '' || text === 'tailDrop' || text === 'threshold'
      ? undefined : 'Choose tail drop or threshold signaling.';
  }
  if (field === 'cubicFastConvergence') {
    return text === 'true' || text === 'false' ? undefined : 'Choose whether CUBIC fast convergence is enabled.';
  }
  if (floatFields.has(field)) {
    const value = Number(text);
    if (!/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i.test(text) || !Number.isFinite(value)) {
      return 'Enter a finite number.';
    }
    const maximum = thresholdFields.has(field) ? 100 : 1;
    if (value < 0 || (field !== 'speed' && value > maximum)) {
      return field === 'speed' ? 'Value must be at least 0.' : `Value must be between 0 and ${maximum}.`;
    }
    return;
  }

  let value: bigint;
  const decimals = timeDecimals[field];
  try {
    if (decimals !== undefined) {
      value = duration(text, decimals);
    } else {
      if (!/^-?(0|[1-9]\d*)$/.test(text)) return 'Enter a whole decimal integer.';
      value = BigInt(text);
    }
  } catch (error) {
    return error instanceof Error ? error.message : 'Enter a valid value.';
  }

  let minimum = positiveFields.has(field) ? 1n : 0n;
  let maximum = maxInt64;
  if (field === 'seed') minimum = -maxInt64 - 1n;
  if (field === 'bytes') minimum = -1n;
  if (field === 'clients') maximum = 16_777_215n;
  if (field === 'servers') maximum = 10_000n;
  if (field === 'maxEvents') maximum = maxUint64;
  if (field === 'receiveBufferBytes') maximum = 65535n;
  if (field === 'mssBytes') maximum = 65495n;
  if (field === 'delayedAckSegments') maximum = 2n;
  if (field === 'delayedAckMaxDelayMs') maximum = 500_000_000n;
  const windowField = field === 'maximumCongestionWindowSegments'
    || field === 'initialCongestionWindowSegments'
    || field === 'initialSlowStartThresholdBytes';
  if (windowField) {
    const mss = integer(mssBytes);
    if (mss < 1n || mss > 65495n) return 'Choose an MSS between 1 and 65495 bytes first.';
    if (field === 'initialSlowStartThresholdBytes') minimum = 2n * mss;
    else maximum = maxInt64 / mss;
  }
  if (field === 'tcpMSLMs') maximum = maxInt64 / 2n;
  if (field === 'tcpMaxFINRetries') maximum = 30n;
  if (value < minimum || value > maximum) {
    const unit = decimals === undefined ? '' : ' nanoseconds';
    return `Value must be between ${minimum} and ${maximum}${unit}.`;
  }
}

// Project fields for display only; this projection never replaces the native draft.
export function scenarioFormValues(scenario: Scenario, selectedAt = '0'): ScenarioDraft {
  const selected = selectedScenarioState(scenario, selectedAt);
  const values = {} as ScenarioDraft;
  for (const [name, path] of Object.entries(fieldPaths)) {
    const field = name as DirectField;

    const requested = path.reduce<unknown>((parent, key) => {
      return parent == null ? undefined : Reflect.get(parent as object, key);
    }, path[0] === 'fixed' ? scenario : selected);
    const inherited = field === 'fastRetransmit'
      ? (scenario.fixed.tcp?.congestionControl?.mode ?? 'reno') !== 'none'
      : optionalDefaults[field];
    const value = (field === 'queuePolicy' || field === 'ingressQueuePolicy') && requested === ''
      ? 'tailDrop' : requested ?? inherited;
    const decimals = timeDecimals[field];
    values[field] = decimals === undefined
      ? String(value)
      : durationText(value as ExactInteger, decimals);
  }
  const groups = selected.workload.clientGroups;
  values.clients = String(groups.reduce((sum, group) => sum + integer(group.count), 0n));
  values.servers = String(selected.resources.backends.length);
  const gaps = new Set(groups.map((group) => String(integer(group.clientGapNanoseconds))));
  values.clientGapMs = gaps.size > 1 ? '' : durationText(groups[0]?.clientGapNanoseconds ?? 0, 6);
  return values;
}

// Apply one explicit edit while keeping all unrelated native settings intact.
export function applyScenarioField(
  scenario: Scenario, field: ScenarioField, raw: string, selectedAt = '0', reservationBaseline = scenario,
): Scenario {
  selectedScenarioState(scenario, selectedAt);
  const error = validateScenarioField(field, raw, scenario.fixed.tcp?.mssBytes ?? 1024);
  if (error) throw new Error(error);
  const mode = scenario.fixed.tcp?.congestionControl?.mode ?? 'reno';
  if (field === 'fastRetransmit' && raw.trim() === 'true' && mode === 'none') {
    throw new Error('Fast retransmit requires Reno, NewReno, Tahoe or CUBIC congestion control.');
  }
  const updated = structuredClone(scenario);
  const selected = selectedScenarioState(updated, selectedAt);
  const usedIds = new Set([...resourceIds(scenario), ...resourceIds(reservationBaseline)]);
  if (field === 'clients') {
    resizeClients(selected, Number(raw), usedIds, selectedAt, scenario);
  } else if (field === 'servers') {
    resizeBackends(selected, Number(raw), usedIds, scenario);
  } else if (field === 'clientGapMs') {
    if (selected.workload.clientGroups.length === 0) resizeClients(selected, 0, usedIds, selectedAt, scenario);
    const value = documentInteger(duration(raw, 6));
    for (const group of selected.workload.clientGroups) group.clientGapNanoseconds = value;
  } else {
    const path = fieldPaths[field];
    let target: object = path[0] === 'fixed' ? updated : selected;
    // Create only the edited optional path, leaving unrelated defaults omitted.
    for (const key of path.slice(0, -1)) {
      if (Reflect.get(target, key) == null) Reflect.set(target, key, {});
      target = Reflect.get(target, key) as object;
    }
    let value: ExactInteger | boolean;
    if (field === 'algorithm' || field === 'rtoMode' || field === 'congestionControl'
      || field === 'queuePolicy' || field === 'ingressQueuePolicy') {
      value = raw.trim();
    } else if (
      field === 'rtoBackoff' || field === 'fastRetransmit'
      || field === 'cubicFastConvergence' || field === 'sackEnabled'
      || field === 'ECNenabled'
      || field === 'delayedAckEnabled'
    ) {
      value = raw.trim() === 'true';
    } else if (floatFields.has(field)) value = Number(raw);
    else {
      const decimals = timeDecimals[field];
      const parsed = decimals === undefined ? BigInt(raw) : duration(raw, decimals);
      value = documentInteger(parsed);
    }
    // Active threshold documents with omitted scalars already mean zero. Keep that
    // behavior when turning the policy off and back on, including reloaded runs.
    if ((field === 'queuePolicy' || field === 'ingressQueuePolicy')
      && Reflect.get(target, 'queuePolicy') === 'threshold') {
      if (Reflect.get(target, 'congestionThresholdPercent') === undefined) {
        Reflect.set(target, 'congestionThresholdPercent', 0);
      }
      if (Reflect.get(target, 'congestionSignalProbability') === undefined) {
        Reflect.set(target, 'congestionSignalProbability', 0);
      }
    }
    Reflect.set(target, path[path.length - 1], value);

    // Activating threshold signaling supplies useful settings only when omitted.
    // Explicit zeroes in imported scenarios remain meaningful and unchanged.
    if ((field === 'queuePolicy' || field === 'ingressQueuePolicy') && value === 'threshold') {
      if (Reflect.get(target, 'congestionThresholdPercent') === undefined) {
        Reflect.set(target, 'congestionThresholdPercent', 80);
      }
      if (Reflect.get(target, 'congestionSignalProbability') === undefined) {
        Reflect.set(target, 'congestionSignalProbability', 1);
      }
    }

    // The rwnd-only baseline disables early loss detection, without changing links.
    if (field === 'congestionControl' && value === 'none') {
      Reflect.set(target, 'fastRetransmit', false);
    }
  }

  // Keep SACK and its recovery policy explicit instead of silently changing either.
  if (updated.fixed.tcp?.sackEnabled) {
    const congestion = updated.fixed.tcp.congestionControl;
    const selectedMode = congestion?.mode ?? 'reno';
    const supportedMode = selectedMode === 'reno'
      || selectedMode === 'newReno' || selectedMode === 'cubic';
    if (!supportedMode) {
      throw new Error('SACK requires Reno, NewReno or CUBIC congestion control.');
    }
    if (congestion?.fastRetransmit === false) {
      throw new Error('SACK requires fast retransmit to be enabled.');
    }
  }

  if (updated.fixed.tcp?.ECNenabled && updated.fixed.tcp.congestionControl?.mode === 'none') {
    throw new Error('ECN requires Reno, NewReno, Tahoe or CUBIC congestion control.');
  }
  const congestion = updated.fixed.tcp?.congestionControl;
  // Changing MSS also changes every window configured in segments.
  const mss = integer(updated.fixed.tcp?.mssBytes ?? 1024);
  const threshold = integer(congestion?.initialSlowStartThresholdBytes ?? 65535);
  if (threshold < 2n * mss) {
    throw new Error(`The slow-start threshold must be at least ${2n * mss} bytes for this MSS.`);
  }
  const maximumSegments = maxInt64 / mss;
  const initialSegments = integer(congestion?.initialWindowSegments ?? 4);
  const cappedSegments = integer(congestion?.maximumWindowSegments ?? 0);
  if (initialSegments > maximumSegments || cappedSegments > maximumSegments) {
    throw new Error(`Congestion windows cannot exceed ${maximumSegments} segments for this MSS.`);
  }
  if (congestion?.mode === 'none' && integer(congestion.maximumWindowSegments ?? 0) !== 0n) {
    throw new Error('A maximum congestion window requires Reno, NewReno, Tahoe or CUBIC.');
  }
  reservations.set(updated, usedIds);
  return updated;
}

// Preserve groups and their relative sizes; never discard explicit client overrides.
function resizeClients(selected: ScenarioStateV2, count: number, used: Set<string>, selectedAt: string, scenario: Scenario) {
  const groups = selected.workload.clientGroups;
  if (groups.length === 0) {
    let id = 'clients';
    for (let index = 1; used.has(id); index++) id = `clients-${index}`;
    used.add(id);
    groups.push({
      id, count, startAtNanoseconds: documentInteger(BigInt(selectedAt)), clientGapNanoseconds: 0,
      network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} }, stream: {},
    });
    return;
  }

  let weights = groups.map((group) => integer(group.count));
  let total = weights.reduce((sum, value) => sum + value, 0n);
  if (total === 0n) {
    weights = groups.map(() => 1n);
    total = BigInt(groups.length);
  }
  const target = BigInt(count);
  const shares = weights.map((weight, index) => ({
    index,
    count: target * weight / total,
    remainder: target * weight % total,
  }));
  const allocated = shares.reduce((sum, share) => sum + share.count, 0n);
  const order = [...shares].sort((left, right) => {
    if (left.remainder === right.remainder) return left.index - right.index;
    return left.remainder > right.remainder ? -1 : 1;
  });
  for (let index = 0; index < Number(target - allocated); index++) order[index].count++;

  for (const share of shares) {
    const group = groups[share.index];
    const historicalCount = Object.entries(scenario.states)
      .filter(([at]) => BigInt(at) < BigInt(selectedAt))
      .flatMap(([, state]) => state.workload.clientGroups.filter(entry => entry.id === group.id))
      .reduce((maximum, entry) => integer(entry.count) > maximum ? integer(entry.count) : maximum, 0n);
    const slots = historicalCount > share.count ? historicalCount : share.count;
    const invalid = group.overrides?.find(override => integer(override.index) >= slots);
    if (invalid) throw new Error(`Group "${group.id}" needs at least ${integer(invalid.index) + 1n} clients to keep its explicit overrides.`);
    if (integer(group.count) !== share.count) group.count = Number(share.count);
  }
}

// Reservations follow the draft lineage, including IDs removed by earlier edits.
const reservations = new WeakMap<Scenario, Set<string>>();
export function retainResourceIds(updated: Scenario, baseline: Scenario): Scenario {
  reservations.set(updated, resourceIds(baseline));
  return updated;
}

export function resourceIds(scenario: Scenario): Set<string> {
  return new Set([
    ...(reservations.get(scenario) ?? []), scenario.fixed.loadBalancer.id,
    ...Object.values(scenario.states).flatMap(state => [
      ...state.resources.backends.map(backend => backend.id),
      ...state.workload.clientGroups.map(group => group.id),
    ]),
  ]);
}

export function scenarioStateTimes(scenario: Scenario): string[] {
  return Object.keys(scenario.states).sort((left, right) => BigInt(left) < BigInt(right) ? -1 : BigInt(left) > BigInt(right) ? 1 : 0);
}

export function selectedScenarioState(scenario: Scenario, selectedAt = '0'): ScenarioStateV2 {
  const state = scenario.states[selectedAt];
  if (!state) throw new Error(`Scenario state ${selectedAt} does not exist.`);
  return state;
}

// Compare mapped IPv6 spellings with IPv4 endpoints before choosing a new address.
function endpointKey(address: string, port: ExactInteger): string {
  let host = address.toLowerCase();
  try {
    if (host.includes(':')) host = new URL(`http://[${host}]/`).hostname;
    else if (/^\d+\.\d+\.\d+\.\d+$/.test(host)) host = new URL(`http://[::ffff:${host}]/`).hostname;
  } catch {
    // Non-IP host names retain the backend's case-insensitive comparison key.
  }
  return `${host}|${integer(port)}`;
}

// Existing backend settings stay in order; new backends inherit network defaults.
function resizeBackends(selected: ScenarioStateV2, count: number, usedIds: Set<string>, baseline: Scenario) {
  const endpoints = new Set(Object.values(baseline.states).flatMap(state => state.resources.backends.map(backend => endpointKey(backend.address, backend.port))));
  selected.resources.backends = selected.resources.backends.slice(0, count);
  const backends = selected.resources.backends;
  let idIndex = 1;
  let addressIndex = 0;
  while (backends.length < count) {
    while (usedIds.has(`backend-${idIndex}`)) idIndex++;
    const id = `backend-${idIndex++}`;
    let address: string;
    do {
      address = `10.0.${Math.floor(addressIndex / 254)}.${addressIndex % 254 + 1}`;
      addressIndex++;
    } while (endpoints.has(endpointKey(address, 80)));
    backends.push({
      id, name: id, address, port: 80,
      network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
    });
    usedIds.add(id);
    endpoints.add(endpointKey(address, 80));
  }
}
