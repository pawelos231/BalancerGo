# Scenario language, version 1

This is the specification for the frontend configuration language. Its compiler
will produce the existing scenario JSON v2. The Go CLI consumes that JSON; it
does not compile this language. No simulation behavior changes in this epic.

**Implementation status:** specification only. The lexer, compiler and editor
are not available yet. Examples are input/output fixtures for later commits.

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

The frontend TypeScript module will live in `src/scenario-language/`. It has no
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

## Documentation roadmap

The next specification step defines timeline semantics and complete resource
identity rules. Each implementation commit adds its field mappings, ranges,
dependencies and examples here. The final reference will include recipes for
bursts, failure/recovery, slow backends, CPU bottlenecks and asymmetric links.

The compiler validates its complete output with the existing frontend validator.
Apply and Start retain authoritative backend JSON validation. Diagnostics will
report source positions and a short explanation. Import or Apply never starts
a simulation automatically.
