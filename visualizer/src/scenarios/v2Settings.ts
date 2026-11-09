import type {
  ResolvedLinks,
  ScenarioExecution,
  ScenarioLinkConfig,
  ScenarioStream,
} from './types.ts';
import type {
  NullablePatch,
  V2EndpointNetwork,
  V2HostConfig,
  V2LinkPatch,
  V2NetworkConfig,
  V2StreamPatch,
} from './v2Types.ts';
import { validateID } from './v2Identity.ts';
import { INT64_MAX } from './v2Values.ts';

// Check virtual run limits separately from real-time playback and UI refreshes.
export function validateExecution(config: ScenarioExecution, path: string): void {
  if (BigInt(config.durationNanoseconds) <= 0n) {
    throw new Error(`${path}.durationNanoseconds: must be positive`);
  }
  if (BigInt(config.snapshotIntervalNanoseconds) <= 0n) {
    throw new Error(`${path}.snapshotIntervalNanoseconds: must be positive`);
  }
  if (!Number.isFinite(config.speed) || config.speed < 0) {
    throw new Error(`${path}.speed: must be finite and nonnegative`);
  }
  if (BigInt(config.maxEvents) <= 0n) {
    throw new Error(`${path}.maxEvents: must be positive`);
  }

  // Go also uses floating-point arithmetic for playback duration, not event timestamps.
  const pacedDuration = Number(config.durationNanoseconds) / config.speed;
  if (config.speed > 0 && pacedDuration >= Number(INT64_MAX)) {
    throw new Error(`${path}: paced duration exceeds time.Duration range`);
  }
}

// A link's limits apply to one direction; zero queue slots still permit active transmission.
export function validateLink(config: ScenarioLinkConfig, path: string): void {
  if (BigInt(config.bandwidthBitsPerSecond) <= 0n) {
    throw new Error(`${path}.bandwidthBitsPerSecond: must be positive`);
  }
  if (BigInt(config.packetTravelTimeNanoseconds) < 0n) {
    throw new Error(`${path}.packetTravelTimeNanoseconds: must be nonnegative`);
  }
  if (BigInt(config.queueCapacity) < 0n) {
    throw new Error(`${path}.queueCapacity: must be nonnegative`);
  }

  validateFraction(config.lossRate, 1, `${path}.lossRate`);
  const policy = config.queuePolicy ?? '';
  if (policy !== '' && policy !== 'tailDrop' && policy !== 'threshold') {
    throw new Error(`${path}.queuePolicy: must be tailDrop or threshold`);
  }
  validateFraction(config.congestionThresholdPercent ?? 0, 100, `${path}.congestionThresholdPercent`);
  validateFraction(config.congestionSignalProbability ?? 0, 1, `${path}.congestionSignalProbability`);
}

// Configured capacity accrues at zero cost; positive cost also bounds waiting time.
export function validateHost(config: V2HostConfig | undefined, duration: bigint, path: string): void {
  const cores = BigInt(config?.cores ?? 1);
  const cost = BigInt(config?.packetProcessingTimeNanoseconds ?? 0);
  const queue = BigInt(config?.queueCapacity ?? 1024);

  if (cores < 1n || cores > 1024n) {
    throw new Error(`${path}.cores: must be in 1..1024`);
  }
  if (cost < 0n) {
    throw new Error(`${path}.packetProcessingTimeNanoseconds: must be nonnegative`);
  }
  if (queue < 0n || queue > 1_000_000n) {
    throw new Error(`${path}.queueCapacity: must be in 0..1000000`);
  }

  const maximumParallelWork = cost > 0n && queue > cores ? queue : cores;
  if (duration > 0n && maximumParallelWork > INT64_MAX / duration) {
    throw new Error(`${path}: cores or queue capacity are too large for this duration`);
  }
}

// Validate named patches even when no endpoint currently selects them.
export function validateProfiles(config: V2NetworkConfig, path: string): Map<string, V2LinkPatch> {
  const profiles = new Map<string, V2LinkPatch>();

  config.profiles.forEach((profile, index) => {
    const profilePath = `${path}.profiles[${index}]`;
    validateID(profile.id, `${profilePath}.id`);
    if (profiles.has(profile.id)) {
      throw new Error(`${profilePath}.id: duplicate profile ${JSON.stringify(profile.id)}`);
    }

    validateLink(applyLinkPatch(config.defaults, profile.config), `${profilePath}.config`);
    profiles.set(profile.id, profile.config);
  });
  return profiles;
}

// Inherit both directions, apply a selected profile, then apply directional patches.
export function resolveNetwork(
  base: ResolvedLinks,
  input: V2EndpointNetwork,
  profiles: Map<string, V2LinkPatch>,
  path: string,
): ResolvedLinks {
  let toLoadBalancer = { ...base.toLoadBalancer };
  let fromLoadBalancer = { ...base.fromLoadBalancer };

  if (input.profile) {
    const profile = profiles.get(input.profile);
    if (profile === undefined) {
      throw new Error(`${path}.profile: unknown profile ${JSON.stringify(input.profile)}`);
    }
    toLoadBalancer = applyLinkPatch(toLoadBalancer, profile);
    fromLoadBalancer = applyLinkPatch(fromLoadBalancer, profile);
  }

  toLoadBalancer = applyLinkPatch(toLoadBalancer, input.toLoadBalancer);
  fromLoadBalancer = applyLinkPatch(fromLoadBalancer, input.fromLoadBalancer);
  validateLink(toLoadBalancer, `${path}.toLoadBalancer`);
  validateLink(fromLoadBalancer, `${path}.fromLoadBalancer`);
  return { toLoadBalancer, fromLoadBalancer };
}

// Null and omitted fields inherit; an explicit zero replaces the inherited value.
export function applyLinkPatch(base: ScenarioLinkConfig, patch: V2LinkPatch): ScenarioLinkConfig {
  return { ...base, ...suppliedValues(patch) };
}

// Keep bytes=-1 until the final client patch, so its packet count still determines its payload.
export function applyStreamPatch(base: ScenarioStream, patch: V2StreamPatch): ScenarioStream {
  return { ...base, ...suppliedValues(patch) };
}

// Packet count is a 1024-byte payload unit, independent of the configured MSS.
export function validateStream(config: ScenarioStream, path: string): void {
  const packets = BigInt(config.packets);
  let bytes = BigInt(config.bytes);

  if (packets < 0n) {
    throw new Error(`${path}.packets: must be nonnegative`);
  }
  if (bytes < -1n) {
    throw new Error(`${path}.bytes: must be -1 or nonnegative`);
  }
  if (BigInt(config.packetGapNanoseconds) < 0n) {
    throw new Error(`${path}.packetGapNanoseconds: must be nonnegative`);
  }

  if (bytes === -1n) {
    if (packets > INT64_MAX / 1024n) {
      throw new Error(`${path}.packets: stream size overflows int`);
    }
    bytes = packets * 1024n;
  }
  if (bytes > INT64_MAX - 40n) {
    throw new Error(`${path}.bytes: stream exceeds the simplified transport's range`);
  }
}

// Dropping nulls matches Go pointer patches without changing the supplied object.
function suppliedValues<T>(patch: NullablePatch<T>): Partial<T> {
  return Object.fromEntries(Object.entries(patch).filter(([, value]) => value != null)) as Partial<T>;
}

function validateFraction(value: number, maximum: number, path: string): void {
  if (!Number.isFinite(value) || value < 0 || value > maximum) {
    throw new Error(`${path}: must be finite and in 0..${maximum}`);
  }
}
