# Fixing errors

## Summary

The compiler checks syntax, units, resource identities and complete scenario
states before applying anything. An error preserves the previous applied
configuration. Fix the first diagnostic, then compile again to reveal any
remaining errors.

## On this page

[Time order](#time-order) ·
[Command order and existence](#command-order-and-existence) ·
[Duplicate settings](#duplicate-settings) · [Names and units](#names-and-units) ·
[TCP dependencies](#tcp-dependencies) · [Unsupported syntax](#unsupported-syntax) ·
[Size and value limits](#size-and-value-limits)

Messages below quote actual compiler text without its changing line/column
prefix. Native validation messages may also contain a JSON path and a fix hint.
Fragments marked as TCP or backend settings belong inside that resource's
initial block. Timeline fragments follow the initial declarations.

## Time order

This timeline goes backward:

```lb-invalid context=timeline error="At timestamps must increase strictly and precede the run end."
at 2s {
    set balancer egress 1MB
}
at 1s {
    set balancer egress 2MB
}
```

The error is `At timestamps must increase strictly and precede the run end.`
Write blocks in increasing virtual time; each timestamp must satisfy
`0 < time < run duration`. For a run longer than 2s, use:

```lb-fragment context=timeline
at 1s {
    set balancer egress 1MB
}
at 2s {
    set balancer egress 2MB
}
```

Related timing mistakes have these diagnostics and fixes:

| Bad input | Compiler text | Why and fix |
| --- | --- | --- |
| Both `at 1s` and `at 1000ms` | `At timestamps must increase strictly and precede the run end.` | Equal timestamps; merge the commands into one `at 1s` block |
| `at 0s` | `Value must be between 1 and 9223372036854775807.` | The timestamp must be positive; put time-zero settings in initial declarations |
| `at 30s` in a 30s run | `At timestamps must increase strictly and precede the run end.` | A change cannot occur at the horizon; move it earlier or extend the run |

Initial declarations must also come before every `at` block. A `profile mobile`
declaration after a timeline block produces
`Initial declarations must precede all at blocks.` Move it before the first
`at`; profiles themselves stay fixed for the run. See [timeline](/docs/language/timeline).

## Command order and existence

This command refers to `extra-0` before it has been added:

```lb-invalid context=timeline error="Unknown backend extra-0."
at 1s {
    set backend extra-0 link 1MB
    add backend extra-0 {}
}
```

The error is `Unknown backend extra-0.` Commands are read in source order.
Add the resource first, then change it:

```lb-fragment context=timeline
at 1s {
    add backend extra-0 {}
    set backend extra-0 link 1MB
}
```

An existing resource also needs the right name and reserved slot:

| Bad input | Compiler text | Why and fix |
| --- | --- | --- |
| `stop arrivals missing` | `Unknown client group missing.` | Declare or add that group before referencing it |
| `client users-3 {}` when `users` has count 3 | `outside its declared group` | Indices start at zero; use `users-2` or reserve more slots |
| Adding a group at 2s with `start 1s` | `new group must start at or after its insertion time` | Omit `start` to use 2s, or specify 2s or later |

## Duplicate settings

A repeated field is an error, even when the values match. Inside one initial
backend block, this fails:

```lb-invalid context=backend error="Duplicate setting"
link 1MB
link 2MB
```

The error is `Duplicate setting "link".` Keep one value:

```lb-fragment context=backend
link 2MB
```

Different spellings can also write the same native field. Inside `tcp`, this
fails with `Setting mode overlaps an earlier value.`:

```lb-invalid context=tcp error="Setting mode overlaps an earlier value."
congestion reno
congestion-control {
    mode cubic
}
```

Select one spelling, for example:

```lb-fragment context=tcp
congestion cubic
```

In one `at` block, separate commands still share conflict checks. Setting a
backend's `link` and then its `to-lb { link ... }` writes that direction twice
and produces `Setting link overlaps an earlier value.` Set each direction
explicitly once instead. Likewise, `down backend api-0` followed by
`set backend api-0 available true` produces
`Setting available overlaps an earlier value.` Keep the intended action.

Initial shared and directional link fields may coexist: a directional override
in one endpoint declaration has defined precedence. See [topology](/docs/language/topology).

## Names and units

Names, keywords and units are case-sensitive. These are input mistakes within
supported fields, so correct their spelling or value:

| Bad input | Compiler text | Why and fix |
| --- | --- | --- |
| `backend api-01 {}` | `Expected an endpoint name such as api-0, without digits in its prefix.` | Use canonical index `api-1`; backend prefix segments contain letters |
| `backend api2-0 {}` | `Expected an endpoint name such as api-0, without digits in its prefix.` | Use a prefix such as `api-two-0` |
| `sack true` inside `tcp` | `Unknown setting "sack".` | Spell the field `SACK true` |
| `link 10Mbps` | `Unsupported unit "Mbps" for this field.` | Rates use decimal bytes per second; use `link 1.25MB` for 10 megabits/s |
| `travel 0.1ns` | `Value does not resolve to a whole native unit.` | Use a whole number of nanoseconds, such as `travel 1ns` |

The receive-buffer limit is 65,535B. For example, the following TCP setting
actually exceeds it:

```lb-invalid context=tcp error="Value must be between 1 and 65535."
receive-buffer 66KB
```

Use a value within the limit:

```lb-fragment context=tcp
receive-buffer 65535B
```

Duplicate resource identities are different from duplicate fields. Declaring
`backend api-0` twice, including through `backends api { count 1 }`, produces
`Duplicate backend api-0.` Keep one declaration or use a different prefix/index.
See [syntax and units](/docs/language/syntax).

## TCP dependencies

Valid individual values can form an invalid combination. Inside `tcp`, these
RTO limits are reversed:

```lb-invalid context=tcp error="require minimum <= initial <= maximum timeout"
rto {
    minimum 2s
    initial 1s
}
```

The error includes `require minimum <= initial <= maximum timeout`.
The omitted maximum remains 60s. Restore an ordered set:

```lb-fragment context=tcp
rto {
    minimum 200ms
    initial 1s
    maximum 60s
}
```

Other dependency errors have specific fixes:

| Bad combination inside `tcp` | Compiler text | Why and fix |
| --- | --- | --- |
| `congestion tahoe` with `SACK true` | `requires Reno, NewReno or CUBIC with fastRetransmit enabled` | Select `congestion reno` and keep fast retransmit enabled, or disable SACK |
| `congestion none` with `ECN true` | `ECNenabled: requires congestion control` | Select a congestion mode such as `reno`, or use `ECN false` |
| `congestion none` with `maximum-window 8` inside `congestion-control` | `maximumWindowSegments: requires congestion control` | Use `maximum-window 0` or select a congestion mode |
| `mss 40000B` with omitted `ssthresh` | `initialSlowStartThresholdBytes: must be in 80000..9223372036854775807` | Set `ssthresh 80KB` inside `congestion-control`, or lower MSS |
| `max-delay 501ms` inside `delayed-ack` | `must be positive and at most 500 ms` | Use `max-delay 40ms`, including when delayed ACK is disabled |

See [TCP settings](/docs/language/tcp) for all bounds and defaults.

## Unsupported syntax

These capabilities are outside language version 1. Adjust the experiment to a
supported form; changing a number cannot enable the missing capability.

| Unsupported input | Actual compiler text | Supported approach |
| --- | --- | --- |
| `set tcp mss 1200B` inside `at` | `Set supports balancer, backend, clients or client.` | Put `mss 1200B` in the initial `tcp` block; compare separate runs |
| `port 8080` inside a backend | `Unknown setting "port".` | Backends use port 80; `address` changes the host only |
| `host { cores 2 }` inside `clients` | `Unknown setting "host".` | Model CPU on the LB or backend |
| A second `balancer` declaration | `Only one balancer declaration is allowed.` | Use one LB per experiment |
| `for i` or `let rate` at top level | `Unsupported declaration "for".` or `Unsupported declaration "let".` | Write explicit declarations; use `backends` and `clients` for counts |
| `mss auto` inside `tcp` | `Expected a numeric value.` | Choose an explicit byte value such as `mss 1024B` |
| `delete backend api-0` inside `at` | `Unsupported timeline command "delete".` | Use `down` for physical failure or `drain` to stop new admission |
| `remove backend api-0` inside `at` | `Expected clients.` | Backend removal is unsupported; `remove clients users` removes a generator |
| `set backends api link 1MB` inside `at` | `Set supports balancer, backend, clients or client.` | Set each expanded backend by its full ID |
| `set backend api-0 to-lb.link 1MB` | `Unexpected character ".".` | Use `set backend api-0 { to-lb { link 1MB } }` with nested blocks |

Start from a [complete small example](/docs/language/examples) when several
diagnostics make a fragment hard to repair.

## Size and value limits

Compilation checks these limits before applying a scenario:

| Item | Limit |
| --- | --- |
| Source and compiled JSON | Each at most 2 MiB of UTF-8; expanded timeline states can make JSON larger than source |
| Source tokens | 200,000 |
| Nested blocks | 64 levels |
| Numeric literal | 128 characters |
| Backends | 10,000 across the source |
| Reserved client slots | 16,777,215 historical total: sum of each group's highest count, including removed groups |

Most native byte, count and nanosecond fields must fit signed 64-bit integers;
their maximum is 9,223,372,036,854,775,807. `seed` also permits negative int64
values, while `max-events` permits unsigned 64-bit values. Individual fields
can have tighter bounds; see [syntax and units](/docs/language/syntax).
