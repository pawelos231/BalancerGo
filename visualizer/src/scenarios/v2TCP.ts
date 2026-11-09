import type { V2TCPConfig } from './v2Types.ts';
import { INT64_MAX } from './v2Values.ts';

// Apply the backend's TCP defaults before checking dependencies such as SACK and fast retransmit.
export function validateTCP(config: V2TCPConfig, path = 'fixed.tcp'): void {
  const mss = BigInt(config.mssBytes ?? 1024);
  requireRange(mss, 1n, 65_495n, `${path}.mssBytes`);
  requireRange(BigInt(config.receiveBufferBytes ?? 65_535), 1n, 65_535n, `${path}.receiveBufferBytes`);

  validateRTO(config.rto, `${path}.rto`);
  const congestion = validateCongestion(config.congestionControl, mss, `${path}.congestionControl`);
  validateDelayedACK(config.delayedAck, `${path}.delayedAck`);

  if (config.ECNenabled && congestion.mode === 'none') {
    throw new Error(`${path}.ECNenabled: requires congestion control`);
  }
  const supportsSACK = ['reno', 'newReno', 'cubic'].includes(congestion.mode);
  if (config.sackEnabled && (!supportsSACK || !congestion.fastRetransmit)) {
    throw new Error(`${path}.sackEnabled: requires Reno, NewReno or CUBIC with fastRetransmit enabled`);
  }

  validateClose(config.close, `${path}.close`);
  for (const field of [
    'clientIdleTimeoutNanoseconds',
    'backendHalfOpenIdleTimeoutNanoseconds',
    'backendEstablishedIdleTimeoutNanoseconds',
  ] as const) {
    if (BigInt(config[field] ?? 0) < 0n) {
      throw new Error(`${path}.${field}: must be nonnegative; zero disables idle cleanup`);
    }
  }
}

// A sender's byte-window arithmetic must stay within int64 for every configured MSS.
function validateCongestion(
  config: V2TCPConfig['congestionControl'],
  mss: bigint,
  path: string,
): { mode: string; fastRetransmit: boolean } {
  const mode = config?.mode ?? 'reno';
  if (!['none', 'reno', 'newReno', 'tahoe', 'cubic'].includes(mode)) {
    throw new Error(`${path}.mode: must be none, reno, newReno, tahoe or cubic`);
  }

  const fastRetransmit = config?.fastRetransmit ?? mode !== 'none';
  if (fastRetransmit && mode === 'none') {
    throw new Error(`${path}.fastRetransmit: requires Reno, NewReno, Tahoe or CUBIC congestion control`);
  }

  const maximumSegments = INT64_MAX / mss;
  const cap = BigInt(config?.maximumWindowSegments ?? 0);
  requireRange(cap, 0n, maximumSegments, `${path}.maximumWindowSegments`);
  if (cap !== 0n && mode === 'none') {
    throw new Error(`${path}.maximumWindowSegments: requires congestion control`);
  }

  requireRange(BigInt(config?.initialWindowSegments ?? 4), 1n, maximumSegments, `${path}.initialWindowSegments`);
  requireRange(
    BigInt(config?.initialSlowStartThresholdBytes ?? 65_535),
    2n * mss,
    INT64_MAX,
    `${path}.initialSlowStartThresholdBytes`,
  );
  return { mode, fastRetransmit };
}

// All timer modes require positive limits ordered as minimum <= initial <= maximum.
function validateRTO(config: V2TCPConfig['rto'], path: string): void {
  const mode = config?.mode ?? 'adaptive';
  if (mode !== 'fixed' && mode !== 'adaptive') {
    throw new Error(`${path}.mode: must be fixed or adaptive`);
  }

  const minimum = BigInt(config?.minimumTimeoutNanoseconds ?? 200_000_000);
  const initial = BigInt(config?.initialTimeoutNanoseconds ?? 1_000_000_000);
  const maximum = BigInt(config?.maximumTimeoutNanoseconds ?? 60_000_000_000);
  if (minimum <= 0n || initial <= 0n || maximum <= 0n) {
    throw new Error(`${path}: initial, minimum and maximum timeouts must be positive`);
  }
  if (minimum > initial || initial > maximum) {
    throw new Error(`${path}: require minimum <= initial <= maximum timeout`);
  }
}

// Even disabled delayed ACK keeps a valid threshold and timer ready for later experiments.
function validateDelayedACK(config: V2TCPConfig['delayedAck'], path: string): void {
  requireRange(BigInt(config?.segmentsPerACK ?? 2), 1n, 2n, `${path}.segmentsPerACK`);
  const delay = BigInt(config?.maxDelayNanoseconds ?? 40_000_000);
  if (delay <= 0n || delay > 500_000_000n) {
    throw new Error(`${path}.maxDelayNanoseconds: must be positive and at most 500 ms`);
  }
}

// TIME_WAIT lasts twice MSL, so that multiplication must also fit the timer range.
function validateClose(config: V2TCPConfig['close'], path: string): void {
  const msl = BigInt(config?.mslNanoseconds ?? 30_000_000_000);
  if (msl <= 0n || msl > INT64_MAX / 2n) {
    throw new Error(`${path}.mslNanoseconds: must be positive and at most half the time.Duration range`);
  }
  if (BigInt(config?.finWaitTimeoutNanoseconds ?? 60_000_000_000) <= 0n) {
    throw new Error(`${path}.finWaitTimeoutNanoseconds: must be positive`);
  }
  requireRange(BigInt(config?.maxFINRetries ?? 5), 0n, 30n, `${path}.maxFINRetries`);
}

function requireRange(value: bigint, minimum: bigint, maximum: bigint, path: string): void {
  if (value < minimum || value > maximum) {
    throw new Error(`${path}: must be in ${minimum}..${maximum}`);
  }
}
