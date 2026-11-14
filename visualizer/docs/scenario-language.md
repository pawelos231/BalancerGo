# Scenario language, version 1

The frontend compiles this declarative language to the existing
[scenario JSON v2](../../examples/scenarios/CONTRACT.md). It describes resources,
traffic and prepared changes in virtual time. It does not change the simulator's
TCP, routing or event execution. The Go CLI accepts the exported JSON; it does
not parse `.lb` source. Language version 1 and scenario version 2 are independent.

**Implemented:** static resources, TCP/CPU/link settings, profiles, compact client
groups, individual overrides and the timeline commands in this reference. The
CodeMirror editor supports highlighting, located diagnostics, retained drafts,
`.lb` import/export and Compile and apply. Compilation and native validation
complete before the applied JSON and planned topology are replaced.

## Quick start

```text
language 1
run 180s seed 2333 speed 1
defaults link 1GB travel 1ms queue 1024
tcp {
    congestion reno
    mss 1460B
    SACK false
    ECN false
}

balancer lb {
    algo rr
    ingress 1GB
    egress 1GB
}

backend api-0 {
    link 250MB
}

clients regular {
    count 2
    payload 10KB
    every 10ms
    link 10MB
}
```

This declares one LB, one backend and two clients, each sending 10,000 payload
bytes. `1GB` in a link field
means 1,000,000,000 bytes per second, emitted as 8,000,000,000 bits per second.
`egress` is the LB's shared forwarding budget, not another physical link.
The checked source/JSON pair is [transfer.lb](../../examples/language/transfer.lb)
and [transfer.json](../../examples/language/transfer.json); the optional language
line is shown here explicitly. [basic.lb](../../examples/language/basic.lb) is a
smaller example without clients.
`SACK` and `ECN` default to `false`; set either flag to `true` to enable it.

1. Open Simulation, choose **Language** in Scenario configuration, then paste
   source or choose **Import source**.
2. Click **Compile and apply**. Fix any located errors; failure preserves the
   previous applied configuration.
3. Inspect **Planned topology** at the declared timestamps. It shows configured
   state, without predicting traffic or measured throughput.
4. Use **Start simulation**, or switch to JSON and **Export JSON** for Go CLI.
   Loading or compiling source never starts a run.

## Source and grammar

The frontend TypeScript module lives in `src/scenario-language/`. It has no
React or editor dependency. The current language and default set are version 1;
changing their meaning requires a new documented version. An optional first
declaration `language 1` pins that version; omission means version 1. Leading
comments and blank lines are allowed. Other versions
are errors, never silently interpreted as the current one.

- Files use UTF-8 with extension `.lb`. LF and CRLF have the same meaning.
- A declaration ends at a newline or closing brace. An opening brace must be on
  its declaration's line. Fields are separated by whitespace, not commas or colons.
- `#` starts a comment outside a quoted string, lasting until the newline.
- Double-quoted strings use JSON escapes. A literal newline inside a string is
  invalid. Strings are for display names, not resource identifiers or units.
- Keywords and names are case-sensitive. Keywords are lowercase except for the
  TCP feature fields `SACK` and `ECN`; units keep their documented case. A field
  may appear only once in its containing block.
- LB, profile and backend-prefix identifiers use lowercase letters and internal hyphens. Individual endpoint
  names append a canonical decimal index, for example `api-0` or `web-api-12`.
  Client-group segments after a hyphen may also contain digits, as in `wave-20s`;
  its first client is `wave-20s-0`. Empty components and leading endpoint index zeroes are errors.
- Values are decimal integers or fractions, optionally negative where allowed;
  numeric literals have at most 128 characters to bound exact conversion work.
  Exponents, separators and leading `+` are unsupported. Booleans are `true`
  and `false`; `null` is allowed only for an existing nullable patch field.
- Blocks can nest, for example `host`, `to-lb`, `from-lb`, `rto`, `close`,
  `delayed-ack`, `congestion-control` and `mapping-timeouts`.

The compiler rejects unknown declarations and fields. There are no variables,
loops, arbitrary expressions or embedded JavaScript.

## Units and exact numbers

| Context | Accepted units | Meaning |
| --- | --- | --- |
| Payload, MSS, receive buffer, ssthresh | B, KB, MB, GB, TB, PB, EB | Decimal bytes, powers of 1000 |
| Link or forwarding rate | B, KB, MB, GB, TB, PB, EB | Decimal bytes **per second** |
| Time | ns, us, ms, s, m | Exact nanoseconds; m is 60 seconds |
| Loss or signal probability | % | Percentage; `1%` becomes 0.01 |
| ECN threshold | % | Queue occupancy percentage; `80%` becomes 80 |
| Counts, seed, speed | No suffix | Count, exact seed or playback multiplier |
| Initial / maximum congestion window | No suffix | Number of MSS-sized segments |

Conversions to integer native fields must be exact: `0.5ms` is valid;
`0.1ns` is invalid. Field limits still apply after conversion. In particular,
`1EB` of bandwidth fits int64 after multiplying by eight, but `2EB` does not.
`GiB`, `Mbps` and `Gbps` are unsupported. A large integer is emitted as a
decimal string whenever it exceeds JavaScript's safe integer range. Calculations
use exact integer arithmetic before selecting the JSON representation.

## Static declarations and defaults

Exactly one `run` and one `balancer` are required. Optional `language`, `defaults`
and `tcp` declarations appear at most once. Initial declarations precede `at`.
Profiles may be declared before or after the resources that reference them.
Exactly one LB is supported. Unless explicitly supplied, backend addresses and
port 80 are generated deterministically before compilation completes; they are
not randomized by the seed.

`algo rr` maps to `roundRobin`; `algo ran` maps to `random`. Future algorithms,
`mss auto`, additional LBs and stochastic profiles are errors.

The version-1 defaults are independent of the running backend and its CLI flags:

| Setting | Default |
| --- | --- |
| Seed / speed | 1 / 1 |
| Snapshot interval / maximum events | 100ms of real time / 5,000,000,000 |
| Network bandwidth / travel / queue / loss | 125MB/s / 1ms / 1024 waiting packets / 0% |
| LB ingress travel / algorithm | 0ns / rr |
| LB ingress and forwarding rates | Network default bandwidth |
| Host cores / packet time / queue | 1 / 0ns (bypass) / 1024 |
| Backend availability / administration | true / active |
| Stream packets / payload / segment gap | 10,000 / -1 (packets × 1024 bytes) / 0ns |
| Client count / start / birth gap | 1 / 0ns / 0ns |
| MSS / receive buffer | 1024B / 65,535B |
| Congestion mode / fast retransmit / CUBIC fast convergence | reno / true / true |
| Initial / maximum window | 4 segments / 0 (no explicit cap) |
| Initial slow-start threshold | 65,535B |
| RTO mode / backoff / initial / minimum / maximum | adaptive / true / 1s / 200ms / 60s |
| SACK / ECN / delayed ACK | false / false / false |
| Delayed ACK threshold / timer | 2 segments / 40ms |
| MSL / FIN wait / FIN retries | 30s / 60s / 5 |
| Client / backend half-open / backend established idle | 20s / 5s / 0ns |
| Mapping half-open / established / closing / reset idle | 5s / 50s / 120s / 10s |
| Network queue policy / ECN threshold / signal probability | tail-drop / 0% / 100% |

`link` sets both endpoint directions. `to-lb` and `from-lb` override only that
direction. `host.queue` counts waiting CPU work; network `queue` counts waiting
packets. Active transmission and packet travel do not occupy queue slots.

## Resources, inheritance and identity

### Execution and top-level fields

| Declaration / field | Meaning and limits |
| --- | --- |
| `language 1` | Optional first declaration; only version 1 is supported |
| `run duration` | Required positive virtual duration |
| `run.seed` | Signed int64, -9,223,372,036,854,775,808 through 9,223,372,036,854,775,807 |
| `run.speed` | Finite nonnegative playback multiplier; 0 runs without wall-clock pacing |
| `run.snapshots` | Positive **real-time** interval between UI observations |
| `run.max-events` | 1 through 18,446,744,073,709,551,615 DES events |
| `defaults` | Network fields below plus `payload`, `packets`, `segment-gap`; fixed for the run |
| `profile name` | Named network patch, with the network fields below; fixed for the run |
| `tcp` | Fixed transport settings; see TCP fields |

Times fit a signed int64 number of nanoseconds. Positive speed must also keep
the paced duration within that range. `speed` changes wall-clock playback, not
virtual transmission/CPU cost. Seed controls the model's existing random streams
(routing, link loss and probabilistic queue signaling), not generated addresses.

### Network, host and balancer fields

The same network fields are accepted by `defaults`, `profile`, endpoint settings,
endpoint `to-lb`/`from-lb` blocks and the LB's `ingress` block:

| Field | Meaning and limits |
| --- | --- |
| `link` | Positive byte/s rate; after multiplying by eight, bit/s must fit int64 |
| `travel` | Nonnegative virtual packet travel time after transmission |
| `queue` | 0 through int64 maximum waiting packet slots; full queues drop arrivals |
| `loss` | Random loss probability, 0% through 100% |
| `queue-policy` | `tail-drop`, `threshold`, or `default` (native default policy spelling) |
| `ecn-threshold` | Waiting occupancy threshold, 0% through 100%; used by `threshold` |
| `signal-probability` | Probability of signaling above the threshold, 0% through 100% |

With `threshold`, waiting occupancy includes the incoming packet. Eligible ECN
packets can be CE-marked; packets without ECN capability can be dropped early.
Full queues still drop packets. `tail-drop` does not use the threshold or signal
probability. For example, `queue-policy threshold ecn-threshold 80%
signal-probability 100%` signals every eligible waiting arrival at that occupancy.
Threshold 0% is valid; it does not disable a selected threshold policy. Use
`tail-drop` or signal probability 0% to disable early signaling.
`default` emits the native empty policy name, which currently selects FIFO
tail-drop; it preserves native configuration snapshots that omit this field.

| Declaration / block | Fields |
| --- | --- |
| `balancer name` | `algo` (`rr`, `ran`), `ingress` (rate or network block), `egress` (rate), `host`, `mapping-timeouts` |
| `backend prefix-index` | Network settings, `use`, `to-lb`, `from-lb`, `name` (quoted display text), `address` (quoted host), `host`, `available`, `admin` |
| `backends prefix` | Same backend settings plus required `count` and optional `from`; see identities below |
| `host` | `cores` (1-1024), `packet-time` (nonnegative time), `queue` (0-1,000,000 waiting CPU packets) |
| `mapping-timeouts` | `half-open`, `established`, `closing`, `reset` (nonnegative time; zero disables cleanup) |
| `backend.available` | `true` / `false`: physical availability |
| `backend.admin` | `active` / `drain`: administrative admission |

`to-lb { ... }` and `from-lb { ... }` override those fields in one direction.
Directional settings take precedence regardless of textual order; explicit null
removes that directional patch and restores native inheritance.

For example, `host { cores 2 packet-time 10us queue 1024 }` models two workers
and 1024 waiting packets. Packet time zero bypasses CPU. Queue slots are separate
from network FIFOs. The native duration/resource-overflow check still applies:
duration times the relevant core/queue bound must fit int64. Nullable CPU and
mapping timeout leaves restore model defaults. `down` and `up` change only physical availability;
`drain` changes only administrative admission. Use `set backend api-0 admin active`
to leave drain. Health observed by the LB remains runtime state, not a language
field. No action removes the backend, terminates clients or migrates active TCP.

Network defaults apply to endpoint links. Only their bandwidth also supplies the
initial LB ingress and forwarding rates; other ingress settings retain their
separate pinned defaults. Ingress is one shared physical receiver for client and
backend arrivals. Egress controls the shared forwarding budget over separate
output FIFOs/links, not one FIFO per client group. These are configured limits,
not promised per-client throughput. No RSS/NAPI or full Linux stack is implied.

### TCP fields

All settings remain fixed for the run. Scalar TCP settings accept `null` to use
the existing model's default; explicit zero only works where the native field
permits it. Existing cross-field validation remains authoritative.

| Block | Fields and units |
| --- | --- |
| `tcp` | `congestion` (reno, newreno, tahoe, cubic, none), `mss`, `receive-buffer` (bytes), `SACK`, `ECN` (boolean, both default false) |
| `tcp` idle timers | `client-idle`, `backend-half-open-idle`, `backend-established-idle` (time; zero disables) |
| `congestion-control` | `mode`, `fast-retransmit`, `fast-convergence`, `initial-window`, `maximum-window` (segments), `ssthresh` (bytes) |
| `rto` | `mode` (fixed/adaptive), `backoff`, `initial`, `minimum`, `maximum` (positive time; minimum ≤ initial ≤ maximum) |
| `close` | `msl`, `fin-wait` (positive time), `fin-retries` (0–30) |
| `delayed-ack` | `enabled`, `segments` (1–2), `max-delay` (positive time, at most 500ms) |

MSS is 1–65,495 bytes and the receive buffer is 1–65,535 bytes. The initial
window is positive; maximum-window zero disables its explicit cap. The slow-start
threshold must be at least twice MSS. SACK requires Reno/NewReno/CUBIC with fast
retransmit. ECN requires congestion control. `none` defaults fast retransmit to
false, but an explicitly incompatible setting is rejected. `congestion` and
`congestion-control.mode` are aliases and cannot both be supplied.
`none` also forbids a nonzero maximum-window cap.

Initial and maximum windows cannot exceed `floor(int64 maximum / MSS)` segments.
The slow-start threshold cannot exceed int64 maximum bytes. MSL cannot exceed
half the int64 nanosecond range because TIME_WAIT lasts twice MSL. Other times
must fit int64. A high MSS may require raising the default slow-start threshold.
Packet sizes are manually configured; the language adds no MSS negotiation.

### Resource identities and workloads

| Declaration | Fields |
| --- | --- |
| `clients group` | `count`, `start`, `every`, `payload`, `packets`, `segment-gap`, network settings, `use`, `to-lb`, `from-lb` |
| `client group-index` | `payload`, `packets`, `segment-gap`, network settings, `use`, `to-lb`, `from-lb` |
| Stream `payload` | Nonnegative bytes per client, or `-1` for the packet-count fallback |
| Stream `packets` | Nonnegative count of 1024-byte payload units, used only when effective payload is -1 |
| Stream `segment-gap` | Nonnegative virtual delay between original TCP segment sends |
| Group `count` | 0-16,777,215 lifetime client slots, also bounded by the whole timeline's reservations |
| Group `start` | Nonnegative absolute virtual time; fixed once the group exists |
| Group `every` | Nonnegative virtual time between client births; zero schedules births together |

Explicit payload takes precedence over packet count; 0B is an empty transfer.
Effective payload must not exceed int64 maximum minus 40 bytes. Packet-count
fallback must fit that same bound after multiplication by 1024. CPU settings
belong to the LB and backends, not client generators. Births after the horizon
remain unstarted, so a group's configured count need not equal its created count.

See [transfer.lb](../../examples/language/transfer.lb) and its matching
[transfer.json](../../examples/language/transfer.json) for an executable workload.
`payload -1` restores the native `packets × 1024` fallback, independent of MSS.
Group stream fields accept `null` to inherit fixed workload defaults; count,
start and every are ordinary nonnegative fields. Count zero creates no new clients.

`backend api-0` declares one backend. `backends api { count 4 ... }` expands
into independent backends `api-0` through `api-3`; `from 10` starts at `api-10`.
It does not create a runtime backend group. `count` is required and positive;
`from` defaults to zero. Omit backend declarations for an empty initial pool.
Counts are checked before expansion
against the existing limit of 10,000 backends. Generated IDs must fit 128 bytes.

Backend addresses are assigned in lexical ID order across the **whole source**,
including later additions: `10.0.0.1` through `10.0.0.254`, then `10.0.1.1`
through `10.0.1.254`, continuing in the third octet, with port 80.
Each address belongs to its ID for the entire document. A retained backend's
address and port never change between states. Reordering declarations does not
change addresses; adding another ID may change the address allocation of a
separately compiled document, which is a different experiment.
An explicit declaration such as `backend api-0 { address "10.0.0.9" }` overrides
that backend's generated address. It is valid in initial declarations or `add`,
not `set`. Explicit and generated endpoints are checked together for collisions;
the compiler does not silently renumber other backends to avoid a collision.

`clients regular` declares a compact native client group. `regular-1` refers to
index 1 in that group; `client regular-1 { ... }` declares a sparse override.
It never allocates a second client. Runtime IDs remain `client/regular/1`.
Client aliases and explicit backend IDs must not collide. LB and group IDs also
share the native resource namespace; profile names have a separate namespace.
Client indices refer to historically reserved slots, not current active clients.
Total reserved slots across the timeline cannot exceed 16,777,215.
This total is the sum of each group's historical maximum count, not the current
number of active connections.

An initial `client regular-1 { ... }` accepts endpoint network settings, a profile,
`payload`, `packets` and `segment-gap`. It may precede its group's declaration.
It cannot change count, start, every, CPU settings or IDs. The slot must exist,
and only one initial declaration may override each slot. Group settings stay
compact and unchanged; the output uses native override entries with an `index`
field identifying the client slot, independently of the entry's array position.

Endpoint link precedence is defaults → selected profile → endpoint settings →
directional settings. A client override applies after its resolved group links.
Profiles describe network settings only; `use mobile` selects a declared profile.
Definitions can follow their first use. Select with a declaration header or a
`use mobile` body field; supplying both is a duplicate, not an override. An empty
profile can represent the unmodified global defaults. Profiles cannot select
other profiles or contain directional/CPU settings. Even unused definitions
are validated. The generated JSON retains native named profiles and patches.
Profile definitions cannot change inside `at`; endpoints may select an existing
profile there. Explicit zero overrides an inherited value. A nullable
patch's `null` means inherit, as in native JSON. Resetting a previous DSL override
to `null` removes that patch; it does not inherit the preceding state's value.

Only nullable scalar leaves accept `null`: TCP settings, CPU settings, mapping
timeouts, backend availability/admin, endpoint/profile link patches and
group/individual stream patches. Whole blocks, complete network defaults and LB
ingress settings do not. An individual stream null inherits its resolved group
stream; group stream null inherits fixed workload defaults. `use` takes a profile
name, not a null inheritance instruction or a quoted string. Select an empty
named profile to return to the unmodified profile base.

`count` is the group's lifetime birth budget, `every` the time between births,
and `payload` the total bytes per client. `segment-gap` controls new TCP segment
spacing separately. `start` is fixed for an existing group. Without an explicit
start, an initial group starts at zero and an added group starts at its `at` time.
Lowering count never kills existing clients or releases their reserved IDs.

## Timeline and atomic states

```text
at 30s {
    set balancer ingress 100MB
    down backend api-0
    add clients burst {
        count 10000
        payload 20MB
        every 10ms
    }
}

at 60s {
    up backend api-0
}

at 90s {
    stop arrivals burst
}
```

All initial declarations must precede the first `at`. Times must be strictly
increasing, unique and satisfy `0 < time < run duration`. They are converted
exactly to nanoseconds; `1s` and `1000ms` identify the same timestamp.
Each block produces **one complete native state** cloned from the preceding
state, then modified and validated. It is not a sequence of runtime callbacks.
The existing DES applies that prepared state atomically at its timestamp.

Commands use these forms:

| Form | Effect |
| --- | --- |
| `set balancer ingress 100MB` | Change a mutable LB setting |
| `set backend api-0 link 10MB` | Change an existing backend's settings |
| `set clients regular count 400` | Change a group's budget or other mutable setting |
| `set client regular-1 payload 20KB` | Change an individual override |
| `add backend api-2 { ... }` | Add a new independent backend |
| `add backends extra { count 2 ... }` | Add several independent backends |
| `add clients burst { ... }` | Add a new client generator |
| `down backend api-0` / `up backend api-0` | Change physical availability only |
| `drain backend api-0` | Prevent new mappings; retain existing mappings |
| `stop arrivals regular` | Set the group's birth budget to zero; keep existing clients |
| `remove clients regular` | Omit a generator from later states; keep existing clients and historical ID reservations |

Block settings can follow `set`, for example
`set backend api-0 { host { packet-time 10us } }`. `set backend api-0 admin active`
leaves drain without changing physical availability. `up` does not cancel drain.

Within one `at`, commands are read in source order: a resource must already exist
when referenced. `add` may be followed by `set` for that new resource: its
initializer establishes starting values, which a following setter may replace.
Conflict checks apply between subsequent mutations, not against that initializer. Writing
the same native leaf twice is an error, including overlapping shorthand writes:
`link` overlaps `to-lb.link`, and `down` overlaps `available`. Separate properties
of one resource may change in the same block. The final state is validated after
all commands, so temporary incomplete combinations are not observed by packets.

| Fixed throughout the run | Mutable in states |
| --- | --- |
| Run duration, seed, pacing, snapshots, event limit | LB algorithm, ingress, forwarding and mapping idle timeouts |
| TCP and default stream settings | Host cores, packet processing time and CPU queue |
| Global link defaults and profile definitions | Endpoint links and selected existing network profile |
| LB ID; backend ID/address/port; retained group ID/start | Backend display name, physical availability and drain |
| Resource role and historical identity reservations | Group count, birth gap, stream and sparse client overrides |

A backend failure does not instantly update health observed by the LB, notify
clients or reroute established TCP. Stopping arrivals does not terminate existing
connections. The language exposes the current simulator's behavior only.

`stop arrivals regular` writes count zero through the same field rules as `set`.
It retains the group's identity, start, links, stream and historical overrides.
It conflicts with another count mutation in the same `at`, and requires an
already existing group. The complete [timeline.lb](../../examples/language/timeline.lb)
now compiles to its checked [timeline.json](../../examples/language/timeline.json).
`remove clients regular` instead removes the group from the configuration and
planned topology. It stops future births without terminating already created
TCP connections. Later commands cannot refer to the removed group or reuse its
identity. Removal preserves the order and source locations of remaining groups.

## Source files

Use the button at the CodeMirror editor's top right, or press F7 in Language mode
on Simulation. Fullscreen places the source editor on the left, with Compile and
apply and a read-only applied scenario JSON preview on the right. The preview
uses the same native document as Start and JSON export. It updates after a
successful Apply; unfinished source or failed validation leaves the last applied
JSON visible, with a reminder to apply source changes. Other controls and help remain in the normal view;
inline error marks and hover messages still work. F7 toggles this workspace; Esc
exits fullscreen. Text, selection and undo history are preserved. On narrow
screens, the Apply button and JSON preview sit below the editor and scroll independently of the source.

The configuration panel has explicit JSON and Language formats. Both use one
applied native scenario. Pending edits block switching until applied or discarded.
Loading a preset compiles its canonical `.lb` source, validates the generated
JSON locally and on the backend, then replaces both buffers and opens Language.
Failed validation preserves the previous source, JSON and format. Importing JSON
selects JSON and preserves the source buffer. Returning to a source that no longer
matches the applied scenario shows a warning and
blocks Start/native export until Compile and apply or selecting JSON.
Neither mode reverse-translates JSON into source or reconstructs comments.

Import source accepts UTF-8 `.lb` files up to 2 MiB. It loads the text into the
source draft and runs the same local compiler for diagnostics. An unfinished
source remains editable; an unreadable, empty or oversized file leaves the old
draft unchanged. No import starts a run or replaces applied JSON. Apply or discard
pending edits before importing another file.

Export source saves the current text, including comments and unfinished edits,
without compiling it. Imported line endings are preserved until text is edited;
the editor may normalize them to LF on editing. Diagnostics use line/column
positions so CRLF files highlight the correct text too.

After Compile and apply, use the existing Export JSON action to save the native
scenario for Go CLI. JSON export and source export are separate formats; neither
requires a second compiler or a backend language endpoint.

The 37 library sources live in `src/scenarios/presets/sources/`; their matching
native exports are in `examples/scenarios/` at the repository root. Catalog tests
compile every source and compare its export. Backend regression tests compare
the complete resolved configuration and the native profile, link and stream
selectors against hashes captured from the original presets before migration.
No settings or identities are excluded from these comparisons.

## Invalid examples

While Language is active on Simulation, local checks run after 100 ms without
typing. A Web Worker uses the same compiler, including syntax, field names,
units and scenario dependencies. The first error is underlined in red; hover
shows its message. Missing input at the end of the file gets a point marker.
Edits clear the previous diagnostic, and responses for older drafts are ignored.
Live checking does not apply a configuration; Compile and apply still performs
the existing local and server validation.

Errors carry UTF-16 source offsets and one-based line/column positions. Syntax,
conversion and field errors retain their direct token ranges. Native cross-field
errors keep their JSON path and explanation, highlight the closest source
setting, and add a short fix suggestion when one is known. A native failure for
an omitted default points to its supplied containing declaration instead.

| Input | Why it fails |
| --- | --- |
| `at 30s { ... }` followed by `at 20s { ... }` | Timeline must be strictly increasing |
| Two `at 1s` blocks, including `at 1000ms` | One atomic block per timestamp |
| `at 180s` in a 180s run | State must precede the simulation end |
| `set tcp mss 1200B` inside `at` | TCP is fixed |
| Declaring `api-01` or `api2-0` | Noncanonical endpoint ID |
| Bulk `api` count 2 and explicit `api-1` | Duplicate expanded ID |
| `set backend absent-0 ...` | Unknown resource |
| Setting the same ingress bandwidth twice | Conflicting writes |
| Adding a group with `start` before its insertion time | Clients cannot be born in the past |

## Five experiment recipes

Each recipe has a checked `.lb` source and native JSON companion. They run for
6-8 virtual seconds with speed 0 so CLI checks finish without playback delays.
Change speed to 1 for live observation, or lengthen duration/payloads for longer
experiments. These are illustrative configurations, not calibrated hardware
profiles or guaranteed measurement outcomes.

| Recipe | Source / native JSON | What to inspect |
| --- | --- | --- |
| Burst and stop | [burst.lb](../../examples/language/burst.lb) / [JSON](../../examples/language/burst.json) | At 2s add staggered clients; at 4s stop future births while created clients continue |
| Backend failure and return | [backend-failure.lb](../../examples/language/backend-failure.lb) / [JSON](../../examples/language/backend-failure.json) | Physical failure at 3s and return at 5s; transport retries and observed health evolve separately |
| Slow backend | [slow-backend.lb](../../examples/language/slow-backend.lb) / [JSON](../../examples/language/slow-backend.json) | 1MB/s versus 100MB/s backend links; inspect queues and measured transfer rates |
| CPU bottleneck | [cpu-bottleneck.lb](../../examples/language/cpu-bottleneck.lb) / [JSON](../../examples/language/cpu-bottleneck.json) | Finite CPU queues with 10us LB and 50us backend processing costs; compare with packet-time 0ns |
| Asymmetric links | [asymmetric-links.lb](../../examples/language/asymmetric-links.lb) / [JSON](../../examples/language/asymmetric-links.json) | Different data/ACK path rates and travel times; `to-lb` is the backend's return direction |

For a longer combined timeline, see [timeline.lb](../../examples/language/timeline.lb)
and [timeline.json](../../examples/language/timeline.json). It combines a burst,
capacity change, failure/recovery and stopped arrivals. The language provides
ordinary settings and timed state changes; a burst needs no separate runtime
traffic mechanism.

To run an exported native JSON from the repository root:

```sh
cd simulator
go run . -scenario ../examples/language/burst.json -output ../burst-report.json
```

Report output must be a new file; the CLI refuses to overwrite existing reports.
Scenario file mode runs without playback pacing. The report records the scenario
and simulated outcomes; a finished run need not mean every transfer completed
before the horizon or every TCP connection left TIME_WAIT.

## Limits and extension boundaries

- Source and compiled native JSON each have a 2 MiB UTF-8 size limit. Complete
  state snapshots can make the output larger than the source.
- The lexer allows at most 200,000 tokens; setting blocks nest at most 64 levels.
  Numeric literals contain at most 128 characters. Resource identifiers are
  bounded by the native 128-byte limit.
- Compilation rejects the whole document on any invalid field, identity,
  timestamp, conflict or native cross-field dependency. No partial state is
  applied, and no validation is deferred into event execution.
- Unsupported constructs include backend removal, `delete`, dotted property syntax,
  mutable profiles/defaults/TCP, custom backend ports, client CPU,
  direct observed-health settings, multiple LBs and `mss auto`.
- New congestion algorithms, probability distributions and a full Linux host
  stack are separate future work. This language does not emulate them.

The compiler lives entirely in `src/scenario-language/`, with no React,
CodeMirror or backend dependency. The editor calls this one compiler, then the
existing JSON validation API. Start retains authoritative backend validation.
There is no reverse compiler, second Go/CLI parser or new runtime config schema.

The frontend test suite checks syntax, exact conversions, field dependencies,
source locations, fixtures and editor/apply behavior. From `visualizer/`, run
`npm test`, `npm run lint` and `npm run build`.
