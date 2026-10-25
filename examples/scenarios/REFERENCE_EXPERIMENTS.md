# Reference experiments

These eight version-1 scenarios form a small comparison matrix for the L4
simulator. Find them in the frontend under **Reference experiments**, or import
the JSON files directly. Each preset builds the same document regardless of
the backend's CLI defaults.

Backend 1 is the fixed reference. Backend 2 varies in base travel RTT and
return-link bandwidth. Stream size changes for every client in a scenario.
Comparing two rows that differ in one column isolates that configured factor.

| JSON file | Backend 2 base travel RTT | Backend 2 return link | Payload per client |
| --- | --- | --- | --- |
| [reference-near-fast-ack-short.json](reference-near-fast-ack-short.json) | 4 ms | 1 Gbit/s | 16 KiB |
| [reference-near-fast-ack-long.json](reference-near-fast-ack-long.json) | 4 ms | 1 Gbit/s | 1 MiB |
| [reference-near-slow-ack-short.json](reference-near-slow-ack-short.json) | 4 ms | 64 kbit/s | 16 KiB |
| [reference-near-slow-ack-long.json](reference-near-slow-ack-long.json) | 4 ms | 64 kbit/s | 1 MiB |
| [reference-far-fast-ack-short.json](reference-far-fast-ack-short.json) | 40 ms | 1 Gbit/s | 16 KiB |
| [reference-far-fast-ack-long.json](reference-far-fast-ack-long.json) | 40 ms | 1 Gbit/s | 1 MiB |
| [reference-far-slow-ack-short.json](reference-far-slow-ack-short.json) | 40 ms | 64 kbit/s | 16 KiB |
| [reference-far-slow-ack-long.json](reference-far-slow-ack-long.json) | 40 ms | 64 kbit/s | 1 MiB |

## Fixed settings

- Two backends, 16 simultaneous clients, one connection and one stream per client.
- Round robin assigns eight clients to each backend; existing connections stay pinned.
- Seed 1, 60 virtual seconds, speed 1, 100 ms snapshots and a 50-million-event limit.
- Client links and LB-to-backend links at 1 Gbit/s.
- Independent shared LB ingress and forwarding limits of 1 Gbit/s each.
- Zero random loss, 1024 waiting slots per queue and no client or segment pauses.
- Backend 1 has 1 Gbit/s links in both directions and 1 ms travel time per direction.
- All client directions have 1 ms travel time; shared ingress adds zero travel time.
- The present packet handlers have zero modeled CPU cost.

For an unpaced comparison, set **speed to 0 in every compared document**.
Keep the remaining execution settings and native documents the same. The JSON
files intentionally retain speed 1 for inspection in the topology view.

## RTT and the return path

The matrix's base travel RTT is the sum of the four directed endpoint travel
times on the client-to-backend round trip:

```text
client -> LB -> backend -> LB -> client
```

Backend 1 contributes 1 ms in each direction, giving 1 + 1 + 1 + 1 = 4 ms.
Backend 2 contributes either 1 ms or 19 ms per direction, giving 4 ms or
1 + 19 + 19 + 1 = 40 ms. Actual RTT also includes packet transmission,
waiting in queues and the shared LB budgets; it is not fixed to those values.

Only backend 2's **backend-to-LB** bandwidth changes in the slow-ACK cases.
That link carries SYN-ACK, data ACK and FIN-ACK packets. Its opposite direction,
the client links and backend 1 stay fast. There is no artificial ACK timer.

## What to compare

Use the near/fast cases as the symmetric baseline. Compare near with far to
study delay, fast-ACK with slow-ACK to study the return bottleneck, and short
with long to separate handshake/closure overhead from a longer transfer.
These fixtures can later be replayed with other policies and transport profiles.

The current transport sends at most 1024 payload bytes before waiting for an ACK.
Changing configured bandwidth therefore does not imply a proportional change
in data throughput. Short streams may finish before the first 100 ms snapshot;
the long variants give more time to observe traffic. The 60-second horizon is
fixed across the matrix; always include unfinished streams in comparisons.

Current `LatencyP95Milli` is SYN-to-FIN connection lifetime, not RTT or application
response time. Completed-payload totals exclude unfinished streams; link counters
include headers, ACKs and retransmissions. The planned metrics work will provide
more precise transfer measurements. These are reference inputs for the present
model, not calibrated predictions of Linux or IPVS performance.
