# Syntax and units

## Summary

The language uses named blocks and one property per line. Names and units are
case-sensitive; quantities use decimal units and exact virtual times.

## On this page

[Blocks and comments](#blocks-and-comments) · [Names and text](#names-and-text) · [Units](#units) · [Zero and null](#zero-and-null) · [Time and playback](#time-and-playback)

## Blocks and comments

A complete valid scenario can include comments and an optional version line:

```lb
# Version 1 is also used when this line is omitted.
language 1

run 30s {
    speed 0
}

balancer lb {
    algo rr
}

backend api-0 {
    name "API server"
}

clients users {
    count 1
    payload 10KB # Bytes sent by each client.
}
```

Put `{` on the same line as the declaration or setting it opens. Close the block
with `}`. Separate fields with whitespace; do not add commas, colons or `=`.
`#` starts a comment outside a quoted string and continues to the line end.

**Invalid complete source:** the opening brace is on a new line.

```lb-invalid error="Expected an opening brace on the declaration line."
run 30s {
    speed 0
}
balancer lb
{
}
```

Write each property once per block. Keywords are lowercase, except the TCP
fields `SACK` and `ECN`: `SACK true` is valid inside `tcp`, while `sack true`
is an unknown setting. Booleans are `true` and `false`.

Declare exactly one `run` and one `balancer`. Put initial resources before any
[`at` blocks](/docs/language/timeline). If present, `language 1` must be the first
declaration. There are no variables, loops or expressions.

## Names and text

| Used for | Valid examples | Rule |
| --- | --- | --- |
| Balancer, profile, backend prefix | `lb`, `mobile`, `web-api` | Lowercase letters with internal hyphens |
| Individual backend | `api-0`, `web-api-12` | Prefix followed by a decimal index |
| Client group | `users`, `wave-20s` | Lowercase first segment; later segments may contain digits |
| Individual client | `users-0`, `wave-20s-2` | Group name followed by a decimal index |

Indices start at zero and have no leading zeroes. `api-0` is valid; `api-01`
and `api2-0` are invalid. Use unique resource names, up to 128 bytes.

**Invalid top-level fragment:** adding this backend fails because of its name.

```lb-invalid context=top-level error="Expected an endpoint name such as api-0"
backend api-01 {
}
```

Use double quotes for display text and explicit backend addresses, such as
`name "API server"`. Quoted text supports JSON escapes such as `\"` and `\n`;
an actual newline inside the quotes is invalid. Resource identifiers, profile
selections and quantities are unquoted.

## Units

| Kind of value | Accepted units | Example and meaning |
| --- | --- | --- |
| Payload, MSS, buffers, byte thresholds | `B`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB` | `10KB` = 10,000 bytes |
| Link, ingress, egress rate | The same byte units | `10MB` = 10,000,000 bytes per second |
| Duration or delay | `ns`, `us`, `ms`, `s`, `m` | `0.5ms` = 500,000ns; `1m` = 60s |
| Loss, queue threshold, signal probability | `%` | `1%` loss; `80%` queue occupancy |
| Counts, seed, speed, congestion window in segments | No suffix | `count 3`, `speed 0.5` |

Byte units are decimal powers of 1000. A link's `1GB` means one billion bytes
per second. Do not write `GiB`, `Mbps`, `Gbps`, or a `/s` suffix.

Decimals must resolve exactly to the field's stored unit: `0.001KB` is one byte
and `0.5ms` is valid, but `0.1B` as a payload and `0.1ns` as a time are invalid.
Rates resolve to whole bits per second. Fields also have size limits; for
example, `1EB` is an accepted rate while `2EB` exceeds the rate limit.
Exponents, digit separators and a leading `+` are unsupported.

## Zero and null

Zero is a value with a field-specific meaning. It can mean no waiting slots
(`queue 0`), ideal CPU processing (`packet-time 0ns`), an empty transfer
(`payload 0B`), or no playback pacing (`speed 0`). A positive link rate or MSS
cannot be zero.

`null` restores inheritance or the model default for a nullable **leaf** setting.
It does not mean zero or reuse the preceding timeline value. For example, this
valid fragment belongs inside `tcp`:

```lb-fragment context=tcp
receive-buffer null
SACK false
ECN false
congestion-control {
    maximum-window 0
}
```

Here the receive buffer uses the model default, while zero removes the explicit
maximum congestion-window cap. Nullable leaves include TCP, host and mapping
timeout settings, endpoint/profile network patches, backend availability/admin,
and client stream patches. Whole blocks, global defaults, LB ingress, names,
`use`, counts and birth times do not accept `null`. See
[defaults and profiles](/docs/language/defaults).

## Time and playback

Virtual time determines births, transmission, travel, CPU work, timers and
[`at` changes](/docs/language/timeline). Playback speed determines how quickly
that virtual experiment is shown in real time.

| `run` setting | Meaning |
| --- | --- |
| Header duration, such as `run 30s` | Positive virtual experiment horizon |
| `speed` | `1` is normal pacing, `2` is twice as fast, `0` runs without pacing |
| `snapshots` | Positive **real-time** interval between UI observations; default `100ms` |
| `seed` | Signed 64-bit integer for repeatable random routing, loss and signaling; default `1` |
| `max-events` | Positive event budget, up to 18,446,744,073,709,551,615; default 5,000,000,000 |

Changing `speed` does not change virtual link rates or CPU costs. Times must fit
signed 64-bit nanoseconds; a positive speed must also keep the paced duration
within that range. Clients scheduled after the horizon never start.
[How virtual time works](concept:virtual-time).
