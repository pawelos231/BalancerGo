import type { FieldRules } from './fields.ts';
import type { Token } from './lexer.ts';
import {
  bitRate, booleanValue, choice, countValue, dataBytes, eventLimit,
  percentage, probability, seedValue, speedValue, textValue, timeValue,
} from './values.ts';

export const executionFields: FieldRules = {
  seed: { path: 'seed', read: seedValue },
  speed: { path: 'speed', read: speedValue },
  snapshots: { path: 'snapshotIntervalNanoseconds', read: token => timeValue(token, 1n) },
  'max-events': { path: 'maxEvents', read: eventLimit },
};

export const linkFields: FieldRules = {
  link: { path: 'bandwidthBitsPerSecond', read: bitRate },
  travel: { path: 'packetTravelTimeNanoseconds', read: token => timeValue(token) },
  queue: { path: 'queueCapacity', read: token => countValue(token) },
  loss: { path: 'lossRate', read: probability },
  'queue-policy': { path: 'queuePolicy', read: token => choice(token, { 'tail-drop': 'tailDrop', threshold: 'threshold' }) },
  'ecn-threshold': { path: 'congestionThresholdPercent', read: percentage },
  'signal-probability': { path: 'congestionSignalProbability', read: probability },
};

// Endpoint patches preserve null as inheritance; complete links keep concrete values.
export const linkPatchFields: FieldRules = Object.fromEntries(
  Object.entries(linkFields).map(([name, rule]) => [name, { ...rule, nullable: true }]),
);

export const hostFields: FieldRules = {
  cores: { path: 'cores', read: token => countValue(token, 1n, 1024n), nullable: true },
  'packet-time': { path: 'packetProcessingTimeNanoseconds', read: token => timeValue(token), nullable: true },
  queue: { path: 'queueCapacity', read: token => countValue(token, 0n, 1_000_000n), nullable: true },
};

const mappingFields: FieldRules = {
  'half-open': { path: 'halfOpenIdleTimeoutNanoseconds', read: token => timeValue(token), nullable: true },
  established: { path: 'establishedIdleTimeoutNanoseconds', read: token => timeValue(token), nullable: true },
  closing: { path: 'closingIdleTimeoutNanoseconds', read: token => timeValue(token), nullable: true },
  reset: { path: 'resetIdleTimeoutNanoseconds', read: token => timeValue(token), nullable: true },
};

// -1 keeps the native packets × 1024 fallback; other payloads use explicit byte units.
const payloadValue = (token: Token) => token.kind === 'number' && token.text === '-1' ? -1 : dataBytes(token);

export const streamFields: FieldRules = {
  payload: { path: 'bytes', read: payloadValue },
  packets: { path: 'packets', read: token => countValue(token) },
  'segment-gap': { path: 'packetGapNanoseconds', read: token => timeValue(token) },
};

export const clientStreamFields: FieldRules = Object.fromEntries(
  Object.entries(streamFields).map(([name, rule]) => [name, {
    ...rule, path: `stream.${rule.path}`, nullable: true,
  }]),
);

export const clientFields: FieldRules = {
  ...clientStreamFields,
  count: { path: 'count', read: token => countValue(token, 0n, 16_777_215n) },
  start: { path: 'startAtNanoseconds', read: token => timeValue(token) },
  every: { path: 'clientGapNanoseconds', read: token => timeValue(token) },
};

const congestionMode = (token: Token) => choice(token, {
  reno: 'reno', newreno: 'newReno', tahoe: 'tahoe', cubic: 'cubic', none: 'none',
});

const congestionFields: FieldRules = {
  mode: { path: 'mode', read: congestionMode, nullable: true },
  'fast-retransmit': { path: 'fastRetransmit', read: booleanValue, nullable: true },
  'fast-convergence': { path: 'cubicFastConvergence', read: booleanValue, nullable: true },
  'initial-window': { path: 'initialWindowSegments', read: token => countValue(token, 1n), nullable: true },
  'maximum-window': { path: 'maximumWindowSegments', read: token => countValue(token), nullable: true },
  ssthresh: { path: 'initialSlowStartThresholdBytes', read: token => dataBytes(token), nullable: true },
};

const rtoFields: FieldRules = {
  mode: { path: 'mode', read: token => choice(token, { fixed: 'fixed', adaptive: 'adaptive' }), nullable: true },
  backoff: { path: 'exponentialBackoff', read: booleanValue, nullable: true },
  initial: { path: 'initialTimeoutNanoseconds', read: token => timeValue(token, 1n), nullable: true },
  minimum: { path: 'minimumTimeoutNanoseconds', read: token => timeValue(token, 1n), nullable: true },
  maximum: { path: 'maximumTimeoutNanoseconds', read: token => timeValue(token, 1n), nullable: true },
};

const closeFields: FieldRules = {
  msl: { path: 'mslNanoseconds', read: token => timeValue(token, 1n), nullable: true },
  'fin-wait': { path: 'finWaitTimeoutNanoseconds', read: token => timeValue(token, 1n), nullable: true },
  'fin-retries': { path: 'maxFINRetries', read: token => countValue(token, 0n, 30n), nullable: true },
};

const delayedACKFields: FieldRules = {
  enabled: { path: 'enabled', read: booleanValue, nullable: true },
  segments: { path: 'segmentsPerACK', read: token => countValue(token, 1n, 2n), nullable: true },
  'max-delay': { path: 'maxDelayNanoseconds', read: token => timeValue(token, 1n), nullable: true },
};

export const tcpFields: FieldRules = {
  mss: { path: 'mssBytes', read: token => dataBytes(token, 1n, 65_495n), nullable: true },
  congestion: {
    path: 'congestionControl.mode', read: congestionMode, nullable: true,
  },
  'receive-buffer': { path: 'receiveBufferBytes', read: token => dataBytes(token, 1n, 65_535n), nullable: true },
  sack: { path: 'sackEnabled', read: booleanValue, nullable: true },
  ecn: { path: 'ECNenabled', read: booleanValue, nullable: true },
  'client-idle': { path: 'clientIdleTimeoutNanoseconds', read: token => timeValue(token), nullable: true },
  'backend-half-open-idle': {
    path: 'backendHalfOpenIdleTimeoutNanoseconds', read: token => timeValue(token), nullable: true,
  },
  'backend-established-idle': {
    path: 'backendEstablishedIdleTimeoutNanoseconds', read: token => timeValue(token), nullable: true,
  },
  rto: { path: 'rto', fields: rtoFields },
  close: { path: 'close', fields: closeFields },
  'delayed-ack': { path: 'delayedAck', fields: delayedACKFields },
  'congestion-control': { path: 'congestionControl', fields: congestionFields },
};

export const balancerFields: FieldRules = {
  algo: { path: 'algorithm', read: token => choice(token, { rr: 'roundRobin', ran: 'random' }) },
  ingress: { path: 'ingress', fields: linkFields, shorthand: 'link' },
  egress: { path: 'forwardingBitsPerSecond', read: bitRate },
  host: { path: 'host', fields: hostFields },
  'mapping-timeouts': { path: 'mappingTimeouts', fields: mappingFields },
};

export const backendFields: FieldRules = {
  name: { path: 'name', read: textValue },
  host: { path: 'host', fields: hostFields },
};

// Existing generators retain their initial start time; other supported settings stay mutable.
export const mutableClientFields: FieldRules = Object.fromEntries(
  Object.entries(clientFields).filter(([name]) => name !== 'start'),
);
