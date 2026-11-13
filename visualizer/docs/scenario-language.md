# Scenario language, version 1

This is the specification for the frontend configuration language. Its compiler
will produce the existing scenario JSON v2. The Go CLI consumes that JSON; it
does not compile this language. No simulation behavior changes in this epic.

**Implementation status:** the structural lexer recognizes names, braces and
newlines with source positions. Numbers, strings, parsing, compilation and the
editor are not available yet. Examples are fixtures for later commits.

## Quick start

```text
run 180s seed 2333 speed 1
defaults link 1GB travel 1ms queue 1024
tcp congestion reno mss 1460B

balancer lb {
    algo rr
    ingress 1GB
    egress 1GB
}

backend api-0 {
    link 250MB
}
```

This declares one LB and one backend, with no clients yet. `1GB` in a link field
means 1,000,000,000 bytes per second, emitted as 8,000,000,000 bits per second.
`egress` is the LB's shared forwarding budget, not another physical link.
The matching files are [basic.lb](../../examples/language/basic.lb) and
[basic.json](../../examples/language/basic.json).
Expected output comparisons validate both JSON documents first: the native
decoder fills ordinary omitted fields such as empty display names and arrays.

## Source and grammar

The frontend TypeScript module lives in `src/scenario-language/`. It has no
React or editor dependency. The current language and default set are version 1;
changing their meaning requires a new documented version. An optional first
line `language 1` pins that version; omission means version 1. Other versions
are errors, never silently interpreted as the current one.

- Files use UTF-8 with extension `.lb`. LF and CRLF have the same meaning.
- A declaration ends at a newline or closing brace. An opening brace must be on
  its declaration's line. Fields are separated by whitespace, not commas or colons.
- `#` starts a comment outside a quoted string, lasting until the newline.
- Double-quoted strings use JSON escapes. A literal newline inside a string is
  invalid. Strings are for display names, not resource identifiers or units.
- Keywords and names are case-sensitive. Keywords are lowercase; units keep
  their documented case. A field may appear only once in its containing block.
- Identifiers use lowercase letters and internal hyphens. Individual endpoint
  names append a canonical decimal index, for example `api-0` or `web-api-12`.
  Digits elsewhere, empty components and leading index zeroes are errors.
- Values are decimal integers or fractions, optionally negative where allowed.
  Exponents, separators and leading `+` are unsupported. Booleans are `true`
  and `false`; `null` is allowed only for an existing nullable patch field.
- Blocks can nest, for example `host`, `to-lb`, `from-lb`, `rto`, `close`,
  `delayed-ack`, `congestion-control` and `mapping-timeouts`.

The compiler checks unknown declarations and fields. A syntactically recognized
feature is rejected until its semantic implementation is available.

## Units and exact numbers

| Context | Accepted units | Meaning |
| --- | --- | --- |
| Payload, MSS, windows | B, KB, MB, GB, TB, PB, EB | Decimal bytes, powers of 1000 |
| Link or forwarding rate | B, KB, MB, GB, TB, PB, EB | Decimal bytes **per second** |
| Time | ns, us, ms, s, m | Exact nanoseconds; m is 60 seconds |
| Loss or signal probability | % | Percentage; `1%` becomes 0.01 |
| ECN threshold | % | Queue occupancy percentage; `80%` becomes 80 |
| Counts, seed, speed | No suffix | Count, exact seed or playback multiplier |

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
Exactly one LB is supported. Addresses and port 80 are generated deterministically
before compilation completes; they are not randomized by the seed.

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
| Network queue policy / ECN threshold / signal probability | tailDrop / 0% / 100% |

`link` sets both endpoint directions. `to-lb` and `from-lb` override only that
direction. `host.queue` counts waiting CPU work; network `queue` counts waiting
packets. Active transmission and packet travel do not occupy queue slots.

## Resources, inheritance and identity

`backend api-0` declares one backend. `backends api { count 4 ... }` expands
into independent backends `api-0` through `api-3`; `from 10` starts at `api-10`.
It does not create a runtime backend group. Counts are checked before expansion
against the existing limit of 10,000 backends. Generated IDs must fit 128 bytes.

Backend addresses are assigned in lexical ID order across the **whole source**,
including later additions: `10.0.0.1` through `10.0.0.254`, then `10.0.1.1`
through `10.0.1.254`, continuing in the third octet, with port 80.
Each address belongs to its ID for the entire document. A retained backend's
address and port never change between states. Reordering declarations does not
change addresses; adding another ID may change the address allocation of a
separately compiled document, which is a different experiment.

`clients regular` declares a compact native client group. `regular-1` refers to
index 1 in that group; `client regular-1 { ... }` declares a sparse override.
It never allocates a second client. Runtime IDs remain `client/regular/1`.
Client aliases and explicit backend IDs must not collide. LB and group IDs also
share the native resource namespace; profile names have a separate namespace.
Client indices refer to historically reserved slots, not current active clients.
Total reserved slots across the timeline cannot exceed 16,777,215.

Endpoint link precedence is defaults → selected profile → endpoint settings →
directional settings. A client override applies after its resolved group links.
Profiles describe network settings only; `use mobile` selects a declared profile.
Profile definitions cannot change inside `at`; endpoints may select an existing
profile there. Explicit zero overrides an inherited value. A nullable
patch's `null` means inherit, as in native JSON. Resetting a previous DSL override
to `null` removes that patch; it does not inherit the preceding state's value.

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

Block settings can follow `set`, for example
`set backend api-0 { host { packet-time 10us } }`. `set backend api-0 admin active`
leaves drain without changing physical availability. `up` does not cancel drain.

Within one `at`, commands are read in source order: a resource must already exist
when referenced. `add` may be followed by `set` for that new resource. Writing
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

## Invalid examples

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

## Documentation roadmap

The static and timeline rules above are specified; compilation is not yet
implemented. Each implementation commit adds its field mappings, ranges,
dependencies and examples here. The final reference will include recipes for
bursts, failure/recovery, slow backends, CPU bottlenecks and asymmetric links.

The compiler validates its complete output with the existing frontend validator.
Apply and Start retain authoritative backend JSON validation. Diagnostics will
report source positions and a short explanation. Import or Apply never starts
a simulation automatically.
