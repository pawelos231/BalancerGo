# TCP settings

## Summary

The top-level `tcp` block configures transport behavior for the whole run. Start
with its defaults, then change one setting at a time. TCP cannot change inside
an `at` block. Scalar settings accept `null` to restore the model default.

## On this page

[Starting configuration](#starting-configuration) ·
[Congestion control](#congestion-control) · [MSS](#mss) ·
[Receive window](#receive-window) · [SACK](#sack) · [ECN](#ecn) ·
[RTO](#rto) · [Closing](#closing) · [Delayed ACK](#delayed-ack)

## Starting configuration

This complete configuration enables SACK and delayed ACK while retaining the
default Reno mode and retransmission timers. Omitted TCP settings keep their
defaults. `speed 0` runs without playback pacing.

```lb
run 6s {
    speed 0
}
tcp {
    congestion reno
    mss 1024B
    receive-buffer 65535B
    SACK true
    ECN false
    rto {
        mode adaptive
        backoff true
        initial 1s
        minimum 200ms
        maximum 60s
    }
    delayed-ack {
        enabled true
        segments 2
        max-delay 40ms
    }
}
balancer lb {}
backend api-0 {}
clients users {
    count 2
    payload 10KB
}
```

The following fragments belong inside the initial `tcp { ... }` block.
See [defaults](/docs/language/defaults) for the full pinned default set and
[errors](/docs/language/errors#tcp-dependencies) for incompatible combinations.

## Congestion control

`congestion` selects `reno`, `newreno`, `tahoe`, `cubic`, or `none`. Its default
is `reno`. The nested spelling `congestion-control { mode ... }` selects the
same field; use one spelling per block.

For example, replace the mode with CUBIC and cap its window:

```lb-fragment context=tcp
congestion cubic
congestion-control {
    initial-window 4
    maximum-window 16
    ssthresh 32KB
    fast-retransmit true
    fast-convergence true
}
```

| Field inside `congestion-control` | Default | Units and requirements |
| --- | --- | --- |
| `mode` | `reno` | Same choices as `congestion`; the two spellings cannot coexist |
| `fast-retransmit` | `true` | Boolean; defaults to `false` in mode `none` |
| `fast-convergence` | `true` | Boolean; controls CUBIC fast convergence |
| `initial-window` | `4` | Positive count of MSS-sized segments |
| `maximum-window` | `0` | Segment count; zero removes the explicit cap |
| `ssthresh` | `65535B` | Bytes; at least twice MSS, at most 9,223,372,036,854,775,807B |

Window counts must not exceed `floor(9,223,372,036,854,775,807 / MSS)`.
Mode `none` forbids enabled fast retransmit, SACK, ECN and a nonzero
`maximum-window`. This valid fragment selects it without those features:

```lb-fragment context=tcp
congestion none
SACK false
ECN false
```

[How congestion control works](concept:congestion).

## MSS

| Field | Default | Units and bounds |
| --- | --- | --- |
| `mss` | `1024B` | 1–65,495 bytes |

MSS is configured manually; `mss auto` and MSS negotiation are unsupported.
Changing MSS changes the bytes represented by a segment-count window. Large
MSS values can also require a larger `ssthresh`: the default 65,535B threshold
is too small when twice MSS exceeds it.

For example, these values satisfy the threshold requirement:

```lb-fragment context=tcp
mss 40000B
congestion-control {
    ssthresh 80KB
}
```

[How MSS works](concept:mss).

## Receive window

| Field | Default | Units and bounds |
| --- | --- | --- |
| `receive-buffer` | `65535B` | 1–65,535 bytes |

The receive buffer sets the modeled receiver capacity. Its limit is independent
of the congestion window; raising one does not remove the other's limit.

Inside `tcp`, a smaller receive buffer looks like this:

```lb-fragment context=tcp
receive-buffer 16KB
```

[How the receive window works](concept:rwnd).

## SACK

| Field | Default | Requirement |
| --- | --- | --- |
| `SACK` | `false` | Boolean; Reno, NewReno or CUBIC with fast retransmit enabled |

Use uppercase `SACK`. Tahoe and `none` cannot enable it. This fragment makes
the required mode and fast retransmit explicit:

```lb-fragment context=tcp
congestion newreno
SACK true
congestion-control {
    fast-retransmit true
}
```

[How SACK works](concept:sack).

## ECN

| Field | Default | Requirement |
| --- | --- | --- |
| `ECN` | `false` | Boolean; congestion mode must differ from `none` |

Use uppercase `ECN`. Enabling it makes transport ECN-capable; configure a
network queue with `queue-policy threshold` to experiment with early signaling.
That queue's `ecn-threshold` is an occupancy percentage and
`signal-probability` is a signaling probability. Full queues can still drop.
See [network fields](/docs/language/topology).

Inside `tcp`, enable ECN with a compatible mode:

```lb-fragment context=tcp
congestion reno
ECN true
```

[How ECN works](concept:ecn) · [How queues work](concept:queues).

## RTO

RTO settings live in `tcp.rto`. `mode` controls estimation; `backoff` separately
controls exponential growth after a timeout. The default minimum is **200ms**.
It is a default value, not a hard lower bound.

| Field inside `rto` | Default | Units and requirements |
| --- | --- | --- |
| `mode` | `adaptive` | `adaptive` or `fixed` |
| `backoff` | `true` | Boolean, independent of mode |
| `initial` | `1s` | Positive time |
| `minimum` | `200ms` | Positive time, no greater than `initial` |
| `maximum` | `60s` | Positive time, no smaller than `initial` |

All modes require `minimum <= initial <= maximum`. Times must fit signed
64-bit nanoseconds. For a fixed 500ms retry interval without backoff, put this
inside `tcp`:

```lb-fragment context=tcp
rto {
    mode fixed
    backoff false
    minimum 500ms
    initial 500ms
    maximum 500ms
}
```

Idle cleanup is separate from RTO. The default client idle timeout stays at 20s;
it does not grow with retransmission backoff. It can therefore close an idle
connection before a backed-off retry. Increase `client-idle` or set it to `0ns`
when a long retry experiment needs that cleanup disabled.

[How RTO works](concept:rto).

## Closing

Closing timers live in `tcp.close`; idle timers are direct `tcp` fields.

| Field | Default | Units and bounds |
| --- | --- | --- |
| `close.msl` | `30s` | Positive time, at most half the signed 64-bit nanosecond range |
| `close.fin-wait` | `60s` | Positive time fitting signed 64-bit nanoseconds |
| `close.fin-retries` | `5` | Integer 0–30 |
| `client-idle` | `20s` | Nonnegative time; zero disables idle cleanup |
| `backend-half-open-idle` | `5s` | Nonnegative time; zero disables idle cleanup |
| `backend-established-idle` | `0ns` | Nonnegative time; zero disables idle cleanup |

TIME_WAIT lasts twice MSL. These settings shorten closing waits for a small
experiment and leave the client enough idle time for longer retries:

```lb-fragment context=tcp
client-idle 120s
close {
    msl 1s
    fin-wait 2s
    fin-retries 3
}
```

LB mapping cleanup has separate [mapping timeouts](/docs/language/topology).
Reaching the run horizon does not imply every connection has left TIME_WAIT.
[How closing works](concept:closing).

## Delayed ACK

| Field inside `delayed-ack` | Default | Units and bounds |
| --- | --- | --- |
| `enabled` | `false` | Boolean |
| `segments` | `2` | Integer 1–2 |
| `max-delay` | `40ms` | Positive time, at most 500ms |

The threshold and timer must remain valid even when delayed ACK is disabled.
Put this fragment inside `tcp` to enable the default delayed ACK policy:

```lb-fragment context=tcp
delayed-ack {
    enabled true
    segments 2
    max-delay 40ms
}
```

[How delayed ACK works](concept:delayed-ack).
