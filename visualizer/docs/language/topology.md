# Topology and capacity

## Summary

Declare one load balancer, an optional backend pool and client groups. Endpoint
links, shared LB limits, network queues and CPU queues model different costs.
Configured rates are ceilings, not guaranteed throughput.

## On this page

[Backends](#backends) · [Endpoint links](#endpoint-links) · [Shared LB limits](#shared-lb-limits) · [Queues](#queues) · [CPU](#cpu)

## Backends

`backend api-0` declares one backend. `backends api` expands a short declaration
into separate backends. This valid top-level fragment adds `api-10` and `api-11`:

```lb-fragment context=top-level
backends api {
    count 2
    from 10
    link 100MB
}
```

`count` is required and positive; `from` defaults to zero. Expanded backends have
independent links and state, rather than a runtime group. Up to 10,000 backends
may be declared across the whole timeline. Omit backend declarations to start
with an empty pool. See [clients and traffic](/docs/language/clients) for senders.

| Backend setting | Meaning |
| --- | --- |
| `name` | Optional quoted display text |
| `address` | Optional quoted address, set when declaring or adding the backend |
| `available` | Physical availability: `true` by default |
| `admin` | `active` by default; `drain` prevents new mappings |
| `use` | Select an existing [network profile](/docs/language/defaults) |
| Network fields, `to-lb`, `from-lb` | Endpoint links described below |
| `host` | CPU settings described below |

Addresses are generated deterministically from all backend IDs, in sorted order,
starting at `10.0.0.1`; the port is 80. Explicit addresses must not collide with
another backend. An existing backend's ID, address and port stay fixed.

Physical availability and drain are separate controls. A failure does not
instantly change the LB's observed health or reroute existing connections.
See [timeline changes](/docs/language/timeline) and
[how backend lifecycle works](concept:backend-lifecycle).

## Endpoint links

Clients and backends each have two link directions. `link` sets the rate in
both directions; `to-lb` and `from-lb` override only their named direction.

This valid fragment goes inside `backend api-0`:

```lb-fragment context=backend
link 100MB
travel 1ms
to-lb {
    link 20MB
}
from-lb {
    link 80MB
}
```

For a **client**, `to-lb` carries client-to-LB packets and `from-lb` carries
LB-to-client packets. For a **backend**, `from-lb` carries LB-to-backend packets
and `to-lb` carries backend-to-LB packets, including returning ACKs.

| Network field | Meaning |
| --- | --- |
| `link` | Positive decimal byte rate per second; `10MB` means 10MB/s |
| `travel` | Nonnegative virtual travel time after transmission |
| `queue` | Nonnegative number of waiting packets |
| `loss` | Independent random loss probability, `0%` through `100%` |
| `queue-policy` | `tail-drop`, `threshold`, or `default` |
| `ecn-threshold` | Waiting occupancy threshold, `0%` through `100%` |
| `signal-probability` | Signaling probability above the threshold, `0%` through `100%` |

All these fields work on endpoints and inside their directional blocks. The
order is defaults → profile → endpoint settings → directional settings, so a
direction wins regardless of where its block appears in the declaration.
[How links work](concept:links).

## Shared LB limits

`ingress` is one shared physical receiver for arrivals from clients **and**
backends. `egress` is the shared forwarding budget across separate output
links and queues. It is not a per-client or per-group allocation.

This valid fragment goes inside `balancer lb`. The ingress block accepts all
network fields; the shorter `ingress 100MB` changes just its rate.

```lb-fragment context=balancer
algo rr
ingress {
    link 100MB
    travel 0ns
    queue 256
    queue-policy threshold
    ecn-threshold 80%
    signal-probability 100%
}
egress 50MB
host {
    cores 2
    packet-time 10us
    queue 1024
}
```

Only `algo rr` (round robin, the default) and `algo ran` (random) are supported.
Random selection uses the run seed. **Invalid fragment inside `balancer lb`:**

```lb-invalid context=balancer error="Expected one of: rr, ran."
algo least-connections
```

The other LB blocks are `host` and `mapping-timeouts`. The latter accepts
`half-open`, `established`, `closing` and `reset` idle times. Defaults are `5s`,
`50s`, `120s` and `10s` respectively; zero disables that cleanup timer.
[TCP settings](/docs/language/tcp) cover connection timers.

Global network defaults supply the initial LB ingress and forwarding **rates**.
Other ingress properties have separate defaults, including zero ingress travel.
For the model behind these two limits, read [how ingress works](concept:ingress)
and [how forwarding works](concept:egress).

## Queues

Network queues count packets waiting to transmit. A packet currently being
transmitted or traveling does not occupy a waiting slot. A full queue drops an
arrival; `queue 0` leaves no waiting slots. CPU queues are separate.

`tail-drop` waits until the queue is full to drop. `threshold` can signal earlier
once waiting occupancy, including the incoming packet, reaches `ecn-threshold`.
With `signal-probability 100%`, every eligible arrival at that threshold is
signaled: ECN-capable packets can be marked, and other packets can be dropped.
Full queues still drop arrivals.

`ecn-threshold 0%` is a valid threshold, not an off switch. Use `tail-drop` or
`signal-probability 0%` to turn off early signaling. `default` selects the model's
default FIFO tail-drop policy. Marking also depends on
[TCP ECN settings](/docs/language/tcp). [How queues work](concept:queues).

## CPU

The LB and each backend accept a `host` block. Client generators do not.

| Host field | Meaning and default |
| --- | --- |
| `cores` | Parallel packet workers, 1–1024; default `1` |
| `packet-time` | Virtual processing time per packet; default `0ns` |
| `queue` | Waiting CPU packets, 0–1,000,000; default `1024` |

`packet-time 0ns` bypasses CPU processing: the default is an ideal CPU, so a
CPU bottleneck needs an explicit positive cost. The example above uses two
workers taking `10us` per packet. These settings describe this simulator's
packet processing model; they do not emulate a Linux host stack.

CPU capacity, endpoint links, shared LB limits, queues and TCP can all affect
the observed result. [How CPU processing works](concept:cpu).
