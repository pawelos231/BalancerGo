import type { ExactInteger, ResolvedLinks, ScenarioLinkConfig, ScenarioStream } from './types.ts';
import type { ScenarioV2, V2HostConfig, V2MappingTimeouts } from './v2Types.ts';
import { applyStreamPatch, resolveNetwork } from './v2Settings.ts';

export interface PlannedHost {
  cores: ExactInteger;
  packetProcessingTimeNanoseconds: ExactInteger;
  queueCapacity: ExactInteger;
}

export interface PlannedClientOverride {
  index: ExactInteger;
  // Historical slots can remain configured after the current birth budget shrinks.
  reserved: boolean;
  network: ResolvedLinks;
  stream: ScenarioStream;
}

export interface PlannedClientGroup {
  id: string;
  count: ExactInteger;
  startAtNanoseconds: ExactInteger;
  clientGapNanoseconds: ExactInteger;
  network: ResolvedLinks;
  stream: ScenarioStream;
  overrides: PlannedClientOverride[];
}

export interface PlannedBackend {
  id: string;
  name?: string;
  address: string;
  port: ExactInteger;
  network: ResolvedLinks;
  host: PlannedHost;
  physicalAvailable: boolean;
  administrativeState: 'active' | 'drain';
}

// Configuration only: no clients, connections, packets or measurements are simulated.
export interface PlannedTopology {
  atNanoseconds: string;
  stateAtNanoseconds: string;
  durationNanoseconds: string;
  previousAtNanoseconds: string | null;
  nextAtNanoseconds: string | null;
  loadBalancer: {
    id: string;
    algorithm: string;
    host: PlannedHost;
    ingress: ScenarioLinkConfig;
    forwardingBitsPerSecond: ExactInteger;
    mappingTimeouts: V2MappingTimeouts;
  };
  backends: PlannedBackend[];
  clientGroups: PlannedClientGroup[];
}

// Seconds stay decimal throughout: 0.000000001 becomes exactly one nanosecond.
export function parseVirtualSeconds(text: string): bigint {
  const trimmed = text.trim();
  if (!/^(?:\d+(?:\.\d{0,9})?|\.\d{1,9})$/.test(trimmed)) {
    throw new Error('Enter nonnegative seconds with at most 9 decimal places.');
  }
  const [seconds, fraction = ''] = trimmed.split('.');
  return BigInt(seconds || '0') * 1_000_000_000n + BigInt(fraction.padEnd(9, '0'));
}

export function formatVirtualSeconds(nanoseconds: bigint | ExactInteger): string {
  const time = BigInt(nanoseconds);
  if (time < 0n) throw new Error('Virtual time must be nonnegative.');
  const seconds = time / 1_000_000_000n;
  const fraction = (time % 1_000_000_000n).toString().padStart(9, '0').replace(/0+$/, '');
  return fraction ? `${seconds}.${fraction}` : seconds.toString();
}

// The caller supplies a fully validated document; every selected state is complete.
// Work scales with declared groups and overrides, never with their client budgets.
export function projectPlannedTopology(scenario: ScenarioV2, atNanoseconds: bigint): PlannedTopology {
  const duration = BigInt(scenario.fixed.execution.durationNanoseconds);
  if (atNanoseconds < 0n || atNanoseconds > duration) {
    throw new Error(`Virtual time must be between 0 and ${formatVirtualSeconds(duration)} seconds.`);
  }

  const times = Object.keys(scenario.states).sort((left, right) => {
    const a = BigInt(left);
    const b = BigInt(right);
    return a < b ? -1 : a > b ? 1 : 0;
  });
  let selectedAt = times[0];
  let previousAt: string | null = null;
  let nextAt: string | null = null;
  for (const time of times) {
    const declaredAt = BigInt(time);
    if (declaredAt <= atNanoseconds) selectedAt = time;
    if (declaredAt < atNanoseconds) previousAt = time;
    if (declaredAt > atNanoseconds) {
      nextAt = time;
      break;
    }
  }

  const state = scenario.states[selectedAt];
  const fixed = scenario.fixed;
  const profiles = new Map(fixed.network.profiles.map(profile => [profile.id, profile.config]));
  const defaults: ResolvedLinks = {
    toLoadBalancer: fixed.network.defaults,
    fromLoadBalancer: fixed.network.defaults,
  };
  const loadBalancer = state.resources.loadBalancer;

  return {
    atNanoseconds: atNanoseconds.toString(),
    stateAtNanoseconds: selectedAt,
    durationNanoseconds: duration.toString(),
    previousAtNanoseconds: previousAt,
    nextAtNanoseconds: nextAt,
    loadBalancer: {
      id: fixed.loadBalancer.id,
      algorithm: loadBalancer.algorithm,
      host: resolveHost(loadBalancer.host),
      ingress: { ...loadBalancer.ingress },
      forwardingBitsPerSecond: loadBalancer.forwardingBitsPerSecond,
      mappingTimeouts: { ...loadBalancer.mappingTimeouts },
    },
    backends: state.resources.backends.map(backend => ({
      id: backend.id,
      name: backend.name,
      address: backend.address,
      port: backend.port,
      network: resolveNetwork(defaults, backend.network, profiles, `backend ${backend.id}`),
      host: resolveHost(backend.host),
      physicalAvailable: backend.physicalAvailable ?? true,
      administrativeState: backend.administrativeState ?? 'active',
    })),
    clientGroups: state.workload.clientGroups.map(group => {
      const network = resolveNetwork(defaults, group.network, profiles, `group ${group.id}`);
      // Preserve bytes=-1 until the individual packet count has also been applied.
      const stream = applyStreamPatch(fixed.workload.defaults, group.stream);
      return {
        id: group.id,
        count: group.count,
        startAtNanoseconds: group.startAtNanoseconds,
        clientGapNanoseconds: group.clientGapNanoseconds,
        network,
        stream: resolveStream(stream),
        overrides: (group.overrides ?? []).map(override => ({
          index: override.index,
          reserved: BigInt(override.index) >= BigInt(group.count),
          network: resolveNetwork(network, override.network, profiles, `group ${group.id} slot ${override.index}`),
          stream: resolveStream(applyStreamPatch(stream, override.stream)),
        })),
      };
    }),
  };
}

function resolveHost(host: V2HostConfig | undefined): PlannedHost {
  return {
    cores: host?.cores ?? 1,
    packetProcessingTimeNanoseconds: host?.packetProcessingTimeNanoseconds ?? 0,
    queueCapacity: host?.queueCapacity ?? 1024,
  };
}

function resolveStream(stream: ScenarioStream): ScenarioStream {
  return {
    ...stream,
    bytes: BigInt(stream.bytes) === -1n ? (BigInt(stream.packets) * 1024n).toString() : stream.bytes,
  };
}
