import simulatorLimitsSource from './presets/sources/simulator-limits.lb?raw';
import mixedClientsSource from './presets/sources/mixed-clients.lb?raw';
import mixedBackendsSource from './presets/sources/mixed-backends.lb?raw';
import ingressBottleneckSource from './presets/sources/ingress-bottleneck.lb?raw';
import egressBottleneckSource from './presets/sources/egress-bottleneck.lb?raw';
import simulatorThroughputSource from './presets/sources/simulator-throughput.lb?raw';
import asymmetricClientsSource from './presets/sources/asymmetric-clients.lb?raw';
import mixedLatencySource from './presets/sources/mixed-latency.lb?raw';
import slowAckPathSource from './presets/sources/slow-ack-path.lb?raw';
import lossyAccessSource from './presets/sources/lossy-access.lb?raw';
import blackholedBackendSource from './presets/sources/blackholed-backend.lb?raw';
import tinyQueuesSource from './presets/sources/tiny-queues.lb?raw';
import unbufferedIngressSource from './presets/sources/unbuffered-ingress.lb?raw';
import ecnCongestionSource from './presets/sources/ecn-congestion.lb?raw';
import delayedAckSmallWindowSource from './presets/sources/delayed-ack-small-window.lb?raw';
import hostCpuDisabledSource from './presets/sources/host-cpu-disabled.lb?raw';
import lbCpuBottleneckSource from './presets/sources/lb-cpu-bottleneck.lb?raw';
import backendCpuBottleneckSource from './presets/sources/backend-cpu-bottleneck.lb?raw';
import heterogeneousBackendCpusSource from './presets/sources/heterogeneous-backend-cpus.lb?raw';
import smallBackendCpuQueueSource from './presets/sources/small-backend-cpu-queue.lb?raw';
import lbCpuFourCoresSource from './presets/sources/lb-cpu-four-cores.lb?raw';
import staggeredArrivalsSource from './presets/sources/staggered-arrivals.lb?raw';
import mixedStreamsSource from './presets/sources/mixed-streams.lb?raw';
import seededRandomSource from './presets/sources/seeded-random.lb?raw';
import noBackendsSource from './presets/sources/no-backends.lb?raw';
import idleNetworkSource from './presets/sources/idle-network.lb?raw';
import referenceNearFastAckShortSource from './presets/sources/reference-near-fast-ack-short.lb?raw';
import referenceNearFastAckLongSource from './presets/sources/reference-near-fast-ack-long.lb?raw';
import referenceNearSlowAckShortSource from './presets/sources/reference-near-slow-ack-short.lb?raw';
import referenceNearSlowAckLongSource from './presets/sources/reference-near-slow-ack-long.lb?raw';
import referenceFarFastAckShortSource from './presets/sources/reference-far-fast-ack-short.lb?raw';
import referenceFarFastAckLongSource from './presets/sources/reference-far-fast-ack-long.lb?raw';
import referenceFarSlowAckShortSource from './presets/sources/reference-far-slow-ack-short.lb?raw';
import referenceFarSlowAckLongSource from './presets/sources/reference-far-slow-ack-long.lb?raw';
import timelineArrivalBurstSource from './presets/sources/timeline-arrival-burst.lb?raw';
import timelineLbCapacitySource from './presets/sources/timeline-lb-capacity.lb?raw';
import timelineCpuScalingSource from './presets/sources/timeline-cpu-scaling.lb?raw';
import type { ScenarioPreset } from './scenarioPresetTypes.ts';

export { presetCategories } from './scenarioPresetTypes.ts';
export type { ScenarioPreset } from './scenarioPresetTypes.ts';

// Catalog metadata selects canonical source files; JSON always comes from that source.
export const scenarioPresets: readonly ScenarioPreset[] = [
  {
    id: "simulator-limits",
    category: "performance",
    title: "Absolute simulator limits",
    description:
      "50,000 configured clients at 10 TB/s and 100 backends at 100 TB/s share 10 PB/s LB input " +
      "and output limits. Runs unpaced for 1 virtual ms.",
    source: simulatorLimitsSource,
  },
  {
    id: "mixed-clients",
    category: "links",
    title: "Mixed client links",
    description: "Two groups of 8 clients: 10 Mbit/s and 1 Gbit/s access links. Two fast backends.",
    source: mixedClientsSource,
  },
  {
    id: "mixed-backends",
    category: "links",
    title: "Heterogeneous backends",
    description: "16 fast clients and two backends with 10 Mbit/s and 1 Gbit/s links.",
    source: mixedBackendsSource,
  },
  {
    id: "ingress-bottleneck",
    category: "queues",
    title: "Shared input bottleneck",
    description: "32 fast clients share a 10 Mbit/s LB receiver. Output budget stays at 1 Gbit/s.",
    source: ingressBottleneckSource,
  },
  {
    id: "egress-bottleneck",
    category: "queues",
    title: "Shared output bottleneck",
    description: "32 fast clients share a 10 Mbit/s forwarding budget. Input limit stays at 1 Gbit/s.",
    source: egressBottleneckSource,
  },
  {
    id: "simulator-throughput",
    category: "performance",
    title: "Simulator throughput",
    description:
      "50 clients at 10 TB/s, 5 backends at 100 TB/s, and shared LB limits of 1 PB/s. Runs " +
      "unpaced for 5 virtual ms; packet transmission reaches the 1 ns engine floor.",
    source: simulatorThroughputSource,
  },
  {
    id: "asymmetric-clients",
    category: "links",
    title: "Asymmetric client links",
    description:
      "Two groups of 8 clients: one has a 10 Mbit/s uplink, the other a 10 Mbit/s downlink. The " +
      "opposite direction stays at 1 Gbit/s.",
    source: asymmetricClientsSource,
  },
  {
    id: "mixed-latency",
    category: "links",
    title: "Near and distant clients",
    description:
      "Two groups of 8 clients have 0.1 ms and 25 ms packet travel time in each direction. All " +
      "configured link rates stay at 1 Gbit/s.",
    source: mixedLatencySource,
  },
  {
    id: "slow-ack-path",
    category: "links",
    title: "Slow backend return path",
    description:
      "Both backend-to-LB links run at 64 kbit/s, delaying ACKs and other return packets. Client " +
      "links and the LB-to-backend direction stay at 1 Gbit/s.",
    source: slowAckPathSource,
  },
  {
    id: "lossy-access",
    category: "loss",
    title: "Lossy client uplinks",
    description:
      "Each client-to-LB packet has a seeded 7.5% loss probability after transmission. Reverse " +
      "links and both backend paths have no random loss.",
    source: lossyAccessSource,
  },
  {
    id: "blackholed-backend",
    category: "loss",
    title: "Blackholed backend link",
    description:
      "One LB-to-backend link loses every packet while the other backend remains reachable. This " +
      "models a failed link, rather than a backend health or CPU failure.",
    source: blackholedBackendSource,
  },
  {
    id: "tiny-queues",
    category: "queues",
    title: "Tiny LB input queues",
    description:
      "64 clients arrive together at a 1 Mbit/s LB receiver with 2 waiting slots per input " +
      "queue. Full queues tail-drop arriving packets; endpoint queues keep 1024 slots.",
    source: tinyQueuesSource,
  },
  {
    id: "unbuffered-ingress",
    category: "queues",
    title: "Unbuffered LB input",
    description:
      "64 clients arrive together at a 1 Mbit/s LB receiver with no waiting slots. An incoming " +
      "packet must start immediately or it is dropped.",
    source: unbufferedIngressSource,
  },
  {
    id: "ecn-congestion",
    category: "queues",
    title: "ECN at a forward bottleneck",
    description:
      "16 Reno clients negotiate ECN. Two 10 Mbit/s LB-to-backend links mark data at 25% waiting " +
      "occupancy and above, with 64 slots each. ACK return paths stay at 1 Gbit/s with no random " +
      "loss.",
    source: ecnCongestionSource,
  },
  {
    id: "delayed-ack-small-window",
    category: "transport",
    title: "Delayed ACK with a one-segment window",
    description:
      "16 Reno clients have cwnd capped at one segment and a 40 ms delayed-ACK timer. A second " +
      "segment cannot follow before the ACK. Disable delayed ACK to compare transfer rates.",
    source: delayedAckSmallWindowSource,
  },
  {
    id: "host-cpu-disabled",
    category: "host-cpu",
    title: "Host CPU disabled reference",
    description:
      "64 NewReno clients with SACK and fast links, with zero CPU processing cost on the LB and " +
      "both backends. Reference for the CPU presets.",
    source: hostCpuDisabledSource,
  },
  {
    id: "lb-cpu-bottleneck",
    category: "host-cpu",
    title: "Load balancer CPU bottleneck",
    description:
      "The LB has one virtual core, 10 µs per packet and 128 waiting slots. Data and returning " +
      "ACKs share its CPU; backend CPU is disabled.",
    source: lbCpuBottleneckSource,
  },
  {
    id: "backend-cpu-bottleneck",
    category: "host-cpu",
    title: "Backend CPU bottleneck",
    description:
      "Each backend has one virtual core, 10 µs per packet and 128 waiting slots. Fast links and " +
      "an ideal LB expose receiver processing limits.",
    source: backendCpuBottleneckSource,
  },
  {
    id: "heterogeneous-backend-cpus",
    category: "host-cpu",
    title: "Heterogeneous backend CPUs",
    description:
      "Equal links and Round Robin, but backend 1 has one core at 100 µs/packet and backend 2 " +
      "has four cores at 10 µs/packet.",
    source: heterogeneousBackendCpusSource,
  },
  {
    id: "small-backend-cpu-queue",
    category: "host-cpu",
    title: "Small backend CPU queues",
    description:
      "Each backend has one core at 10 µs/packet but only eight waiting slots. CPU queue " +
      "overflow drops packets independently of network queues.",
    source: smallBackendCpuQueueSource,
  },
  {
    id: "lb-cpu-four-cores",
    category: "host-cpu",
    title: "Load balancer CPU: four cores",
    description:
      "The same LB processing cost of 10 µs/packet with four virtual cores. Compare against the " +
      "one-core LB CPU bottleneck preset.",
    source: lbCpuFourCoresSource,
  },
  {
    id: "staggered-arrivals",
    category: "workload",
    title: "Staggered client waves",
    description:
      "Four groups of 4 clients start at 0, 5, 20 and 200 virtual seconds, with 250 ms between " +
      "clients. The last group starts beyond the 180-second horizon.",
    source: staggeredArrivalsSource,
  },
  {
    id: "mixed-streams",
    category: "workload",
    title: "Short and long streams",
    description:
      "8 short streams include explicit 0, 1, 1024 and 1025-byte cases; 8 long streams send 1 " +
      "GiB each. These boundaries show empty payloads and partial final segments.",
    source: mixedStreamsSource,
  },
  {
    id: "seeded-random",
    category: "routing",
    title: "Seeded random routing",
    description:
      "16 clients use random routing across 4 identical backends with the exact seed " +
      "9007199254740993. The seed stays a decimal string so every digit survives JSON exchange.",
    source: seededRandomSource,
  },
  {
    id: "no-backends",
    category: "edge-cases",
    title: "No available backends",
    description:
      "8 clients attempt to connect to an LB with no backends. Their SYN attempts exercise the " +
      "retry and connection cleanup path.",
    source: noBackendsSource,
  },
  {
    id: "idle-network",
    category: "edge-cases",
    title: "Idle network",
    description:
      "Two backends and all shared limits remain present, but no clients are created. This " +
      "separates an idle topology from traffic and queue activity.",
    source: idleNetworkSource,
  },
  {
    id: "reference-near-fast-ack-short",
    category: "reference",
    title: "Reference: 4 ms / fast-ack / 16 KiB",
    description:
      "16 clients, two backends, 16 KiB per client. Backend 2: 4 ms base travel RTT and 1 Gbit/s " +
      "backend-to-LB return link. Backend 1 stays at 4 ms and 1 Gbit/s; actual RTT also includes " +
      "transmission and queues.",
    source: referenceNearFastAckShortSource,
  },
  {
    id: "reference-near-fast-ack-long",
    category: "reference",
    title: "Reference: 4 ms / fast-ack / 1 MiB",
    description:
      "16 clients, two backends, 1 MiB per client. Backend 2: 4 ms base travel RTT and 1 Gbit/s " +
      "backend-to-LB return link. Backend 1 stays at 4 ms and 1 Gbit/s; actual RTT also includes " +
      "transmission and queues.",
    source: referenceNearFastAckLongSource,
  },
  {
    id: "reference-near-slow-ack-short",
    category: "reference",
    title: "Reference: 4 ms / slow-ack / 16 KiB",
    description:
      "16 clients, two backends, 16 KiB per client. Backend 2: 4 ms base travel RTT and 64 " +
      "kbit/s backend-to-LB return link. Backend 1 stays at 4 ms and 1 Gbit/s; actual RTT also " +
      "includes transmission and queues.",
    source: referenceNearSlowAckShortSource,
  },
  {
    id: "reference-near-slow-ack-long",
    category: "reference",
    title: "Reference: 4 ms / slow-ack / 1 MiB",
    description:
      "16 clients, two backends, 1 MiB per client. Backend 2: 4 ms base travel RTT and 64 kbit/s " +
      "backend-to-LB return link. Backend 1 stays at 4 ms and 1 Gbit/s; actual RTT also includes " +
      "transmission and queues.",
    source: referenceNearSlowAckLongSource,
  },
  {
    id: "reference-far-fast-ack-short",
    category: "reference",
    title: "Reference: 40 ms / fast-ack / 16 KiB",
    description:
      "16 clients, two backends, 16 KiB per client. Backend 2: 40 ms base travel RTT and 1 " +
      "Gbit/s backend-to-LB return link. Backend 1 stays at 4 ms and 1 Gbit/s; actual RTT also " +
      "includes transmission and queues.",
    source: referenceFarFastAckShortSource,
  },
  {
    id: "reference-far-fast-ack-long",
    category: "reference",
    title: "Reference: 40 ms / fast-ack / 1 MiB",
    description:
      "16 clients, two backends, 1 MiB per client. Backend 2: 40 ms base travel RTT and 1 Gbit/s " +
      "backend-to-LB return link. Backend 1 stays at 4 ms and 1 Gbit/s; actual RTT also includes " +
      "transmission and queues.",
    source: referenceFarFastAckLongSource,
  },
  {
    id: "reference-far-slow-ack-short",
    category: "reference",
    title: "Reference: 40 ms / slow-ack / 16 KiB",
    description:
      "16 clients, two backends, 16 KiB per client. Backend 2: 40 ms base travel RTT and 64 " +
      "kbit/s backend-to-LB return link. Backend 1 stays at 4 ms and 1 Gbit/s; actual RTT also " +
      "includes transmission and queues.",
    source: referenceFarSlowAckShortSource,
  },
  {
    id: "reference-far-slow-ack-long",
    category: "reference",
    title: "Reference: 40 ms / slow-ack / 1 MiB",
    description:
      "16 clients, two backends, 1 MiB per client. Backend 2: 40 ms base travel RTT and 64 " +
      "kbit/s backend-to-LB return link. Backend 1 stays at 4 ms and 1 Gbit/s; actual RTT also " +
      "includes transmission and queues.",
    source: referenceFarSlowAckLongSource,
  },
  {
    id: "timeline-arrival-burst",
    category: "timeline",
    title: "Timeline: burst, then stop arrivals",
    description:
      "8 slow baseline clients; at 30 s add 24 clients in a burst and a 120-client lifetime " +
      "budget at one arrival/s. At 90 s remove the added generators; clients already born keep " +
      "transferring.",
    source: timelineArrivalBurstSource,
  },
  {
    id: "timeline-lb-capacity",
    category: "timeline",
    title: "Timeline: shared LB capacity recovery",
    description:
      "32 clients with mixed access links keep sending while shared LB ingress and forwarding " +
      "fall from 1 Gbit/s to 100 Mbit/s at 30 s, then recover to 1 Gbit/s at 90 s.",
    source: timelineLbCapacitySource,
  },
  {
    id: "timeline-cpu-scaling",
    category: "timeline",
    title: "Timeline: CPU bottleneck and scale-up",
    description:
      "32 clients keep sending while LB CPU processing changes from zero cost to one core at 10 " +
      "µs/packet at 30 s, then four cores at the same cost at 90 s. CPU waiting capacity stays " +
      "at 128.",
    source: timelineCpuScalingSource,
  },
];
