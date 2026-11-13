import type { FieldRules } from './fields.ts';
import {
  bitRate, choice, countValue, dataBytes, eventLimit,
  probability, seedValue, speedValue, timeValue,
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
};

export const tcpFields: FieldRules = {
  mss: { path: 'mssBytes', read: token => dataBytes(token, 1n, 65_495n) },
  congestion: {
    path: 'congestionControl.mode',
    read: token => choice(token, { reno: 'reno', newreno: 'newReno', tahoe: 'tahoe', cubic: 'cubic', none: 'none' }),
  },
};

export const balancerFields: FieldRules = {
  algo: { path: 'algorithm', read: token => choice(token, { rr: 'roundRobin', ran: 'random' }) },
  ingress: { path: 'ingress.bandwidthBitsPerSecond', read: bitRate },
  egress: { path: 'forwardingBitsPerSecond', read: bitRate },
};
