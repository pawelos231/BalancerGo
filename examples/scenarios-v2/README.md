# Version 2 scenario contract examples

These are **contract examples, NOT RUNNABLE until E1.13**. They describe the
planned timeline format; the CLI, HTTP runtime and frontend still use version 1.
They live outside `examples/scenarios/*.json` so existing presets and tests do
not discover them as runnable scenarios.

`scenario.DecodeDocumentV2` and `scenario.EncodeDocumentV2` currently check
document syntax only. E1.2 will add semantic validation and normalization;
E1.3 will add frontend types. Successful decoding does not mean a document is
valid for simulation or that any timeline transition has been implemented.

| File | Intended experiment | Target application payload |
| --- | --- | --- |
| [static.json](static.json) | One state, two backends, four clients born 250 ms apart. | 4 × 262,144 B = 1 MiB. |
| [burst-and-recovery.json](burst-and-recovery.json) | Eight baseline clients; at 2 s add a 16-client burst, reduce shared LB limits to 5 Mbit/s and drain backend 2; at 6 s restore capacity and admission. | 24 × 1,048,576 B = 24 MiB. |

Targets exclude protocol headers and retransmissions; completion before the
horizon is not guaranteed. Both examples explicitly set execution, TCP, link,
host, stream and mapping-timeout settings. Empty endpoint/stream patches inherit
the document's fixed defaults, rather than undocumented values from another state.

## Document shape and units

| JSON path | Contents |
| --- | --- |
| `version` | Exactly `2`. |
| `fixed.execution` | Duration, real-time snapshot interval, seed, playback speed and event limit. |
| `fixed.tcp` | Run-wide TCP parameters, timers, receive buffer and congestion control. |
| `fixed.loadBalancer` | Immutable `id` only. |
| `fixed.network` | Link `defaults` and optional named `profiles`. |
| `fixed.workload.defaults` | Default stream `packets`, `bytes` and `packetGapNanoseconds`. |
| `states` | Object keyed by absolute virtual nanoseconds, e.g. `"0"`, `"2000000000"`. |
| `states[t].resources.loadBalancer` | `algorithm`, `host`, `ingress`, `forwardingBitsPerSecond`, `mappingTimeouts`; no `id`. |
| `states[t].resources.backends` | Complete backend list: `id`, optional `name`, `address`, `port`, `network`, `host`, optional `physicalAvailable` and `administrativeState`. |
| `states[t].workload.clientGroups` | Complete generator list: `id`, `count`, `startAtNanoseconds`, `clientGapNanoseconds`, `network`, `stream`, optional indexed `overrides`. |

`mappingTimeouts` contains `halfOpenIdleTimeoutNanoseconds`,
`establishedIdleTimeoutNanoseconds`, `closingIdleTimeoutNanoseconds` and
`resetIdleTimeoutNanoseconds`. Omission selects a default; explicit zero disables
that mapping phase's idle cleanup. Backend `physicalAvailable` is an optional
boolean; `administrativeState` is an optional enum, `"active"` or `"drain"`.
Omission or null selects `physicalAvailable: true` and `administrativeState: "active"`
for that state. These fields never inherit false or drain from a preceding state;
the LB's separately observed health is not reset by these defaults.

All `*Nanoseconds` values use exact nanoseconds; 1 s is `1000000000`.
Link bandwidth and forwarding limits are decimal bit/s. Queue capacities count
waiting packet slots, excluding active service; LB ingress has one capacity per
input FIFO. `bytes` is payload per client and overrides `packets`; `bytes: -1`
means `packets * 1024`. `speed: 0` disables intentional real-time pacing.

Endpoint links resolve from fixed defaults, then an optional profile, then
`toLoadBalancer` / `fromLoadBalancer` patches. Individual client overrides apply
last. For every endpoint, these directions mean endpoint-to-LB and LB-to-endpoint.
Stream patches similarly resolve from fixed stream defaults. An omitted patch
field inherits; an explicit zero sets zero where that field permits it.

State keys must be canonical nonnegative decimal `int64` strings: `"0"` is valid;
`"00"`, `"+1"`, fractions and exponent notation are invalid. JSON object order is
irrelevant; the eventual runtime orders states numerically. Integer field values
within JavaScript's safe range may be numbers; outside ±9,007,199,254,740,991 they
must be decimal strings. The reader enforces each destination integer's range.

## Full configurations, not patches or checkpoints

A state is the complete desired configuration from its timestamp until the next
state. Resolve it independently against `fixed`, never against the preceding
state. Repeat retained backends and groups; omission removes them. This makes a
state inspectable on its own and prevents a hidden chain of inherited edits.
Nested link and stream patches still inherit from fixed defaults as described above.

A state does not serialize live TCP connections, sequence numbers, congestion
windows, retransmission timers, queues or in-flight packets. Those remain runtime
state across a transition. This is configuration scheduling, not TCP-state replay
or a checkpoint/resume format.

## Agreed runtime rules for future implementation

The following rules are the contract for later phases, not claims about current
runtime support.

- `fixed`, including the LB ID, is immutable throughout a run. A retained backend
  keeps its ID, address, port and role; its display `name` may change. IDs cannot
  be reused after removal or moved to another role. Renaming an ID is delete/add.
- A retained client group's identity is `(id, startAtNanoseconds)`; its start time
  is absolute and cannot change. A newly inserted group must start at or after
  its insertion timestamp. Existing groups retain their birth progress.
- `count` is a lifetime birth budget, not an active-client target. If the new
  count is at or below the number already born, pause future births without
  removing clients. Increasing it resumes at the next unused client index.
  Removing a group stops only future births; existing clients continue.
- Apply the full state before replanning. Replan pending births only when a
  retained group's birth budget or gap changes. With a prior birth, the next
  eligible time is `max(stateTime, lastBornAt + newGap)`. If no client has been born,
  use `max(stateTime, startAtNanoseconds)`; increasing a zero budget later therefore
  cannot schedule that first client in the past. A zero gap explicitly requests a burst.
  Never catch up historical births that would have occurred before the edit.
- Group network changes update links for existing and future clients. Stream
  changes affect future clients only; every existing client's stream is frozen
  at birth, including its inherited values and individual override.
- Serialization, propagation and CPU jobs already in progress keep the settings
  captured when they started. Queued work uses current settings when dispatched.
  Shrinking a queue retains admitted work and rejects new arrivals while full.
- CPU core reductions let active jobs finish. No new job starts while active jobs
  already occupy the new core budget. Retiring busy capacity still contributes
  to the core-time integral.
  Setting CPU cost to zero lets new work, including waiting work after the state
  commit, bypass CPU service; old active jobs finish. Preserve historical counters.
- An LB algorithm change affects new mappings only. New mapping timeouts apply
  to all mappings at the next cleanup, preserving each mapping's `lastActivity`.
- `physicalAvailable: false` makes a backend unresponsive while retaining its
  membership. Observed health is runtime knowledge, not configuration. `"drain"`
  excludes new connections while existing connections continue.
- Removing an entity discards work local to that entity; remote connections do
  not vanish instantly. Explicit backend removal and drain update the LB's
  administrative selection set. Physical unavailability does not automatically
  inform the LB: transport and configured observation mechanisms must reveal it.

In `burst-and-recovery.json`, the retained baseline group's eight births are not
repeated at 2 s. The new burst group starts at its insertion time with gap zero.
Its 16-client budget is repeated at 6 s, so recovery does not create another burst.
Backend 2 remains physically available throughout its temporary administrative drain.

## Validation boundary

The current reader checks version 2, strict JSON shape/field types, unknown and
duplicate fields, canonical timestamp spelling, and exact integer transport.
Ordinary omitted fields keep zero values. It does **not** enforce a state at zero,
state times below the duration, numeric setting ranges, valid enum values,
profile references, stable identities, insertion rules or client-count budgets.
Those semantic checks belong to E1.2. The examples include a complete state at
zero and transitions strictly inside the horizon so they fit that planned contract.
