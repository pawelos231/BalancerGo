import type { ScenarioField } from './draft';

export type NumericScenarioField = Exclude<
  ScenarioField,
  'algorithm' | 'rtoMode' | 'rtoBackoff' | 'congestionControl'
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
      'Initial limit on sent payload awaiting acknowledgements when Reno is enabled. ' +
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
      'Congestion-window size at which Reno switches from faster slow-start growth ' +
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
  lbHalfOpenIdleTimeoutMs: {
    name: 'lbHalfOpenIdleTimeoutMs',
    label: 'LB half-open idle timeout (virtual ms)',
    min: 0,
    hint: '0 disables idle cleanup.',
    help:
      'How long the LB retains an inactive connection whose handshake has not finished. ' +
      'This cleanup timer is independent of the TCP sender\'s retransmission timeout. ' +
      'Cleanup checks run every 5 virtual seconds. ' +
      'Set 0 to disable this idle cleanup.',
  },
  lbEstablishedIdleTimeoutMs: {
    name: 'lbEstablishedIdleTimeoutMs',
    label: 'LB established idle timeout (virtual ms)',
    min: 0,
    hint: '0 disables idle cleanup.',
    help:
      'How long the LB retains an established connection without traffic. ' +
      'A short value can remove the connection while a sender is waiting to retry. ' +
      'Cleanup checks run every 5 virtual seconds. ' +
      'Set 0 to disable this idle cleanup.',
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
    label: 'Snapshot interval (virtual ms)',
    min: 0,
    exclusiveMin: true,
    help:
      'Virtual time between state updates: 100 ms gives about 10 updates per real second at speed 1, ' +
      'or 20 at speed 2. Fast runs may skip intermediate updates. ' +
      'This also sets how often commands from the page are applied; ' +
      'simulated events still happen at their exact times.',
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
      'Basic Reno adds slow start, congestion avoidance and a window reduction after a timeout. ' +
      'Fast retransmit and fast recovery are not included yet. ' +
      'Mode none retains the receive-window-only baseline; retransmission timers still apply.',
    options: [
      { value: 'reno', label: 'Reno (basic)' },
      { value: 'none', label: 'None (receive window only)' },
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
    fields: ['clients', 'clientGapMs', 'packets', 'bytes', 'packetGapMs', 'receiveBufferBytes'],
    defaultExpanded: true,
  },
  {
    id: 'tcp-congestion',
    title: 'TCP congestion control',
    description: 'Limit data in flight and adjust the sender window as acknowledgements and timeouts arrive.',
    fields: ['congestionControl', 'initialCongestionWindowSegments', 'initialSlowStartThresholdBytes'],
    defaultExpanded: false,
  },
  {
    id: 'tcp-timers',
    title: 'TCP retransmission and idle timers',
    description: 'Choose RTT-based timing, retry backoff and independent client idle cleanup.',
    fields: [
      'rtoMode', 'rtoBackoff', 'rtoInitialMs', 'rtoMinimumMs', 'rtoMaximumMs',
      'clientIdleTimeoutMs',
    ],
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
      'lbHalfOpenIdleTimeoutMs',
      'lbEstablishedIdleTimeoutMs',
    ],
    defaultExpanded: false,
  },
  {
    id: 'links',
    title: 'Endpoint link defaults',
    description: 'Set default client and backend link properties. Profiles and directional overrides remain in place.',
    fields: ['bandwidthBitsPerSecond', 'packetTravelTimeMs', 'linkQueue', 'loss'],
    defaultExpanded: false,
  },
  {
    id: 'repeatability',
    title: 'Sampling and repeatability',
    description: 'Set snapshot frequency, the random seed and the event execution limit.',
    fields: ['snapshotIntervalMs', 'seed', 'maxEvents'],
    defaultExpanded: false,
  },
];
