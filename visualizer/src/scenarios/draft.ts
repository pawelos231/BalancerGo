import type { SimulationConfig } from '../types';
import type { ExactInteger, Scenario } from './types';

export type ScenarioField = keyof SimulationConfig;
export type ScenarioDraft = Record<ScenarioField, string>;

const maxInt64 = (1n << 63n) - 1n;
const maxUint64 = (1n << 64n) - 1n;
const maxSafeInteger = BigInt(Number.MAX_SAFE_INTEGER);

type DirectField = Exclude<ScenarioField, 'clients' | 'servers' | 'clientGapMs'>;

// These paths change shared defaults without flattening endpoint overrides.
const fieldPaths: Record<DirectField, readonly string[]> = {
  algorithm: ['resources', 'loadBalancer', 'algorithm'],
  durationSeconds: ['execution', 'durationNanoseconds'],
  snapshotIntervalMs: ['execution', 'snapshotIntervalNanoseconds'],
  seed: ['execution', 'seed'],
  speed: ['execution', 'speed'],
  maxEvents: ['execution', 'maxEvents'],
  packets: ['workload', 'defaults', 'packets'],
  bytes: ['workload', 'defaults', 'bytes'],
  receiveBufferBytes: ['tcp', 'receiveBufferBytes'],
  rtoMode: ['tcp', 'rto', 'mode'],
  rtoBackoff: ['tcp', 'rto', 'exponentialBackoff'],
  rtoInitialMs: ['tcp', 'rto', 'initialTimeoutNanoseconds'],
  rtoMinimumMs: ['tcp', 'rto', 'minimumTimeoutNanoseconds'],
  rtoMaximumMs: ['tcp', 'rto', 'maximumTimeoutNanoseconds'],
  clientIdleTimeoutMs: ['tcp', 'clientIdleTimeoutNanoseconds'],
  lbHalfOpenIdleTimeoutMs: ['resources', 'loadBalancer', 'halfOpenIdleTimeoutNanoseconds'],
  lbEstablishedIdleTimeoutMs: ['resources', 'loadBalancer', 'establishedIdleTimeoutNanoseconds'],
  packetGapMs: ['workload', 'defaults', 'packetGapNanoseconds'],
  bandwidthBitsPerSecond: ['network', 'defaults', 'bandwidthBitsPerSecond'],
  packetTravelTimeMs: ['network', 'defaults', 'packetTravelTimeNanoseconds'],
  linkQueue: ['network', 'defaults', 'queueCapacity'],
  loss: ['network', 'defaults', 'lossRate'],
  forwardingBitsPerSecond: ['resources', 'loadBalancer', 'forwardingBitsPerSecond'],
  ingressBandwidthBitsPerSecond: ['resources', 'loadBalancer', 'ingress', 'bandwidthBitsPerSecond'],
  ingressPacketTravelTimeMs: ['resources', 'loadBalancer', 'ingress', 'packetTravelTimeNanoseconds'],
  ingressQueueCapacity: ['resources', 'loadBalancer', 'ingress', 'queueCapacity'],
  ingressLoss: ['resources', 'loadBalancer', 'ingress', 'lossRate'],
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
  clientIdleTimeoutMs: 6,
  lbHalfOpenIdleTimeoutMs: 6,
  lbEstablishedIdleTimeoutMs: 6,
};

const floatFields = new Set<ScenarioField>(['speed', 'loss', 'ingressLoss']);
const positiveFields = new Set<ScenarioField>([
  'snapshotIntervalMs', 'maxEvents', 'bandwidthBitsPerSecond',
  'forwardingBitsPerSecond', 'ingressBandwidthBitsPerSecond',
  'receiveBufferBytes',
  'rtoInitialMs', 'rtoMinimumMs', 'rtoMaximumMs',
]);

// Older documents keep their original shape while the form shows effective defaults.
const optionalDefaults: Partial<Record<ScenarioField, string | number | boolean>> = {
  receiveBufferBytes: 65535,
  rtoMode: 'adaptive',
  rtoBackoff: true,
  rtoInitialMs: 1_000_000_000,
  rtoMinimumMs: 200_000_000,
  rtoMaximumMs: 60_000_000_000,
  clientIdleTimeoutMs: 20_000_000_000,
  lbHalfOpenIdleTimeoutMs: 5_000_000_000,
  lbEstablishedIdleTimeoutMs: 50_000_000_000,
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
export function validateScenarioField(field: ScenarioField, raw: string): string | undefined {
  const text = raw.trim();
  if (field === 'algorithm') {
    return text === 'roundRobin' || text === 'random' ? undefined : 'Choose a supported algorithm.';
  }
  if (field === 'rtoMode') {
    return text === 'fixed' || text === 'adaptive' ? undefined : 'Choose fixed or adaptive timing.';
  }
  if (field === 'rtoBackoff') {
    return text === 'true' || text === 'false' ? undefined : 'Choose whether timeout backoff is enabled.';
  }
  if (floatFields.has(field)) {
    const value = Number(text);
    if (!/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i.test(text) || !Number.isFinite(value)) {
      return 'Enter a finite number.';
    }
    if (value < 0 || (field !== 'speed' && value > 1)) {
      return field === 'speed' ? 'Value must be at least 0.' : 'Value must be between 0 and 1.';
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
  if (value < minimum || value > maximum) {
    const unit = decimals === undefined ? '' : ' nanoseconds';
    return `Value must be between ${minimum} and ${maximum}${unit}.`;
  }
}

// Project fields for display only; this projection never replaces the native draft.
export function scenarioFormValues(scenario: Scenario): ScenarioDraft {
  const values = {} as ScenarioDraft;
  for (const [name, path] of Object.entries(fieldPaths)) {
    const field = name as DirectField;

    const requested = path.reduce<unknown>((parent, key) => {
      return parent == null ? undefined : Reflect.get(parent as object, key);
    }, scenario);
    const value = requested ?? optionalDefaults[field];
    const decimals = timeDecimals[field];
    values[field] = decimals === undefined
      ? String(value)
      : durationText(value as ExactInteger, decimals);
  }
  const groups = scenario.workload.clientGroups;
  values.clients = String(groups.reduce((sum, group) => sum + integer(group.count), 0n));
  values.servers = String(scenario.resources.backends.length);
  const gaps = new Set(groups.map((group) => String(integer(group.clientGapNanoseconds))));
  values.clientGapMs = gaps.size > 1 ? '' : durationText(groups[0]?.clientGapNanoseconds ?? 0, 6);
  return values;
}

// Apply one explicit edit while keeping all unrelated native settings intact.
export function applyScenarioField(scenario: Scenario, field: ScenarioField, raw: string): Scenario {
  const error = validateScenarioField(field, raw);
  if (error) throw new Error(error);
  const updated = structuredClone(scenario);
  if (field === 'clients') {
    resizeClients(updated, Number(raw));
  } else if (field === 'servers') {
    resizeBackends(updated, Number(raw));
  } else if (field === 'clientGapMs') {
    if (updated.workload.clientGroups.length === 0) resizeClients(updated, 0);
    const value = documentInteger(duration(raw, 6));
    for (const group of updated.workload.clientGroups) group.clientGapNanoseconds = value;
  } else {
    const path = fieldPaths[field];
    let target: object = updated;
    // Create only the edited optional path, leaving unrelated defaults omitted.
    for (const key of path.slice(0, -1)) {
      if (Reflect.get(target, key) == null) Reflect.set(target, key, {});
      target = Reflect.get(target, key) as object;
    }
    let value: ExactInteger | boolean;
    if (field === 'algorithm' || field === 'rtoMode') value = raw.trim();
    else if (field === 'rtoBackoff') value = raw.trim() === 'true';
    else if (floatFields.has(field)) value = Number(raw);
    else {
      const decimals = timeDecimals[field];
      const parsed = decimals === undefined ? BigInt(raw) : duration(raw, decimals);
      value = documentInteger(parsed);
    }
    Reflect.set(target, path[path.length - 1], value);
  }
  return updated;
}

// Preserve groups and their relative sizes; never discard explicit client overrides.
function resizeClients(scenario: Scenario, count: number) {
  const groups = scenario.workload.clientGroups;
  if (groups.length === 0) {
    const used = resourceIds(scenario);
    let id = 'clients';
    for (let index = 1; used.has(id); index++) id = `clients-${index}`;
    groups.push({
      id, count, startAtNanoseconds: 0, clientGapNanoseconds: 0,
      network: { toLoadBalancer: {}, fromLoadBalancer: {} }, stream: {},
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
    const invalid = group.overrides?.find((override) => integer(override.index) >= share.count);
    if (invalid) {
      throw new Error(`Group "${group.id}" needs at least ${integer(invalid.index) + 1n} clients to keep its explicit overrides.`);
    }
    if (integer(group.count) !== share.count) group.count = Number(share.count);
  }
}

// IDs belong to one shared resource namespace across LB, groups and backends.
function resourceIds(scenario: Scenario): Set<string> {
  return new Set([
    scenario.resources.loadBalancer.id,
    ...scenario.resources.backends.map((backend) => backend.id),
    ...scenario.workload.clientGroups.map((group) => group.id),
  ]);
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
function resizeBackends(scenario: Scenario, count: number) {
  scenario.resources.backends = scenario.resources.backends.slice(0, count);
  const backends = scenario.resources.backends;
  const usedIds = resourceIds(scenario);
  const endpoints = new Set(backends.map((backend) => endpointKey(backend.address, backend.port)));
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
      network: { toLoadBalancer: {}, fromLoadBalancer: {} },
    });
    usedIds.add(id);
    endpoints.add(endpointKey(address, 80));
  }
}
