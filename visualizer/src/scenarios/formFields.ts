import type { ScenarioField } from './draft';
import { congestionModeOptions } from './types.ts';

export type NumericScenarioField = Exclude<
  ScenarioField,
  'algorithm' | 'rtoMode' | 'rtoBackoff' | 'congestionControl'
  | 'fastRetransmit' | 'cubicFastConvergence' | 'sackEnabled'
  | 'ECNenabled' | 'delayedAckEnabled' | 'queuePolicy' | 'ingressQueuePolicy'
>;

// Inputs keep their existing draft paths; these definitions only organize the form.
export interface NumericFieldDefinition {
  name: NumericScenarioField;
  label: string;
  help: string;
  integer?: boolean;
  min?: number;
  max?: number;
  exclusiveMin?: boolean;
  exclusiveMax?: boolean;
  hint?: string;
}

// The backend supplies defaults; each field keeps its own units and limits.
export const scenarioNumericFields: Record<NumericScenarioField, NumericFieldDefinition> = {
  delayedAckSegments: {
    name: 'delayedAckSegments',
    label: 'Segments per ACK',
    min: 1,
    max: 2,
    integer: true,
    help: 'With delayed ACK enabled, acknowledge after this many new in-order segments. ' +
      'Two groups a pair, including small segments; one sends immediately. ' +
      'Gaps, duplicates, closing packets and fresh congestion feedback bypass the wait.',
  },
  delayedAckMaxDelayMs: {
    name: 'delayedAckMaxDelayMs',
    label: 'Maximum ACK wait (virtual ms)',
    min: 0,
    exclusiveMin: true,
    max: 500,
    help: 'Maximum virtual wait from the first segment, before TCP creates its ACK. ' +
      'For example, the first segment at 10 ms with a 40 ms limit is acknowledged by 50 ms. ' +
      'The ACK then crosses the normal network links. Later segments do not restart this timer.',
  },
  maximumCongestionWindowSegments: {
    name: 'maximumCongestionWindowSegments',
    label: 'Maximum congestion window (segments)',
    min: 0,
    integer: true,
    hint: '0 leaves the window uncapped.',
    help: 'Optional ceiling for cwnd, including recovery. It differs from the initial window, ' +
      'which can grow as ACKs arrive. A cap of one holds cwnd to one 1,024-byte segment. ' +
      'A capped window can expose delayed-ACK waiting. Requires congestion control other than none.',
  },
  congestionThresholdPercent: {
    name: 'congestionThresholdPercent',
    label: 'Default congestion threshold (%)',
    min: 0,
    max: 100,
    hint: 'Used by threshold signaling only.',
    help: 'Waiting occupancy that triggers threshold signaling, including the arriving packet. ' +
      '0% makes every packet that must wait eligible; 100% makes the arrival that fills the last waiting slot eligible. ' +
      'Later arrivals find a full queue and are dropped. ' +
      'Active transmissions and travelling packets do not count. This is an occupancy rule, not a queue-delay rule.',
  },
  congestionSignalProbability: {
    name: 'congestionSignalProbability',
    label: 'Default signal probability (0–1)',
    min: 0,
    max: 1,
    hint: '1 selects every eligible packet at or above the threshold.',
    help: 'Chance of selecting an arriving packet at or above the threshold. Selected ECN-capable packets ' +
      'receive a CE mark; selected packets without ECN are dropped early. 0 disables early signals. ' +
      'A full queue always drops the arriving packet, regardless of ECN or this probability.',
  },
  ingressCongestionThresholdPercent: {
    name: 'ingressCongestionThresholdPercent',
    label: 'LB ingress congestion threshold (%)',
    min: 0,
    max: 100,
    hint: 'Used by threshold signaling only.',
    help: 'Waiting occupancy threshold applied separately to each LB input queue, including the arriving packet. ' +
      '0% makes every packet that must wait eligible; 100% makes the arrival that fills the last waiting slot eligible. ' +
      'Later arrivals find a full queue and are dropped. ' +
      'Transmissions and packets already travelling do not occupy waiting slots.',
  },
  ingressCongestionSignalProbability: {
    name: 'ingressCongestionSignalProbability',
    label: 'LB ingress signal probability (0–1)',
    min: 0,
    max: 1,
    hint: '1 selects every eligible packet at or above the threshold.',
    help: 'At or above the threshold, selected ECN-capable packets receive CE marks and packets without ECN ' +
      'are dropped early. 0 disables early signals; 1 selects every eligible arrival. ' +
      'Full queues always drop. The same rule applies independently to the frontside and backside queues.',
  },
  clients: {
    name: 'clients',
    label: 'TCP clients (total)',
    integer: true,
    min: 0,
    max: 2 ** 24,
    exclusiveMax: true,
    help:
      'Number of clients scheduled for this run using the simplified TCP model. ' +
      'Changing the total redistributes clients proportionally across existing groups. ' +
      'A change that would remove a client with explicit overrides is rejected.',
  },
  servers: {
    name: 'servers',
    label: 'Backend servers',
    integer: true,
    min: 0,
    max: 10000,
    help:
      'Number of simulated backend servers available to the load balancer. ' +
      'Set 0 to examine a run with no available backend.',
  },
  packets: {
    name: 'packets',
    label: 'Default payload units per client (1,024 B)',
    integer: true,
    min: 0,
    hint: 'Used when default payload bytes per client = -1.',
    help:
      'Sets total payload to this count multiplied by 1,024 bytes when payload bytes per client is -1. ' +
      'Actual segments can be smaller when the receive window is below 1,024 bytes. ' +
      'Explicit stream overrides on groups and clients stay unchanged. ' +
      'A client can send several segments before acknowledgements return, within the advertised receive window.',
  },
  bytes: {
    name: 'bytes',
    label: 'Default payload bytes per client',
    integer: true,
    min: -1,
    hint: '-1 uses the segment count; 0 or more sets the exact payload size.',
    help:
      'Set -1 to use the segment count multiplied by 1,024 payload bytes per client. ' +
      'Any nonnegative value overrides that count with an exact payload size; 0 sends no data. ' +
      'The final segment can be shorter than 1,024 bytes. Group and client overrides stay unchanged.',
  },
  receiveBufferBytes: {
    name: 'receiveBufferBytes',
    label: 'TCP receive buffer per connection (bytes)',
    integer: true,
    min: 1,
    max: 65535,
    help:
      'Maximum byte range a TCP receiver accepts ahead of its next missing byte. ' +
      'Out-of-order data waits in this connection\'s buffer until the gap is filled. ' +
      'Data received in order is consumed immediately. ' +
      'The advertised window limits sent payload that has not yet been acknowledged. ' +
      'That includes data already received while its acknowledgement is still returning. ' +
      'This is separate from link queues and bandwidth.',
  },
  initialCongestionWindowSegments: {
    name: 'initialCongestionWindowSegments',
    label: 'Initial congestion window (segments)',
    integer: true,
    min: 1,
    help:
      'Initial limit on sent payload awaiting acknowledgements ' +
      'with Reno, NewReno, Tahoe or CUBIC. ' +
      'The default of four 1,024-byte segments allows 4,096 bytes in flight before ACKs return. ' +
      'Values above four are experimental and exceed the selected RFC 5681 initial-window limit. ' +
      'The receiver window can impose a smaller limit. Mode none ignores this setting.',
  },
  initialSlowStartThresholdBytes: {
    name: 'initialSlowStartThresholdBytes',
    label: 'Initial slow-start threshold (bytes)',
    integer: true,
    min: 2048,
    help:
      'Congestion-window threshold for switching from faster slow-start growth ' +
      'to slower congestion avoidance. A retransmission timeout can lower this threshold. ' +
      'Must be at least two 1,024-byte segments. Mode none ignores this setting.',
  },
  rtoInitialMs: {
    name: 'rtoInitialMs',
    label: 'Initial retransmission timeout (virtual ms)',
    min: 0,
    exclusiveMin: true,
    help:
      'How long the sender initially waits for an acknowledgement before retransmitting. ' +
      'Adaptive mode updates the base delay from safe RTT samples; fixed mode keeps its base unchanged. ' +
      'Must be between the minimum and maximum timeout.',
  },
  rtoMinimumMs: {
    name: 'rtoMinimumMs',
    label: 'Minimum retransmission timeout (virtual ms)',
    min: 0,
    exclusiveMin: true,
    help:
      'Lower bound for the retransmission timer. The default is 200 ms. ' +
      'A smaller experimental value can retry too early when packets or ACKs are still travelling. ' +
      'Must not exceed the initial timeout.',
  },
  rtoMaximumMs: {
    name: 'rtoMaximumMs',
    label: 'Maximum retransmission timeout (virtual ms)',
    min: 0,
    exclusiveMin: true,
    help:
      'Upper bound for estimated and backed-off retransmission delays. ' +
      'For example, repeated timeouts stop doubling when this limit is reached. ' +
      'Must not be below the initial timeout.',
  },
  tcpMSLMs: {
    name: 'tcpMSLMs',
    label: 'Maximum segment lifetime (virtual ms)',
    min: 0,
    exclusiveMin: true,
    help:
      'How long an old packet may remain in the network. ' +
      'The active closer remains in TIME_WAIT for twice this duration, ' +
      'so it can acknowledge a repeated FIN before releasing the connection.',
  },
  tcpFinWaitTimeoutMs: {
    name: 'tcpFinWaitTimeoutMs',
    label: 'FIN wait timeout (virtual ms)',
    min: 0,
    exclusiveMin: true,
    help:
      'Maximum time to wait for the peer to finish closing after it acknowledges our FIN. ' +
      'If the peer never sends its FIN, the connection ends with a close timeout. ' +
      'Payload already acknowledged remains successful.',
  },
  tcpMaxFINRetries: {
    name: 'tcpMaxFINRetries',
    label: 'Maximum FIN retransmissions',
    integer: true,
    min: 0,
    max: 30,
    help:
      'How many times an unacknowledged FIN may be sent again after the first attempt. ' +
      'FIN retries use the TCP retransmission timer and its backoff setting. ' +
      'Set 0 for one attempt; reaching this limit ends the connection with a close failure.',
  },
  clientIdleTimeoutMs: {
    name: 'clientIdleTimeoutMs',
    label: 'Client idle timeout (virtual ms)',
    min: 0,
    hint: '0 disables idle cleanup.',
    help:
      'Closes a simulated client when no matching incoming packet arrives within this time. ' +
      'This is a separate cleanup policy: it can end the connection before a long retransmission timer expires. ' +
      'Set 0 to study retransmissions without this idle limit.',
  },
  backendHalfOpenIdleTimeoutMs: {
    name: 'backendHalfOpenIdleTimeoutMs',
    label: 'Backend handshake timeout (virtual ms)',
    min: 0,
    hint: '0 disables handshake cleanup.',
    help:
      'How long a backend reserves a half-open connection while waiting for the handshake to finish. ' +
      'Repeated SYNs do not extend this deadline. ' +
      'Expiry releases only the backend state; it does not stop the client or remove the LB mapping.',
  },
  backendEstablishedIdleTimeoutMs: {
    name: 'backendEstablishedIdleTimeoutMs',
    label: 'Backend connection idle timeout (virtual ms)',
    min: 0,
    hint: '0 disables idle cleanup (default).',
    help:
      'How long a backend retains an established connection without a valid incoming packet. ' +
      'Expiry releases only the backend state; the client and LB use their own timers. ' +
      'FIN retransmission and TIME_WAIT use the separate closing timers.',
  },
  lbHalfOpenIdleTimeoutMs: {
    name: 'lbHalfOpenIdleTimeoutMs',
    label: 'LB half-open idle timeout (virtual ms)',
    min: 0,
    hint: '0 disables idle cleanup.',
    help:
      'How long the LB retains a mapping whose handshake has not finished, ' +
      'measured from the last matching packet seen in either direction. ' +
      'Expiry removes only the LB mapping; endpoint TCP state is unchanged. ' +
      'Cleanup runs at the next sweep, every 5 virtual seconds. ' +
      'Set 0 to disable cleanup in this phase.',
  },
  lbEstablishedIdleTimeoutMs: {
    name: 'lbEstablishedIdleTimeoutMs',
    label: 'LB established idle timeout (virtual ms)',
    min: 0,
    hint: '0 disables idle cleanup.',
    help:
      'How long the LB retains an established mapping, ' +
      'measured from the last matching packet seen in either direction. ' +
      'Expiry removes only the LB mapping; endpoint TCP state is unchanged. ' +
      'Cleanup runs at the next sweep, every 5 virtual seconds. ' +
      'Set 0 to disable cleanup in this phase.',
  },
  lbClosingIdleTimeoutMs: {
    name: 'lbClosingIdleTimeoutMs',
    label: 'LB closing idle timeout (virtual ms)',
    min: 0,
    hint: 'After FIN. 0 disables cleanup in this phase.',
    help:
      'How long the LB retains a closing mapping after observing FIN, ' +
      'measured from the last matching packet seen in either direction. ' +
      'Expiry removes only the LB mapping; endpoint TCP state is unchanged. ' +
      'This limit is configured independently of TCP retransmission and closing timers. ' +
      'Cleanup runs at the next sweep, every 5 virtual seconds. ' +
      'Set 0 to disable cleanup in this phase.',
  },
  lbResetIdleTimeoutMs: {
    name: 'lbResetIdleTimeoutMs',
    label: 'LB reset idle timeout (virtual ms)',
    min: 0,
    hint: 'After RST. 0 disables cleanup in this phase.',
    help:
      'How long the LB retains a reset mapping after observing RST, ' +
      'measured from the last matching packet seen in either direction. ' +
      'Expiry removes only the LB mapping; endpoint TCP state is unchanged. ' +
      'This limit is configured independently of TCP retransmission and closing timers. ' +
      'Cleanup runs at the next sweep, every 5 virtual seconds. ' +
      'Set 0 to disable cleanup in this phase.',
  },
  durationSeconds: {
    name: 'durationSeconds',
    label: 'Virtual duration (s)',
    min: 0,
    help:
      'Hard end time on the virtual simulation clock. ' +
      'The run stops at this limit even if some streams are incomplete. ' +
      'The real time needed to reach it depends on playback speed and your computer.',
  },
  clientGapMs: {
    name: 'clientGapMs',
    label: 'Client creation interval (all groups, virtual ms)',
    min: 0,
    help:
      'Virtual delay between creating successive clients in each group. Editing this field applies to all groups. ' +
      'Set 0 to schedule all clients at the same virtual time; ' +
      'each has its own links, while shared LB ingress and forwarding limits still apply.',
  },
  packetGapMs: {
    name: 'packetGapMs',
    label: 'Default pause between segments (virtual ms)',
    min: 0,
    help:
      'Virtual pause before the first data segment after the handshake and between subsequent new segments. ' +
      'Acknowledgements release window space without restarting this pause. ' +
      'A full receive window can delay new data; retransmissions do not use this pause. ' +
      'Explicit group and client stream overrides stay unchanged.',
  },
  snapshotIntervalMs: {
    name: 'snapshotIntervalMs',
    label: 'Snapshot interval (real ms)',
    min: 0,
    exclusiveMin: true,
    help:
      'Real time between UI updates: 100 ms targets about 10 updates per second, ' +
      'independently of playback speed. Busy runs may update later. ' +
      'The final state is always published, even when a run finishes before the next update.',
  },
  bandwidthBitsPerSecond: {
    name: 'bandwidthBitsPerSecond',
    label: 'Default link bandwidth (bit/s)',
    integer: true,
    min: 0,
    exclusiveMin: true,
    help:
      'Default maximum bitrate of each client or backend link, including modeled IPv4 and TCP headers. ' +
      'Profiles and directional overrides retain their configured values. ' +
      'One gigabit per second is 1,000,000,000 bit/s, or 125 MB/s of raw traffic. ' +
      'Each client and backend has its own links. Incoming packets join their side\'s LB input queue, ' +
      'then use one shared LB input bandwidth limit. ' +
      'Links leaving the LB also share its independent forwarding budget.',
  },
  forwardingBitsPerSecond: {
    name: 'forwardingBitsPerSecond',
    label: 'LB forwarding budget (bits/s)',
    integer: true,
    min: 0,
    exclusiveMin: true,
    help:
      'One shared bitrate limit for all packets leaving the load balancer, ' +
      'both toward backend servers and toward clients. ' +
      'Each packet is counted once when its outgoing transmission starts, ' +
      'including modeled headers, control packets and retransmissions. ' +
      'Packets entering the load balancer are not counted against this budget.',
  },
  ingressBandwidthBitsPerSecond: {
    name: 'ingressBandwidthBitsPerSecond',
    label: 'LB ingress bandwidth (bit/s)',
    integer: true,
    min: 0,
    exclusiveMin: true,
    help:
      'One shared input bitrate for the client-side and backend-side queues. ' +
      'Packets from all clients and replies from all backends share this limit, ' +
      'including modeled headers, acknowledgements and retransmissions. ' +
      'Access-link transmission finishes before a packet joins its LB input queue. ' +
      'This setting is independent of the LB forwarding budget.',
  },
  ingressPacketTravelTimeMs: {
    name: 'ingressPacketTravelTimeMs',
    label: 'LB ingress packet travel time (virtual ms)',
    min: 0,
    help:
      'Extra virtual travel time after a packet finishes transmission through the shared LB input. ' +
      'It applies to both client packets and backend replies and is added to travel time on their access links. ' +
      'The LB receives the packet only after it completes both physical sections.',
  },
  ingressQueueCapacity: {
    name: 'ingressQueueCapacity',
    label: 'LB input slots per queue (packets)',
    integer: true,
    min: 0,
    help:
      'Waiting slots in each of the two LB input queues: one for clients and one for backends. ' +
      'A value of 1024 gives each queue 1024 slots; they still share one input bitrate limit. ' +
      'The oldest waiting packet across both queues uses the shared receiver next. ' +
      'Active transmissions and packets already travelling do not occupy waiting slots. ' +
      'With 0 slots, a packet is admitted only if transmission can start immediately; otherwise it is dropped. ' +
      'This is a physical network queue, not a kernel receive queue.',
  },
  ingressLoss: {
    name: 'ingressLoss',
    label: 'LB ingress loss probability',
    min: 0,
    max: 1,
    help:
      'Probability of losing a packet after transmission through the shared LB input. ' +
      'It applies to client packets and backend replies, including acknowledgements and retransmissions. ' +
      '0 means no random loss; 1 loses every packet on this link. ' +
      'A lost packet still consumes ingress bandwidth and can trigger a retry or timeout.',
  },
  packetTravelTimeMs: {
    name: 'packetTravelTimeMs',
    label: 'Default packet travel time (virtual ms)',
    min: 0,
    help:
      'Default virtual travel time after transmission on each client or backend link. ' +
      'On a link with 1 ms of travel time, a packet finishing transmission at 5 ms arrives at 6 ms. ' +
      'The link can transmit the next packet while the previous one is still travelling. ' +
      'The common ingress link has a separate travel time setting. Profiles and overrides stay unchanged.',
  },
  linkQueue: {
    name: 'linkQueue',
    label: 'Default link queue slots (packets)',
    integer: true,
    min: 0,
    help:
      'Waiting packet slots on each client or backend access link and outgoing LB path, excluding active transmissions ' +
      'and packets already travelling. LB output packets may wait for their link ' +
      'or the shared forwarding budget. New packets are dropped when the waiting slots are full; ' +
      '0 admits a packet only when it can start transmitting immediately. ' +
      'The client-side and backend-side LB input queues have a separate capacity setting.',
  },
  loss: {
    name: 'loss',
    label: 'Default packet loss probability (0-1)',
    min: 0,
    max: 1,
    help:
      'Independent probability of losing a packet on each client or backend link after transmission; ' +
      '0.1 means a 10% chance on each link. Use 0 for no random loss or 1 to lose every packet. ' +
      'Lost packets still consume link bandwidth and can trigger client retries or timeouts. ' +
      'The common ingress link has a separate loss probability setting.',
  },
  seed: {
    name: 'seed',
    label: 'Random seed',
    integer: true,
    help:
      'Starting value for the simulation random generators. ' +
      'The same configuration and seed reproduce the random choices ' +
      'when commands are also applied at the same virtual times.',
  },
  speed: {
    name: 'speed',
    label: 'Playback speed (virtual s / real s)',
    min: 0,
    hint: '0 = no waiting; 1 = real time; 2 = twice real time.',
    help:
      'Target pace of virtual time compared with real time. ' +
      'Use 0 to remove deliberate waits, 1 for real-time pacing, or 2 for two virtual seconds per real second. ' +
      'Heavy workloads may run slower than the selected pace.',
  },
  maxEvents: {
    name: 'maxEvents',
    label: 'Event execution limit',
    integer: true,
    min: 0,
    exclusiveMin: true,
    help:
      'Maximum number of simulated events, such as packet arrivals, timer expirations, ' +
      'health checks and state updates. The run stops with an error if it reaches this limit. ' +
      'Increase it for larger experiments.',
  },
};

export const algorithmHelp =
  'Policy used to choose a backend for new connections at the start of the run. ' +
  'Round Robin rotates through eligible backends; Random chooses using the seeded random generator. ' +
  'Changing the policy during a run affects new connections while existing ones keep their backend.';

export const rtoModeHelp =
  'Adaptive timing estimates RTT and its variation from acknowledgements. ' +
  'Fixed timing always starts from the configured initial timeout. ' +
  'Exponential backoff can be enabled or disabled independently in either mode.';

export const rtoBackoffHelp =
  'After a timeout, double the retry delay up to the maximum: for example, 1 s, 2 s, 4 s. ' +
  'Disable this to compare runs without timeout backoff. This does not enable congestion control.';

// Select inputs share rendering while keeping each transport policy's explanation close by.
export const scenarioSelectFields = {
  delayedAckEnabled: {
    label: 'Delayed acknowledgements',
    help: 'Group ordinary data acknowledgements until the segment threshold or virtual timer. ' +
      'Disabled sends ACKs immediately. A small send window or widely spaced packets may wait for ' +
      'the timer without reducing the number of ACKs, which can slow the transfer.',
    options: [
      { value: 'true', label: 'Enabled' },
      { value: 'false', label: 'Disabled (default)' },
    ],
  },
  ECNenabled: {
    label: 'Explicit congestion notification (ECN)',
    help: 'Negotiate ECN during the TCP handshake. A threshold queue can mark ECN-capable data ' +
      'instead of dropping it; the receiver echoes congestion and the sender reduces its congestion window. ' +
      'Requires Reno, NewReno, Tahoe or CUBIC. ECN is disabled by default and full queues still drop packets.',
    options: [
      { value: 'true', label: 'Enabled' },
      { value: 'false', label: 'Disabled' },
    ],
  },
  queuePolicy: {
    label: 'Default link queue policy',
    help: 'Tail drop admits packets until waiting slots are full. Threshold signaling can mark ECN data ' +
      'or drop packets without ECN before the queue fills. It uses waiting occupancy and a fixed probability; ' +
      'it is not RED or CoDel and does not measure waiting time.',
    options: [
      { value: 'tailDrop', label: 'Tail drop (default)' },
      { value: 'threshold', label: 'Threshold signaling' },
    ],
  },
  ingressQueuePolicy: {
    label: 'LB ingress queue policy',
    help: 'Apply tail drop or threshold signaling separately to both LB input queues. ' +
      'Threshold signaling marks selected ECN-capable data and drops selected packets without ECN. ' +
      'Full queues always drop, and both queues still share the same receiver bandwidth.',
    options: [
      { value: 'tailDrop', label: 'Tail drop (default)' },
      { value: 'threshold', label: 'Threshold signaling' },
    ],
  },
  sackEnabled: {
    label: 'Selective acknowledgements (SACK)',
    help:
      'The receiver reports byte ranges already received beyond a missing fragment. ' +
      'The sender can repair several gaps without waiting for each cumulative ACK. ' +
      'Requires Reno, NewReno or CUBIC with fast retransmit enabled. ' +
      'Disabled preserves the existing cumulative-ACK recovery profile.',
    options: [
      { value: 'true', label: 'Enabled' },
      { value: 'false', label: 'Disabled' },
    ],
  },
  algorithm: {
    label: 'Initial algorithm',
    help: algorithmHelp,
    options: [
      { value: 'roundRobin', label: 'Round Robin' },
      { value: 'random', label: 'Random' },
    ],
  },
  rtoMode: {
    label: 'Retransmission timing',
    help: rtoModeHelp,
    options: [
      { value: 'adaptive', label: 'Adaptive RTT' },
      { value: 'fixed', label: 'Fixed timeout' },
    ],
  },
  congestionControl: {
    label: 'Congestion control',
    help:
      'Reno, NewReno and Tahoe grow the sender window during successful transfers. ' +
      'CUBIC uses elapsed virtual time to shape window growth. ' +
      'After fast retransmit, Reno/CUBIC use fast recovery; Tahoe restarts slow start. ' +
      'NewReno stays in recovery through partial ACKs to retry more missing data. ' +
      'Mode none retains the receive-window-only baseline; retransmission timers still apply.',
    options: congestionModeOptions,
  },
  fastRetransmit: {
    label: 'Fast retransmit',
    help:
      'Three duplicate ACKs retry missing data before the timeout. ' +
      'Reno/NewReno/CUBIC use fast recovery; Tahoe restarts slow start with one segment. ' +
      'NewReno uses partial ACKs to retry more missing data before leaving recovery. ' +
      'Requires Reno, NewReno, Tahoe or CUBIC; disable this to compare with timeout-only recovery.',
    options: [
      { value: 'true', label: 'Enabled' },
      { value: 'false', label: 'Disabled' },
    ],
  },
  cubicFastConvergence: {
    label: 'CUBIC fast convergence',
    help:
      'When competing flows reduce available capacity, CUBIC lowers its remembered window peak ' +
      'to make room sooner. Disable this for an isolated single-flow experiment. ' +
      'This setting has no effect with Reno, NewReno, Tahoe or none.',
    options: [
      { value: 'true', label: 'Enabled' },
      { value: 'false', label: 'Disabled' },
    ],
  },
};

// Sections group existing settings without changing the native scenario document.
export interface ScenarioFieldSection {
  id: string;
  title: string;
  description: string;
  fields: readonly ScenarioField[];
  defaultExpanded?: boolean;
}

export const scenarioFieldSections: readonly ScenarioFieldSection[] = [
  {
    id: 'experiment',
    title: 'Experiment',
    description: 'Set the virtual duration and playback speed of the run.',
    fields: ['durationSeconds', 'speed'],
    defaultExpanded: true,
  },
  {
    id: 'tcp-traffic',
    title: 'TCP traffic',
    description: 'Set client arrivals, payload, receive buffering and pauses between segments.',
    fields: [
      'clients', 'clientGapMs', 'packets', 'bytes', 'packetGapMs',
      'receiveBufferBytes', 'sackEnabled',
    ],
    defaultExpanded: true,
  },
  {
    id: 'tcp-congestion',
    title: 'TCP congestion control',
    description: 'Limit data in flight and adjust the sender window as acknowledgements and timeouts arrive.',
    fields: [
      'congestionControl', 'ECNenabled', 'fastRetransmit', 'cubicFastConvergence',
      'initialCongestionWindowSegments', 'initialSlowStartThresholdBytes',
      'maximumCongestionWindowSegments',
    ],
    defaultExpanded: false,
  },
  {
    id: 'tcp-timers',
    title: 'TCP retransmission and idle timers',
    description: 'Choose RTT-based timing, retry backoff and independent client and backend cleanup.',
    fields: [
      'rtoMode', 'rtoBackoff', 'rtoInitialMs', 'rtoMinimumMs', 'rtoMaximumMs',
      'clientIdleTimeoutMs',
      'backendHalfOpenIdleTimeoutMs', 'backendEstablishedIdleTimeoutMs',
    ],
    defaultExpanded: false,
  },
  {
    id: 'tcp-acknowledgements',
    title: 'TCP acknowledgement timing',
    description: 'Choose immediate ACKs or group ordinary data ACKs with a bounded virtual wait.',
    fields: ['delayedAckEnabled', 'delayedAckSegments', 'delayedAckMaxDelayMs'],
    defaultExpanded: false,
  },
  {
    id: 'tcp-closing',
    title: 'TCP connection closing',
    description: 'Set FIN retry limits, the peer closing timeout and TIME_WAIT retention.',
    fields: ['tcpMSLMs', 'tcpFinWaitTimeoutMs', 'tcpMaxFINRetries'],
    defaultExpanded: false,
  },
  {
    id: 'load-balancer',
    title: 'Load balancer and backends',
    description: 'Choose backend resources, routing and the shared LB input and output limits.',
    fields: [
      'servers',
      'algorithm',
      'forwardingBitsPerSecond',
      'ingressBandwidthBitsPerSecond',
      'ingressPacketTravelTimeMs',
      'ingressQueueCapacity',
      'ingressLoss',
      'ingressQueuePolicy',
      'ingressCongestionThresholdPercent',
      'ingressCongestionSignalProbability',
      'lbHalfOpenIdleTimeoutMs',
      'lbEstablishedIdleTimeoutMs',
      'lbClosingIdleTimeoutMs',
      'lbResetIdleTimeoutMs',
    ],
    defaultExpanded: false,
  },
  {
    id: 'links',
    title: 'Endpoint link defaults',
    description: 'Set default client and backend link properties. Profiles and directional overrides remain in place.',
    fields: ['bandwidthBitsPerSecond', 'packetTravelTimeMs', 'linkQueue', 'loss',
      'queuePolicy', 'congestionThresholdPercent', 'congestionSignalProbability'],
    defaultExpanded: false,
  },
  {
    id: 'repeatability',
    title: 'UI updates and repeatability',
    description: 'Set the real-time snapshot interval, random seed and event execution limit.',
    fields: ['snapshotIntervalMs', 'seed', 'maxEvents'],
    defaultExpanded: false,
  },
];
