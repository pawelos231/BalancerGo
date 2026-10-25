import type { ScenarioField } from './draft';

export type NumericScenarioField = Exclude<ScenarioField, 'algorithm'>;

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
    label: 'Default segments per client',
    integer: true,
    min: 0,
    hint: 'Used when default payload bytes per client = -1.',
    help:
      'Number of 1,024-byte payload segments per client when payload bytes per client is -1. ' +
      'Explicit stream overrides on groups and clients stay unchanged. ' +
      'A client sends one segment at a time and waits for its acknowledgement before sending the next.',
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
      'Virtual pause before each new data segment: after the handshake for the first segment, ' +
      'then after each acknowledgement. Since each client waits for an acknowledgement, ' +
      'the actual interval between segments also includes the packet round trip. ' +
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
    description: 'Set client arrivals, total payload and pauses between data segments.',
    fields: ['clients', 'clientGapMs', 'packets', 'bytes', 'packetGapMs'],
    defaultExpanded: true,
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
