import type { Scenario } from './types.ts';
import { JSONNumber, replaceLoneSurrogates } from './json.ts';
import { INT64_MAX, INT64_MIN, UINT64_MAX } from './values.ts';

type Shape =
  | { kind: 'object'; fields: Record<string, Shape> }
  | { kind: 'array' | 'map' | 'pointer'; item: Shape }
  | { kind: 'integer'; minimum: bigint; maximum: bigint }
  | { kind: 'string' | 'boolean' | 'float' };

const int64: Shape = { kind: 'integer', minimum: INT64_MIN, maximum: INT64_MAX };
const uint64: Shape = { kind: 'integer', minimum: 0n, maximum: UINT64_MAX };
const uint16: Shape = { kind: 'integer', minimum: 0n, maximum: 65535n };
const text: Shape = { kind: 'string' };
const boolean: Shape = { kind: 'boolean' };
const float: Shape = { kind: 'float' };

const object = (fields: Record<string, Shape>): Shape => ({ kind: 'object', fields });
const array = (item: Shape): Shape => ({ kind: 'array', item });
const pointer = (item: Shape): Shape => ({ kind: 'pointer', item });

// The shape follows Go's DTOs: pointers allow null, ordinary missing fields get zero values.
// Domain limits and relationships are checked after this structural pass.
const linkFields = {
  bandwidthBitsPerSecond: int64,
  packetTravelTimeNanoseconds: int64,
  queueCapacity: int64,
  lossRate: float,
  queuePolicy: text,
  congestionThresholdPercent: float,
  congestionSignalProbability: float,
};
const link = object(linkFields);
const linkPatch = object(Object.fromEntries(
  Object.entries(linkFields).map(([name, field]) => [name, pointer(field)]),
));
const network = object({ profile: text, toLoadBalancer: linkPatch, fromLoadBalancer: linkPatch });
const host = object({
  cores: pointer(int64),
  packetProcessingTimeNanoseconds: pointer(int64),
  queueCapacity: pointer(int64),
});
const streamFields = { packets: int64, bytes: int64, packetGapNanoseconds: int64 };
const stream = object(streamFields);
const streamPatch = object(Object.fromEntries(
  Object.entries(streamFields).map(([name, field]) => [name, pointer(field)]),
));
const timeouts = object({
  halfOpenIdleTimeoutNanoseconds: pointer(int64),
  establishedIdleTimeoutNanoseconds: pointer(int64),
  closingIdleTimeoutNanoseconds: pointer(int64),
  resetIdleTimeoutNanoseconds: pointer(int64),
});
const tcp = object({
  mssBytes: pointer(int64),
  receiveBufferBytes: pointer(int64),
  sackEnabled: pointer(boolean),
  ECNenabled: pointer(boolean),
  delayedAck: object({
    enabled: pointer(boolean),
    segmentsPerACK: pointer(int64),
    maxDelayNanoseconds: pointer(int64),
  }),
  rto: object({
    mode: pointer(text),
    exponentialBackoff: pointer(boolean),
    initialTimeoutNanoseconds: pointer(int64),
    minimumTimeoutNanoseconds: pointer(int64),
    maximumTimeoutNanoseconds: pointer(int64),
  }),
  close: object({
    mslNanoseconds: pointer(int64),
    finWaitTimeoutNanoseconds: pointer(int64),
    maxFINRetries: pointer(int64),
  }),
  congestionControl: object({
    mode: pointer(text),
    fastRetransmit: pointer(boolean),
    cubicFastConvergence: pointer(boolean),
    initialWindowSegments: pointer(int64),
    maximumWindowSegments: pointer(int64),
    initialSlowStartThresholdBytes: pointer(uint64),
  }),
  clientIdleTimeoutNanoseconds: pointer(int64),
  backendHalfOpenIdleTimeoutNanoseconds: pointer(int64),
  backendEstablishedIdleTimeoutNanoseconds: pointer(int64),
});
const backend = object({
  id: text,
  name: text,
  address: text,
  port: uint16,
  network,
  host,
  physicalAvailable: pointer(boolean),
  administrativeState: pointer(text),
});
const group = object({
  id: text,
  count: int64,
  startAtNanoseconds: int64,
  clientGapNanoseconds: int64,
  network,
  stream: streamPatch,
  overrides: array(object({ index: int64, network, stream: streamPatch })),
});
const state = object({
  resources: object({
    loadBalancer: object({
      algorithm: text,
      host,
      ingress: link,
      forwardingBitsPerSecond: int64,
      mappingTimeouts: timeouts,
    }),
    backends: array(backend),
  }),
  workload: object({ clientGroups: array(group) }),
});
const scenario = object({
  scenarioVersion: int64,
  fixed: object({
    execution: object({
      durationNanoseconds: int64,
      snapshotIntervalNanoseconds: int64,
      seed: int64,
      speed: float,
      maxEvents: uint64,
    }),
    tcp,
    loadBalancer: object({ id: text }),
    network: object({ defaults: link, profiles: array(object({ id: text, config: linkPatch })) }),
    workload: object({ defaults: stream }),
  }),
  states: { kind: 'map', item: state },
});

// Decode into a detached DTO before checking semantic rules; no casts of unchecked JSON escape.
export function decodeScenario(input: unknown): Scenario {
  return decode(scenario, input, 'scenario') as Scenario;
}

function invalid(path: string, message: string): never {
  throw new Error(`${path}: ${message}`);
}

// Follow the destination type, so a float may use exponent notation but an integer may not.
function decode(shape: Shape, input: unknown, path: string): unknown {
  if (shape.kind === 'pointer') {
    return input == null ? input : decode(shape.item, input, path);
  }
  if (input === null) invalid(path, 'must not be null');

  switch (shape.kind) {
    case 'object': {
      const source = input === undefined ? {} : record(input, path);
      for (const key of Object.keys(source)) {
        if (!Object.prototype.hasOwnProperty.call(shape.fields, key)) invalid(`${path}.${key}`, 'unknown field');
      }
      const result: Record<string, unknown> = {};
      for (const [key, field] of Object.entries(shape.fields)) {
        const value = decode(field, source[key], `${path}.${key}`);
        if (value !== undefined) result[key] = value;
      }
      return result;
    }
    case 'map': {
      const source = input === undefined ? {} : record(input, path);
      const result: Record<string, unknown> = Object.create(null);
      for (const [key, value] of Object.entries(source)) {
        result[key] = decode(shape.item, value, `${path}[${JSON.stringify(key)}]`);
      }
      return result;
    }
    case 'array': {
      if (input === undefined) return [];
      if (!Array.isArray(input)) invalid(path, 'expected an array');
      return Array.from(input, (value, index) => {
        // A JavaScript hole becomes JSON null; it is not an omitted object field.
        if (value === undefined) invalid(`${path}[${index}]`, 'array entries must not be omitted');
        return decode(shape.item, value, `${path}[${index}]`);
      });
    }
    case 'integer':
      return decodeInteger(input ?? 0, shape, path);
    case 'string':
      if (input === undefined) return '';
      if (typeof input !== 'string') invalid(path, 'expected a string');
      return replaceLoneSurrogates(input);
    case 'boolean':
      if (input === undefined) return false;
      if (typeof input !== 'boolean') invalid(path, 'expected a boolean');
      return input;
    case 'float': {
      const value = input instanceof JSONNumber ? Number(input.text) : (input ?? 0);
      if (typeof value !== 'number' || !Number.isFinite(value)) invalid(path, 'expected a finite number');
      return value;
    }
  }
}

function record(input: unknown, path: string): Record<string, unknown> {
  const prototype = typeof input === 'object' && input !== null ? Object.getPrototypeOf(input) : undefined;
  if (prototype !== Object.prototype && prototype !== null) invalid(path, 'expected an object');
  return input as Record<string, unknown>;
}

// Keep quoted integers as strings and reject unsafe numeric tokens before conversion.
function decodeInteger(input: unknown, shape: Extract<Shape, { kind: 'integer' }>, path: string): number | string {
  const raw = input instanceof JSONNumber ? input.text : input;
  if (typeof raw !== 'string' && typeof raw !== 'number') invalid(path, 'expected an integer number or decimal string');

  const text = String(raw);
  const digits = text.startsWith('-') ? text.slice(1) : text;
  if (digits === '' || /[^0-9]/.test(digits) || (digits.length > 1 && digits[0] === '0')) {
    invalid(path, 'expected a base-10 integer without fractions or exponents');
  }

  const value = BigInt(text);
  if (shape.minimum === 0n && text.startsWith('-')) invalid(path, 'integer outside unsigned range');
  if (value < shape.minimum || value > shape.maximum) invalid(path, 'integer outside field range');

  const quoted = typeof input === 'string';
  if (!quoted && (value < -BigInt(Number.MAX_SAFE_INTEGER) || value > BigInt(Number.MAX_SAFE_INTEGER))) {
    invalid(path, 'unsafe JSON integer; use a decimal string outside ±(2^53-1)');
  }
  return quoted ? text : Number(value);
}
