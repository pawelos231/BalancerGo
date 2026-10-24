# Scenario files

These UTF-8 JSON files are native version-1 scenarios accepted by the backend.
Eighteen match the UI preset factories; one extra file demonstrates nested
overrides. The library supports search, category filters and six cards per page.
Ordinary presets use seed 1, a 50-million-event limit, 100 ms snapshots,
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
| [staggered-arrivals.json](staggered-arrivals.json) | 4 × 4 / 2 | Groups start at 0, 5, 20 and 200 s, with 250 ms between clients; the last group is beyond the horizon. |
| [mixed-streams.json](mixed-streams.json) | 8 + 8 / 2 | Short streams with 0, 1, 1024 and 1025-byte overrides alongside 1 GiB streams. |
| [seeded-random.json](seeded-random.json) | 16 / 4 | Random routing with exact seed `"9007199254740993"` for repeatable assignments. |
| [no-backends.json](no-backends.json) | 8 / 0 | Connection attempts with no destination backend; retries and cleanup. |
| [idle-network.json](idle-network.json) | 0 / 2 | Infrastructure without client traffic; scheduled maintenance events still execute. |
| [advanced-overrides.json](advanced-overrides.json) | 4 / 2 | Profile, directional settings and one explicit client override. |

Ordinary catalog presets run for 180 virtual seconds at speed 1 with 1 GiB per
client, except deliberate short/empty stream overrides. They may finish with
incomplete streams or clients scheduled beyond the horizon. The throughput example uses speed 0,
a 5 ms virtual horizon, 50 microsecond snapshots and 4096-slot queues. Its client
links are 10 TB/s, backend links 100 TB/s, and both shared LB limits 1 PB/s
(decimal byte rates; the JSON stores bit/s). Transmission reaches the engine's
1 ns floor. This measures simulator execution speed, rather than Linux LB capacity.

**Absolute simulator limits** is pinned first in the UI. It uses a 1-ms virtual
horizon and snapshot interval, speed 0, 1 GiB per client, a 20-million-event cap,
4096-slot endpoint queues and 131072 slots per LB input FIFO. Travel times,
random loss and client/segment pauses are zero. Its LB limits are exact strings
`"80000000000000000"` bit/s, beyond JavaScript's safe integer range. Model setup,
large telemetry frames and rendering are part of this scale experiment. The
existing 100 half-open slots per backend remain in effect: not all scheduled
clients must establish a connection before the horizon.

The original four topology presets inherit fetched backend defaults; the twelve
new factories use an explicit baseline so unrelated CLI overrides do not change
the property under test. The files above are concrete snapshots of those factories
using ordinary Go defaults. Search and category changes preserve the selected
candidate and the current form; only successful validation loads another preset.

The blackholed case simulates a failed directed link, without changing backend
health or implementing failover. Tail drops in the queue cases come from a
simultaneous SYN burst and bounded ingress admission. They are not inferred
from the sum of configured client bitrates. For quicker E2E automation, use an
independent copy with `speed: 0`, a bounded horizon and smaller stream sizes;
keep delayed start times and the behavior being tested in mind.

## Import, inspect and export

1. Start the backend and visualizer, then open **Simulation**.
2. In **Scenario files**, choose **Import JSON** and select one of these files.
   Import sends the original file text to backend validation before browser JSON
   parsing. A successful import replaces the draft; an error preserves it.
3. Review the form and the **Native scenario** preview. **Validate draft** refreshes
   validation and the **Effective configuration** preview without starting a run.
4. Click the single **Start simulation** button when ready. Import, validation
   and preset selection never start an experiment.
5. **Export JSON** downloads the current native draft, including edits, profiles,
   groups and individual overrides. It exports the draft rather than the active
   run's original document or the resolved preview.

Files must contain one complete JSON scenario and fit within **2 MiB of UTF-8
bytes**. Correct invalid form inputs before validating or exporting. The native
document is the format for import, export and Start. Effective configuration is
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

Keep the exported native document, seed, code revision and Go/model version.
If runtime commands are used, replay them at the same virtual times. Compare
virtual results such as completions, simulated lifetimes and per-hop counters.
Wall time and packets/events per real second vary with the machine, background
load, pacing and snapshot work; the same seed does not make those timings equal.

Access-link queues, the two LB input FIFOs and per-endpoint output queues have
separate drop scopes. A tail drop happens when waiting capacity is full; random
loss happens after transmission and still consumes that hop's bandwidth. Delivery
and drop totals count physical-hop events, including retries, rather than unique
application packets. Work still queued, transmitting or travelling at the horizon
is unfinished work, rather than an extra recorded loss.

The current transport sends one segment at a time and waits for its ACK, with
fixed retry timers. Reverse ACK paths also share LB resources. Adding configured
client bitrates therefore does not guarantee saturation or measured throughput.
These queue/loss results describe this model; congestion control and kernel RX
processing are not modeled.
