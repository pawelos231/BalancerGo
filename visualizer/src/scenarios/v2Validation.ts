import type { ResolvedLinks } from './types.ts';
import type {
  BackendStateV2,
  ScenarioStateV2,
  ScenarioV2,
  V2ClientGroup,
  V2LinkPatch,
} from './v2Types.ts';
import { TimelineIdentities, registerID, validateID } from './v2Identity.ts';
import {
  applyStreamPatch,
  resolveNetwork,
  validateExecution,
  validateHost,
  validateLink,
  validateProfiles,
  validateStream,
} from './v2Settings.ts';
import { validateTCP } from './v2TCP.ts';
import { endpointKey, INT64_MAX } from './v2Values.ts';

const maxBackends = 10_000;

// Check all states before Start; an invalid final state rejects the whole document.
// Shape and exact integer checks run first in the document decoder.
export function checkScenarioV2(input: ScenarioV2): void {
  if (BigInt(input.version) !== 2n) {
    throw new Error('scenario.version: expected version 2');
  }
  const times = timelineTimes(input.states);

  const fixed = input.fixed;
  validateExecution(fixed.execution, 'fixed.execution');
  const duration = BigInt(fixed.execution.durationNanoseconds);
  validateID(fixed.loadBalancer.id, 'fixed.loadBalancer.id');
  validateTCP(fixed.tcp, 'fixed.tcp');
  validateLink(fixed.network.defaults, 'fixed.network.defaults');
  const profiles = validateProfiles(fixed.network, 'fixed.network');
  validateStream(fixed.workload.defaults, 'fixed.workload.defaults');

  if (!Object.prototype.hasOwnProperty.call(input.states, '0')) {
    throw new Error('states: a state at timestamp 0 is required');
  }

  const identities = new TimelineIdentities();
  for (const [stateIndex, key] of times.entries()) {
    const at = BigInt(key);
    if (at >= duration) {
      throw new Error(`${statePath(key)}: timestamp must be below the simulation duration`);
    }
    validateState(input.states[key], input, profiles, identities, at, stateIndex);
  }
}

// Decimal keys are compared as bigint: 2 must precede 10, even above 2^53.
function timelineTimes(states: ScenarioV2['states']): string[] {
  const keys = Object.keys(states);
  for (const key of keys) {
    const canonical = key !== '' && (key === '0' || key[0] !== '0') && !/[^0-9]/.test(key);
    if (!canonical || BigInt(key) > INT64_MAX) {
      throw new Error(`${statePath(key)}: expected a canonical nonnegative int64 nanosecond timestamp`);
    }
  }
  return keys.sort((left, right) => {
    const leftTime = BigInt(left);
    const rightTime = BigInt(right);
    if (leftTime < rightTime) return -1;
    return leftTime > rightTime ? 1 : 0;
  });
}

// Complete state settings always start from fixed defaults, never the previous state.
function validateState(
  state: ScenarioStateV2,
  input: ScenarioV2,
  profiles: Map<string, V2LinkPatch>,
  identities: TimelineIdentities,
  at: bigint,
  stateIndex: number,
): void {
  const path = statePath(at.toString());
  const fixed = input.fixed;
  const duration = BigInt(fixed.execution.durationNanoseconds);
  const lbPath = `${path}.resources.loadBalancer`;
  const loadBalancer = state.resources.loadBalancer;

  if (loadBalancer.algorithm !== 'roundRobin' && loadBalancer.algorithm !== 'random') {
    throw new Error(`${lbPath}.algorithm: must be roundRobin or random`);
  }
  if (BigInt(loadBalancer.forwardingBitsPerSecond) <= 0n) {
    throw new Error(`${lbPath}.forwardingBitsPerSecond: must be positive`);
  }
  validateLink(loadBalancer.ingress, `${lbPath}.ingress`);
  validateHost(loadBalancer.host, duration, `${lbPath}.host`);
  for (const [name, timeout] of Object.entries(loadBalancer.mappingTimeouts)) {
    if (timeout != null && BigInt(timeout) < 0n) {
      throw new Error(`${lbPath}.mappingTimeouts.${name}: must be nonnegative; zero disables idle cleanup`);
    }
  }

  if (state.resources.backends.length > maxBackends) {
    throw new Error(`${path}.resources.backends: count must not exceed ${maxBackends}`);
  }
  const usedIDs = new Map([[fixed.loadBalancer.id, 'fixed.loadBalancer.id']]);
  const addresses = new Map<string, string>();
  const defaultLinks: ResolvedLinks = {
    toLoadBalancer: fixed.network.defaults,
    fromLoadBalancer: fixed.network.defaults,
  };

  // An empty backend list is legal, for example during a complete outage.
  for (const [index, backend] of state.resources.backends.entries()) {
    const backendPath = `${path}.resources.backends[${index}]`;
    registerID(usedIDs, backend.id, `${backendPath}.id`);
    validateAddress(backend, addresses, backendPath);
    identities.backend(backend, backendPath, stateIndex);
    resolveNetwork(defaultLinks, backend.network, profiles, `${backendPath}.network`);
    validateHost(backend.host, duration, `${backendPath}.host`);

    const administrativeState = backend.administrativeState ?? 'active';
    if (administrativeState !== 'active' && administrativeState !== 'drain') {
      throw new Error(`${backendPath}.administrativeState: must be active or drain`);
    }
  }

  for (const [index, group] of state.workload.clientGroups.entries()) {
    const groupPath = `${path}.workload.clientGroups[${index}]`;
    registerID(usedIDs, group.id, `${groupPath}.id`);
    const clientSlots = identities.group(group, at, groupPath, stateIndex);
    validateGroup(group, fixed, defaultLinks, profiles, clientSlots, groupPath);
  }
}

// Group settings apply to every client, then individual patches may adjust one slot.
function validateGroup(
  group: V2ClientGroup,
  fixed: ScenarioV2['fixed'],
  defaultLinks: ResolvedLinks,
  profiles: Map<string, V2LinkPatch>,
  clientSlots: bigint,
  path: string,
): void {
  if (BigInt(group.startAtNanoseconds) < 0n) {
    throw new Error(`${path}.startAtNanoseconds: must be nonnegative`);
  }
  if (BigInt(group.clientGapNanoseconds) < 0n) {
    throw new Error(`${path}.clientGapNanoseconds: must be nonnegative`);
  }

  const links = resolveNetwork(defaultLinks, group.network, profiles, `${path}.network`);
  const rawStream = applyStreamPatch(fixed.workload.defaults, group.stream);
  validateStream(rawStream, `${path}.stream`);

  const usedIndices = new Set<bigint>();
  for (const [index, override] of (group.overrides ?? []).entries()) {
    const overridePath = `${path}.overrides[${index}]`;
    const clientIndex = BigInt(override.index);
    if (clientIndex < 0n || clientIndex >= clientSlots) {
      throw new Error(`${overridePath}.index: must identify a client in this group`);
    }
    if (usedIndices.has(clientIndex)) {
      throw new Error(`${overridePath}.index: duplicate client index ${clientIndex}`);
    }
    usedIndices.add(clientIndex);

    resolveNetwork(links, override.network, profiles, `${overridePath}.network`);

    // Keep bytes=-1 until this client's packets override is applied: 3 packets -> 3 KiB.
    const clientStream = applyStreamPatch(rawStream, override.stream);
    validateStream(clientStream, `${overridePath}.stream`);
  }
}

// Compare endpoints using canonical host spelling while preserving the original document.
function validateAddress(backend: BackendStateV2, used: Map<string, string>, path: string): void {
  if (backend.address === '' || /[ \t\r\n]/.test(backend.address)) {
    throw new Error(`${path}.address: must be a nonempty host without whitespace`);
  }
  if (BigInt(backend.port) === 0n) {
    throw new Error(`${path}.port: must be in 1..65535`);
  }

  const endpoint = endpointKey(backend.address, backend.port);
  const previous = used.get(endpoint);
  if (previous !== undefined) {
    throw new Error(`${path}.address: endpoint ${JSON.stringify(endpoint)} already used by ${previous}`);
  }
  used.set(endpoint, path);
}

// Brackets keep state timestamps distinct from ordinary JSON field names in errors.
function statePath(key: string): string {
  return `states[${JSON.stringify(key)}]`;
}
