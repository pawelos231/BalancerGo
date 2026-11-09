# Version 2 scenario contract examples

These are **contract examples, NOT RUNNABLE until E1.13**. They describe the
planned timeline format; the CLI, HTTP runtime and frontend still use version 1.
They live outside `examples/scenarios/*.json` so existing presets and tests do
not discover them as runnable scenarios.

`scenario.DecodeDocumentV2` and `scenario.EncodeDocumentV2` check document syntax.
`scenario.NormalizeV2` additionally validates the whole timeline and resolves
each state independently. The frontend has matching types and local validation.
Successful validation
does not enable execution; timeline transitions are implemented in later phases.

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
irrelevant; normalization orders states numerically. Integer field values
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

The document reader checks version 2, strict JSON shape/field types, unknown and
duplicate fields, canonical timestamp spelling, and exact integer transport.
Ordinary omitted fields keep zero values. Call `NormalizeV2` after decoding to
check semantic validity; the same checks also cover directly constructed Go values.

Normalization requires positive duration, a state at zero, and state times strictly
below duration. It checks setting ranges, enum values, profile references, duplicate
IDs/endpoints, retained identities, insertion times and run-wide client reservations.
Equivalent IP spellings and ASCII host casing identify the same backend endpoint.
Other Unicode characters remain literal and case-sensitive; this v2 policy avoids
depending on the browser's or Go compiler's Unicode version. The original address
spelling is preserved. A later error rejects the whole timeline,
without returning partially usable states or creating runtime objects.

`NormalizedV2.Fixed` holds shared resolved settings; `States` contains full states
sorted by `At`, not compiled transition actions. State `TotalClients` sums its
declared group budgets. Run-wide `TotalClients` reserves the sum of each distinct
group's maximum declared count, bounded by `MaxClients`. Reducing a count or
removing a group does not release reservations, including slots never born.
This conservative rule matches the current address reservation model.

An override may address any slot below the group's largest count declared **up
to that state**. For count 10 -> 1, override index 9 remains valid for a retained
client; increasing to 10 later does not make an earlier out-of-range override valid.
These are reserved slots, not proof that a client has already been born.
`NormalizedClientGroup.Client(index)` still looks up clients within the current
birth budget; later runtime updates must also handle existing clients beyond it.

Client start times beyond the run duration and large arrival gaps are allowed;
they may produce no arrivals during the run. Generator progress, overflow-safe
replanning belongs to E1.12; state differences are prepared by the E1.4 compiler.

## Compiled transitions and configuration lookup

`scenario.CompileV2(input)` first validates and normalizes the entire document.
It returns a private `CompiledV2` plan or an error, without creating a world,
clients or engine events. An invalid later state returns no partially usable plan.

The plan exposes:

- `Fixed()` for shared TCP, execution and default settings.
- `TotalClients()` for the run-wide reserved client slots.
- `StateAt(t)` for a full desired configuration, with `0 <= t <= duration`.
- `Transitions()` for nonempty changes after the initial state at zero.

All returned collections are independent copies, including individual client
overrides and transition payloads. `StateAt` selects the latest declaration at
or before the query. For `burst-and-recovery.json`, a query at 4 s returns the
state declared at 2 s; its `At` stays 2 s. Querying the terminal time returns the
last declared configuration. This is a planned configuration, not live traffic
or a prediction of clients still connected at that time.

Transitions compare backends and groups by stable ID and sort each operation list
by ID. Changing declaration order or equivalent backend endpoint spelling creates
no runtime update. Sparse overrides are compared by index without expanding groups
into individual clients. Identical declarations and redundant default-equivalent
overrides need no execution event, but remain visible through `StateAt`.
Other differences in resolved settings are preserved; the compiler does not try
to prove every pair of configurations behaviorally equivalent.

Each transition contains optional LB changes, added/updated/removed backends and
added/updated/stopped generators. Component flags distinguish links, CPU, policy
and metadata edits. A group's `ReplanArrivals` is true only when `Count` or
`ClientGap` changes. `LinksChanged` includes group and individual client links;
`StreamChanged` concerns future clients. Stopping a generator does not remove its
existing clients, and removing a backend does not instantly clear remote TCP state.

E1.5 supplies atomic execution; later stages add individual resource handlers.
Address ranges, actual generator progress and birth-time arithmetic remain E1.12;
their reservation must use each group's maximum count across the entire run.
CLI, HTTP and the current frontend still reject or do not submit runnable v2.

## Internal execution boundary (E1.5)

`simulation.NewTimeline(plan)` prepares an internal world only after checking the
entire compiled plan. E1.5 introduced backend display-name changes; E1.6 added
backend link edits, E1.7 added shared LB ingress and forwarding limits, E1.8 added
mutable LB/backend CPU resources, E1.9 added LB algorithms and mapping timeouts,
E1.10 added backend insertion/removal with insertion-time resource lifetimes,
and E1.11 adds physical failure/recovery and administrative drain.
This allows real execution and publication tests without silently ignoring
unfinished generator changes. Generator transitions still return
`ErrUnsupportedTimelineChange` before any world is constructed.

One `engine.ScheduleConfigurationAt` callback applies all prepared edits at a
timestamp, then installs the configuration and increments its commit counter once.
It has priority over ordinary events at the same timestamp. A callback runs to
completion on the engine owner; event-budget pauses cannot split it, and it adds
no virtual delay. Preparation and checks happen before execution, not inside the
commit. This is not a general rollback mechanism for arbitrary user callbacks.

The engine preserves ordinary FIFO ordering and rejects retroactive configuration
insertion after ordinary work at that timestamp has started, even after a pause.
All compiled changes are registered before the internal world begins advancing.

`CurrentConfiguration()` returns an independent copy of the last committed state;
future states never become visible merely because they exist in the plan.
`Info.ConfigurationRevision` counts world commits, independently of wire topology
revisions. A new commit invalidates cached definitions, including retained clients
and links. The next successful projection builds one complete document alongside
the first snapshot using it. Repeated reads reuse that document; previously returned
documents remain unchanged. If several commits occur between samples, the next
sample describes the latest complete state.

Manual `SetAlgorithm` and `SpawnClients` are blocked for timeline worlds. Stop
still clears pending events, including future configuration commits. CLI/HTTP
admission remains deferred to E1.13 and frontend migration to E1.14.

## Mutable directed links (E1.6)

`Link.PrepareConfig(config, congestionRNG)` checks a future edit without changing
the link. Its prepared `Apply()` installs the settings without resetting queues,
statistics or random streams. The timeline binds both backend link directions
before advancing the engine and applies them with the rest of the timestamp's
configuration commit. Link IDs and endpoint objects stay unchanged, so the next
topology definition reports the new limits for the same paths.

A packet already serializing keeps its starting bandwidth, loss rate and travel
time. A packet already travelling keeps its scheduled arrival. Waiting packets
use settings current when serialization starts. After a sufficiently large rate
reduction, admitted work may remain transmitting beyond the run's horizon.

Queue shrinkage retains admitted packets; new waiting arrivals are rejected while
the queue is full. This also applies to managed LB output FIFOs. ECN marking and
early-drop decisions are made on admission, so queue-policy edits affect subsequent
arrivals without re-marking already admitted packets. A newly enabled probabilistic
policy receives a separate seeded stream; disabling and re-enabling it resumes
that stream rather than resetting it.

Client-group link updates are integrated with generator changes in E1.12.

## Shared LB network limits (E1.7)

The internal timeline can change the shared ingress link's bandwidth, travel,
loss and queue policy, including ECN signaling. Both input FIFOs use that one
physical link, with a separate waiting capacity for each input. Shrinking the
capacity keeps admitted packets and rejects new waiting work while a FIFO is full.
Active transmission and travel keep their starting settings; queued packets use
the settings current when their transmission starts.

All LB outputs share the forwarding limit. A rate edit preserves the budget
reservation already in progress. Each later dispatch reserves its packet's cost
at the current rate, preserving FIFO order and historical accounting. A large
rate cut can reserve the remaining horizon, including when the new duration
would exceed the duration type's range. Restoring the rate does not shorten that
existing reservation.

Ingress, forwarding and backend edits at one timestamp are prepared before the
run and applied in one configuration event. Applying settings processes no packet
handlers. A fresh topology definition publishes the current shared limits and
input capacities under the same resource IDs before the first snapshot using them.
CLI and HTTP execution of version 2 remains deferred to E1.13.

## Mutable LB mapping policy (E1.9)

The internal timeline can switch between `roundRobin` and seeded `random` for new
mappings. Existing flows keep their backend through later packets and retries.
An unchanged algorithm preserves its selector state. Random selection always uses
the world's algorithm stream; switching away and back resumes the same sequence.
Neither unrelated settings edits nor configuration reads reset or consume it.

All four mapping idle limits can change together with CPU and network settings.
The new limits apply to existing and future mappings at the next normal five-second
cleanup sweep, including a sweep at the transition's timestamp. Each mapping keeps
its original last activity and phase. A settings edit does not expire a mapping
immediately, close an endpoint or reroute a flow. Zero disables that phase's cleanup.

Algorithm objects and checked timeout settings are prepared before execution.
The configuration event installs them with the rest of its state, then publishes
one revision. Current configuration, algorithm metadata and refreshed topology
definitions therefore describe the same complete state. CLI/HTTP admission remains
deferred to E1.13.

## Physical availability and administrative drain (E1.11)

Each full-state backend declaration has independent `physicalAvailable` and
`administrativeState` settings. Omission resolves to `true` and `active` in that
state; it does not retain an earlier `false` or `drain`. Initial backends and
backends inserted later follow the same rules.

For example, set `physicalAvailable: false` to crash a backend. Its local TCP
connections, protocol timers, queued CPU packets and active CPU work are cleared.
Its CPU resource remains recoverable with its current settings and cumulative
counters. The links keep their queues and packets already emitted on the return
path may still arrive. Clients and LB mappings retain their independent state;
an old mapping never migrates to another backend.

The LB initially assumes a registered backend is healthy. It learns failure only
when a probe times out, so new mappings may still select the crashed backend
before that timeout. Restoring `physicalAvailable: true` permits local work again;
it neither clears drain nor changes observed health. A later successful probe
restores eligibility when administrative policy also allows it. Probes currently
use the existing ideal path without consuming network capacity.

Set `administrativeState: "drain"` to exclude a backend from new mappings while
keeping pinned traffic and probes working. A first SYN already assigned before
drain, retransmitted SYNs and established-flow packets still reach the backend.
Set the state to `active` to allow new selection subject to LB-observed health.
Selection uses the LB's registered capacity and own mapping count, rather than
the backend's hidden physical state or local TCP counters.

Availability and admission edits can share one atomic commit with membership,
names, CPU, network and mapping policy. Generic backend metrics expose physical
availability, administrative drain and LB-observed health separately, including
the interval where a crashed backend is still observed healthy. Existing v1
server fields and frontend hydration remain compatible until the v2 UI migration.

## Frontend validation

`visualizer/src/scenarios/v2.ts` exposes two entry points:

- `parseScenarioV2(text)` checks raw JSON before a browser can round number tokens
  or replace duplicate keys. Use it for imported documents.
- `validateScenarioV2(input)` checks an existing object and returns a detached,
  typed document. Large integers in an existing object must already be strings;
  validation cannot recover digits lost before it was called.

Both check the complete timeline using the backend's limits and identity rules.
They fill ordinary missing fields with Go zero values and preserve optional
null/zero distinctions. TCP, link, CPU and stream defaults are considered when
validating dependencies; the returned document is not a resolved runtime snapshot.
Timestamp sorting and integer arithmetic use `bigint` internally, while the DTO
retains JSON-compatible numbers and decimal strings. Invalid Unicode surrogate
escapes are replaced with U+FFFD, matching Go's JSON reader.

The existing form, file import and Start still use v1. This module is the v2
validation foundation; runtime admission follows in E1.13 and UI migration in E1.14.

`validation/cases.json` supplies the same valid and invalid inputs to Go and
TypeScript tests. Each case edits a fresh copy of `static.json`; optional text
replacements preserve ambiguous raw tokens such as duplicate keys and exponents.
Negative cases also check the intended error field. Add shared cases whenever
a rule changes so the two implementations remain aligned.
