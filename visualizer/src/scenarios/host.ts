import type { ExactInteger, Scenario, ScenarioHostConfig } from './types.ts';
import type { V2HostConfig } from './v2Types.ts';
import { retainResourceIds, selectedScenarioState } from './draft.ts';

export type HostField = 'cores' | 'packetProcessingTimeUs' | 'queueCapacity';
export type HostValues = Record<HostField, string>;
export type HostTarget = 'loadBalancer' | `backend:${string}`;
export const backendHostTarget = (id: string): HostTarget => `backend:${id}`;

// Invalid text stays attached to its host until corrected or replaced by an import.
export interface HostEdits {
  values: Record<string, Partial<HostValues>>;
  errors: Record<string, string>;
}

export const emptyHostEdits = (): HostEdits => ({ values: {}, errors: {} });
export const idealHost = (): ScenarioHostConfig => ({
  cores: 1, packetProcessingTimeNanoseconds: 0, queueCapacity: 1024,
});
export const hostFields: readonly HostField[] = ['cores', 'packetProcessingTimeUs', 'queueCapacity'];
const maxDuration = (1n << 63n) - 1n;
const maxSafe = BigInt(Number.MAX_SAFE_INTEGER);

function exact(value: bigint): ExactInteger {
  return value <= maxSafe ? Number(value) : String(value);
}

function microseconds(value: ExactInteger): string {
  const nanoseconds = BigInt(value);
  const fraction = String(nanoseconds % 1000n).padStart(3, '0').replace(/0+$/, '');
  return `${nanoseconds / 1000n}${fraction ? `.${fraction}` : ''}`;
}

export function hostValues(host?: V2HostConfig): HostValues {
  return {
    cores: String(host?.cores ?? 1),
    packetProcessingTimeUs: microseconds(host?.packetProcessingTimeNanoseconds ?? 0),
    queueCapacity: String(host?.queueCapacity ?? 1024),
  };
}

// UI time is virtual microseconds; the native document retains exact nanoseconds.
function parseHostValue(field: HostField, raw: string): ExactInteger {
  const text = raw.trim();
  let value: bigint;
  if (field === 'packetProcessingTimeUs') {
    const match = /^(0|[1-9]\d*)(?:\.(\d{1,3}))?$/.exec(text);
    if (!match) throw new Error('Enter a nonnegative time with at most three decimal places.');
    value = BigInt(match[1]) * 1000n + BigInt((match[2] ?? '').padEnd(3, '0') || '0');
    if (value > maxDuration) throw new Error('Processing time exceeds the supported duration.');
  } else {
    if (!/^(0|[1-9]\d*)$/.test(text)) throw new Error('Enter a whole nonnegative number.');
    value = BigInt(text);
    const minimum = field === 'cores' ? 1n : 0n;
    const maximum = field === 'cores' ? 1024n : 1_000_000n;
    if (value < minimum || value > maximum) {
      throw new Error(`Value must be between ${minimum} and ${maximum}.`);
    }
  }
  return exact(value);
}

export function validateHostValue(field: HostField, raw: string): string | undefined {
  try {
    parseHostValue(field, raw);
  } catch (failure) {
    return failure instanceof Error ? failure.message : 'Invalid host setting.';
  }
}

export function hostErrorKey(target: string, field: HostField): string {
  return `${target}/${field}`;
}

export function scenarioHost(scenario: Scenario, target: HostTarget, selectedAt = '0'): V2HostConfig | undefined {
  return target === 'loadBalancer'
    ? selectedScenarioState(scenario, selectedAt).resources.loadBalancer.host
    : selectedScenarioState(scenario, selectedAt).resources.backends.find(backend => backendHostTarget(backend.id) === target)?.host;
}

function targetHost(scenario: Scenario, target: HostTarget, selectedAt = '0'): { host?: V2HostConfig } {
  if (target === 'loadBalancer') return selectedScenarioState(scenario, selectedAt).resources.loadBalancer;
  const backend = selectedScenarioState(scenario, selectedAt).resources.backends.find(entry => backendHostTarget(entry.id) === target);
  if (!backend) throw new Error(`Backend ${target} no longer exists.`);
  return backend;
}

function assignHostField(host: V2HostConfig, field: HostField, value: ExactInteger) {
  if (field === 'packetProcessingTimeUs') host.packetProcessingTimeNanoseconds = value;
  else host[field] = value;
}

// Changing one backend leaves every other host and unrelated scenario field intact.
export function editHostField(
  scenario: Scenario, edits: HostEdits, target: HostTarget, field: HostField, raw: string, selectedAt = '0',
): { scenario: Scenario; edits: HostEdits } {
  const next = {
    values: { ...edits.values, [target]: { ...edits.values[target], [field]: raw } },
    errors: { ...edits.errors },
  };
  const key = hostErrorKey(target, field);
  let value: ExactInteger;
  try {
    value = parseHostValue(field, raw);
  } catch (failure) {
    next.errors[key] = failure instanceof Error ? failure.message : 'Invalid host setting.';
    return { scenario, edits: next };
  }

  const updated = retainResourceIds(structuredClone(scenario), scenario);
  const resource = targetHost(updated, target, selectedAt);
  resource.host ??= {};
  assignHostField(resource.host, field, value);
  delete next.errors[key];
  return { scenario: updated, edits: next };
}

// Blank common values mean that imported backends have different effective settings.
export function commonBackendHostValues(scenario: Scenario, selectedAt = '0'): HostValues {
  const values = selectedScenarioState(scenario, selectedAt).resources.backends.map(backend => hostValues(backend.host));
  const first = values[0] ?? hostValues();
  return Object.fromEntries(hostFields.map(field => [
    field, values.every(value => value[field] === first[field]) ? first[field] : '',
  ])) as HostValues;
}

// This explicit action replaces backend settings, including pending individual edits.
export function applyHostToAllBackends(scenario: Scenario, values: HostValues, selectedAt = '0'): Scenario {
  const host: ScenarioHostConfig = {};
  for (const field of hostFields) assignHostField(host, field, parseHostValue(field, values[field]));
  const updated = retainResourceIds(structuredClone(scenario), scenario);
  for (const backend of selectedScenarioState(updated, selectedAt).resources.backends) backend.host = { ...host };
  return updated;
}

// A removed backend must not leave an invisible invalid edit blocking Start.
export function retainHostEdits(scenario: Scenario, edits: HostEdits, onlyLB = false, selectedAt = '0'): HostEdits {
  const backendTargets = onlyLB ? [] : selectedScenarioState(scenario, selectedAt).resources.backends.map(backend => backendHostTarget(backend.id));
  const targets = new Set(['loadBalancer', ...backendTargets]);
  const validErrors = new Set([...targets].flatMap(target => (
    hostFields.map(field => hostErrorKey(target, field))
  )));
  return {
    values: Object.fromEntries(Object.entries(edits.values).filter(([target]) => targets.has(target))),
    errors: Object.fromEntries(Object.entries(edits.errors).filter(([key]) => validErrors.has(key))),
  };
}
