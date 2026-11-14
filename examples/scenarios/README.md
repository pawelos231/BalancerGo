# Scenario files

These 40 UTF-8 JSON files use the native `scenarioVersion: 2` contract accepted by
the frontend, CLI and HTTP API. Thirty-seven are native exports of the canonical
UI sources in `visualizer/src/scenarios/presets/sources/`; one
file demonstrates nested overrides, and two demonstrate the [timeline contract](CONTRACT.md). Telemetry uses
`snapshotVersion: 1` and reports use `reportVersion: 1`; these are independent
contracts. The library supports search, category filters and six cards per page.
Ordinary presets use seed 1, a 50-million-event limit, 100 real ms snapshots,
1 ms endpoint travel time, 1024 waiting slots and zero random loss unless their
experiment changes that property. Each file records its own settings; later CLI
overrides or changes to Go defaults do not alter it.

| File | Clients / backends | Experiment |
| --- | --- | --- |
| [simulator-limits.json](simulator-limits.json) | 50,000 / 100 | First-page scale stress: 10 TB/s clients, 100 TB/s backends, 10 PB/s shared LB limits. |
| [mixed-clients.json](mixed-clients.json) | 8 slow + 8 fast / 2 | Client links at 10 Mbit/s and 1 Gbit/s. |
| [mixed-backends.json](mixed-backends.json) | 16 / 2 | Backend links at 10 Mbit/s and 1 Gbit/s. |
| [ingress-bottleneck.json](ingress-bottleneck.json) | 32 / 2 | Shared LB input at 10 Mbit/s; forwarding budget at 1 Gbit/s. |
| [egress-bottleneck.json](egress-bottleneck.json) | 32 / 2 | Shared forwarding budget at 10 Mbit/s; input at 1 Gbit/s. |
| [simulator-throughput.json](simulator-throughput.json) | 50 / 5 | Unpaced simulator benchmark with very high configured rates. |
| [asymmetric-clients.json](asymmetric-clients.json) | 8 + 8 / 2 | 10 Mbit/s upload versus 10 Mbit/s return paths; opposite directions stay at 1 Gbit/s. |
| [mixed-latency.json](mixed-latency.json) | 8 + 8 / 2 | Clients with 0.1 ms versus 25 ms travel time in each direction. |
| [slow-ack-path.json](slow-ack-path.json) | 16 / 2 | Backend-to-LB links at 64 kbit/s slow the shared return paths, including ACKs. |
| [lossy-access.json](lossy-access.json) | 16 / 2 | Seeded 7.5% loss on client-to-LB links; return and backend paths remain loss-free. |
| [blackholed-backend.json](blackholed-backend.json) | 16 / 2 | One LB-to-backend link loses every packet; the other remains reachable. |
| [tiny-queues.json](tiny-queues.json) | 64 / 2 | Simultaneous arrivals, 1 Mbit/s LB input and two waiting slots per side. |
| [unbuffered-ingress.json](unbuffered-ingress.json) | 64 / 2 | Same input bottleneck with zero waiting slots: start immediately or drop. |
| [ecn-congestion.json](ecn-congestion.json) | 16 / 2 | Reno with ECN; 10 Mbit/s forward bottlenecks mark above 25% occupancy, while ACK paths remain fast. |
| [delayed-ack-small-window.json](delayed-ack-small-window.json) | 16 / 2 | Reno capped at one segment, with delayed ACK enabled and a 40 ms virtual timer. |
| [host-cpu-disabled.json](host-cpu-disabled.json) | 64 / 2 | Fast-link reference with ideal CPU processing on every host. |
| [lb-cpu-bottleneck.json](lb-cpu-bottleneck.json) | 64 / 2 | One LB core, 10 µs per packet and 128 CPU waiting slots. |
| [backend-cpu-bottleneck.json](backend-cpu-bottleneck.json) | 64 / 2 | Each backend has one core, 10 µs per packet and 128 CPU waiting slots. |
| [heterogeneous-backend-cpus.json](heterogeneous-backend-cpus.json) | 64 / 2 | Equal links with a slow one-core backend and a faster four-core backend. |
| [small-backend-cpu-queue.json](small-backend-cpu-queue.json) | 64 / 2 | Backend CPU processing with only eight waiting slots. |
| [lb-cpu-four-cores.json](lb-cpu-four-cores.json) | 64 / 2 | Four LB cores at 10 µs per packet, paired with the one-core LB preset. |
| [staggered-arrivals.json](staggered-arrivals.json) | 4 × 4 / 2 | Groups start at 0, 5, 20 and 200 s, with 250 ms between clients; the last group is beyond the horizon. |
| [mixed-streams.json](mixed-streams.json) | 8 + 8 / 2 | Short streams with 0, 1, 1024 and 1025-byte overrides alongside 1 GiB streams. |
| [seeded-random.json](seeded-random.json) | 16 / 4 | Random routing with exact seed `"9007199254740993"` for repeatable assignments. |
| [no-backends.json](no-backends.json) | 8 / 0 | Connection attempts with no destination backend; retries and cleanup. |
| [idle-network.json](idle-network.json) | 0 / 2 | Infrastructure without client traffic; scheduled maintenance events still execute. |
| [timeline-arrival-burst.json](timeline-arrival-burst.json) | 8 + 24 + 60 born / 2 | At 30 s add burst and staggered generators; at 90 s stop their future arrivals while born clients continue. |
| [timeline-lb-capacity.json](timeline-lb-capacity.json) | 16 slow + 16 fast / 2 | Shared LB ingress and forwarding: 1 Gbit/s, 100 Mbit/s at 30 s, recovery at 90 s. |
| [timeline-cpu-scaling.json](timeline-cpu-scaling.json) | 16 slow + 16 fast / 2 | LB CPU: zero cost, one core at 10 µs/packet at 30 s, four cores at 90 s. |
| [advanced-overrides.json](advanced-overrides.json) | 4 / 2 | Profile, directional settings and one explicit client override. |
| [Reference matrix](REFERENCE_EXPERIMENTS.md) | 16 / 2 | Eight cases crossing backend 2's travel RTT, return-link bandwidth and stream size. |

Host CPU presets use NewReno with SACK to sustain traffic when CPU queues overflow.
Their processing times are assumed experiment parameters, not measured hardware profiles.

The **Timeline changes** category uses ordinary complete `states` at 0, 30 and
90 virtual seconds, with a 180-second horizon and a five-billion-event ceiling.
Its explicit NewReno/SACK configuration, seed 1 and disabled TCP/mapping idle
cleanup keep long streams alive through the resource changes. Slow client
access is 20 Mbit/s; other access links are 2 Gbit/s. Backend links are 1 and
2 Gbit/s, respectively. Every client has a finite 1 GiB payload; incomplete
streams at the horizon are expected. The 32-client capacity/CPU cases cannot
finish their combined 32 GiB before either transition through the 1 Gbit/s LB.

In the arrival timeline, `count` is each generator's lifetime birth budget,
not a target number of currently active connections. The baseline's count 8
is repeated without creating it again. At 30 s, the new burst creates 24 clients
and the new `arrivals` group has a budget of 120, starting one client per second.
Removing both groups at 90 s cancels future births, including the birth exactly
at 90 s; the 24 burst clients and 60 staggered clients already born continue
their streams. There are 92 births in total, while the conservative run-wide
reservation remains 152 slots. The final planned topology shows only the
remaining baseline generator; live topology can still show the removed groups'
clients. See the [timeline contract](CONTRACT.md#client-generators-and-access-links-e112)
for the execution rules.

The **Reference experiments** category adds a fixed 2 x 2 x 2 matrix: backend 2
has a 4 or 40 ms base travel RTT and a 1 Gbit/s or 64 kbit/s return link;
every client sends 16 KiB or 1 MiB. Backend 1 stays at 4 ms and 1 Gbit/s.
All eight cases run for 60 virtual seconds with the same seed and round robin.
See [the reference guide](REFERENCE_EXPERIMENTS.md) for the files, fixed settings,
RTT calculation and current measurement limits.

Ordinary catalog presets run for 180 virtual seconds at speed 1 with 1 GiB per
client, except deliberate short/empty stream overrides. They may finish with
incomplete streams or clients scheduled beyond the horizon. The throughput example uses speed 0,
a 5 ms virtual horizon, 100 real ms snapshots and 4096-slot queues. Its client
links are 10 TB/s, backend links 100 TB/s, and both shared LB limits 1 PB/s
(decimal byte rates; the JSON stores bit/s). Transmission reaches the engine's
1 ns floor. This measures simulator execution speed, rather than Linux LB capacity.

**Absolute simulator limits** is pinned first in the UI. It uses a 1-ms virtual
horizon, a 100 real ms snapshot interval, speed 0, 1 GiB per client, a 20-million-event cap,
4096-slot endpoint queues and 131072 slots per LB input FIFO. Travel times,
random loss and client/segment pauses are zero. Its LB limits are exact strings
`"80000000000000000"` bit/s, beyond JavaScript's safe integer range. Model setup,
large telemetry frames and rendering are part of this scale experiment. The
existing 100 half-open slots per backend remain in effect: not all scheduled
clients must establish a connection before the horizon.

Snapshot intervals use real time, independently of playback speed and virtual
duration. Every run publishes its final state, including runs shorter than the
snapshot interval.

All library presets now pin their settings in standalone language sources;
changing Go defaults does not change these experiments. The four original topology
cases retain the current Go baseline's five-billion-event limit. The other ordinary
network and reference experiments retain their explicit 50-million-event baseline.
Search and category changes preserve the current draft. Only successful validation
loads another preset, replacing its source and generated JSON together.

The blackholed case simulates a failed directed link, without changing backend
health or implementing failover. Tail drops in the queue cases come from a
simultaneous SYN burst and bounded ingress admission. They are not inferred
from the sum of configured client bitrates. For quicker E2E automation, use an
independent copy with `speed: 0`, a bounded horizon and smaller stream sizes;
keep delayed start times and the behavior being tested in mind.

## Delayed acknowledgements

`fixed.tcp.delayedAck` contains `enabled` (default false), `segmentsPerACK` (1 or 2,
default 2) and `maxDelayNanoseconds` (positive, at most 500 ms; default 40 ms).
Ordinary data ACKs wait for that segment count or the timer from the first
arrival. Gaps, duplicates, closing packets and fresh congestion feedback get
prompt responses. The receiver never reads the client's remaining workload;
a final solitary segment is acknowledged by the timer.

The **Delayed ACK with a one-segment window** preset sets
`fixed.tcp.congestionControl.maximumWindowSegments` to 1 and leaves the receive
window at 65,535 B. This is an actual cwnd ceiling, not a small rwnd or merely
an initial window. Zero removes the ceiling; a positive ceiling requires a
congestion-control algorithm. Disable delayed ACK in the form to compare rates:
with one segment in flight, a second segment cannot arrive to trigger the pair.
Fewer ACKs can also slow a transfer, especially with a small window or sparse data.

CLI options are `-tcp-delayed-ack`, `-tcp-delayed-ack-segments`,
`-tcp-delayed-ack-max-delay` and `-tcp-maximum-window`.

## ECN and threshold signaling

The **ECN at a forward bottleneck** preset runs for 180 virtual seconds at speed 1
with 1 GiB per client. Only the two LB-to-backend links use threshold signaling:
each has 64 waiting slots, a 25% threshold and signal probability 1. Client links,
LB ingress and ACK return links remain at 1 Gbit/s with zero random loss. This
isolates congestion feedback from a slow return path. Inspect client TCP metrics
for negotiation, ECE/CWR and congestion responses; inspect the forward links for
ECN marks, queue drops and early congestion drops. Large streams can remain
unfinished at the horizon.

Native TCP settings use the exact field `fixed.tcp.ECNenabled`. It defaults to false
and requires Reno, NewReno, Tahoe or CUBIC. Queue settings are independent:
`queuePolicy` is `tailDrop` (also the behavior of an omitted/empty policy) or
`threshold`; `congestionThresholdPercent` ranges from 0 to 100, and
`congestionSignalProbability` ranges from 0 to 1. They can appear in link defaults,
profiles, directional patches and shared LB ingress. Existing presets keep ECN
and threshold signaling disabled.

Threshold signaling considers waiting occupancy including the arriving packet,
excluding the active transmission and packets already travelling. At or above
the threshold, selected ECN-capable data
gets a CE mark; selected packets without ECN are dropped early. Probability 1
selects every eligible arrival, and 0 disables early signaling. Full queues always
drop arriving packets. Early congestion drops are included in total queue drops,
while ECN marks are counted separately. This fixed occupancy rule is not RED or
CoDel and does not use time spent waiting.

Omitted threshold/probability scalars in imported documents mean zero. Validation
writes these effective values explicitly so saving and reloading preserves zero.
Selecting threshold signaling in a native draft initializes genuinely absent
values to 80% and probability 1; explicit zero values stay zero. Fresh UI and CLI
defaults use those 80%/1 parameters with tail drop selected.
Run this command from the `simulator` directory:

```sh
go run . -ecn-enabled -queue-policy threshold \
  -queue-threshold-percent 80 -queue-signal-probability 1
```

The independent ingress flags are `-lb-ingress-queue-policy`,
`-lb-ingress-queue-threshold-percent` and `-lb-ingress-queue-signal-probability`.
They also default to tail drop with inactive 80%/1 parameters. Choosing threshold
enables signaling; an explicit probability of zero disables early signals.

## Import, inspect and export

1. Start the backend and visualizer, then open **Simulation**.
2. In **Scenario files**, choose **Import JSON** and select one of these files.
   Import sends the original file text to backend validation before browser JSON
   parsing. A successful import replaces the draft; an error preserves it.
3. Select an existing state timestamp, then review the form and the **Native scenario** preview. **Validate draft** refreshes
   validation and the **Effective configuration** preview without starting a run.
4. Click the single **Start simulation** button when ready. Import, validation
   and preset selection never start an experiment.
5. **Export JSON** downloads the current native draft, including edits, profiles,
   all states, groups and individual overrides. It exports the draft rather than the active
   run's original document or the resolved preview.

Files must contain one complete JSON scenario and fit within **2 MiB of UTF-8
bytes**. Correct invalid form inputs before validating or exporting. The native
document is the format for import, export and Start. Other `scenarioVersion`
values and the generic `version` field are rejected. The form edits only fixed
settings or the selected state and preserves every other timestamp. State creation
and removal are later editor features. Effective configuration is
a read-only view of inherited values for backends, groups and explicit client
overrides; it is not an import format. Groups remain compact rather than expanding
into a list of every client.

The same validation is available without the UI. For example, from the repository
root with Docker Compose running:

```sh
curl --fail-with-body -H "Content-Type: application/json" \
  --data-binary @examples/scenarios/mixed-clients.json \
  http://localhost:8081/api/scenarios/validate
```

This request validates only. POST the same native file to `/api/simulations` to
start it explicitly. Local `go run .` uses port 8080 unless configured otherwise.

## Units and inheritance

| Field or value | Unit / meaning |
| --- | --- |
| `*Nanoseconds` | Exact virtual nanoseconds: `1000000` is 1 ms, `1000000000` is 1 s. |
| `bandwidthBitsPerSecond`, `forwardingBitsPerSecond` | Decimal bit/s; 1 Gbit/s is 125 MB/s of wire traffic. |
| `queueCapacity` | Waiting packet slots, excluding transmission and travel. LB ingress capacity applies separately to each of its two input FIFOs. |
| `lossRate` | Independent probability from 0 to 1, applied after transmission on that directed hop. |
| `bytes` | Payload bytes per stream; `-1` uses `packets * 1024`. A nonnegative value overrides the packet count. |
| `speed` | Virtual seconds per real second; 0 removes intentional pacing. |

Safe integers may be JSON numbers. Values outside `-(2^53 - 1)` through
`2^53 - 1` must be decimal strings, such as `"9007199254740993"` for a seed.
The backend checks each field's Go integer range and rejects unsafe unquoted
numbers. Keep these strings intact when editing or processing exported files.

Network defaults apply first, then a selected profile to both directions, then
directional patches. Client overrides apply after their group's settings.
`toLoadBalancer` means endpoint to LB; `fromLoadBalancer` means LB to endpoint,
for both clients and backends. Omitted patch values inherit; explicit zero sets
zero where that setting permits it. Stream defaults, group patches and individual
patches resolve before the `bytes: -1` sentinel is evaluated.

In `advanced-overrides.json`, ordinary clients inherit a 10 Mbit/s, 2 ms profile,
then use a 20 Mbit/s return link. Client index 1 (the second client) changes its
outgoing limit to 1 Mbit/s, return travel time to 0, payload to 2050 bytes and
sender pause to 0.1 ms. Backend 2 changes only LB-to-backend bandwidth to
25 Mbit/s. The other three clients inherit 64 KiB streams from `packets: 64`.

## Reproduce and interpret a run

Keep the exported full timeline, seed, code revision and Go/model version.
Configuration changes use saved `states`; the only manual runtime control is Stop. Compare
virtual results such as completions, simulated lifetimes and per-hop counters.
Wall time and packets/events per real second vary with the machine, background
load, pacing and snapshot work; the same seed does not make those timings equal.

Access-link queues, the two LB input FIFOs and per-endpoint output queues have
separate drop scopes. A tail drop happens when waiting capacity is full; random
loss happens after transmission and still consumes that hop's bandwidth. Delivery
and drop totals count physical-hop events, including retries, rather than unique
application packets. Work still queued, transmitting or travelling at the horizon
is unfinished work, rather than an extra recorded loss.

The current transport uses sliding windows, cumulative ACKs, configurable
congestion control and fixed or adaptive retransmission timers. SACK, ECN and
delayed ACK are optional. Reverse ACK paths also share LB resources. Configured
client bitrates are limits, so windows, ACK timing and losses can keep throughput
below them. Optional host profiles add finite virtual CPU service and its own
waiting queue. Detailed kernel RX processing is outside the current model.
